// Package upstreamproxy dials targets through a parent HTTP or SOCKS5 proxy.
package upstreamproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Parse validates and normalizes an upstream proxy URL (http/https/socks5/socks5h).
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("upstreamproxy: URL 为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("upstreamproxy: 解析 URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("upstreamproxy: 不支持协议 %q（仅 http/https/socks5）", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("upstreamproxy: 缺少主机名")
	}
	return u, nil
}

// HostPort returns the upstream proxy dial address host:port.
func HostPort(u *url.URL) (string, error) {
	host := u.Hostname()
	port, err := proxyPort(u)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

// TargetIsUpstream reports whether host:port is the upstream proxy itself (not a site behind it).
func TargetIsUpstream(u *url.URL, host string, port uint16) bool {
	if u == nil {
		return false
	}
	upPort, err := proxyPort(u)
	if err != nil {
		return false
	}
	if port != upPort {
		return false
	}
	upHost := u.Hostname()
	if strings.EqualFold(host, upHost) {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if upIP := net.ParseIP(upHost); upIP != nil {
			return ip.Equal(upIP)
		}
		ips, err := ResolvePublicIPv4(upHost)
		if err != nil {
			return false
		}
		for _, cand := range ips {
			if ip.Equal(cand) {
				return true
			}
		}
	}
	return false
}

// BypassIPStrings returns IPv4 strings for TUN/pf bypass (upstream 主机名解析 + 字面 IP).
func BypassIPStrings(u *url.URL) []string {
	if u == nil {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return []string{ip4.String()}
		}
		return nil
	}
	ips, err := ResolvePublicIPv4(host)
	if err != nil {
		log.Printf("[上级代理] 解析旁路 IP 失败 host=%s: %v", host, err)
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func proxyPort(u *url.URL) (uint16, error) {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	var n uint64
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("upstreamproxy: 代理端口无效 %q", port)
		}
		n = n*10 + uint64(ch-'0')
		if n > 65535 {
			return 0, fmt.Errorf("upstreamproxy: 代理端口超出范围 %q", port)
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("upstreamproxy: 代理端口无效 %q", port)
	}
	return uint16(n), nil
}

// ResolvePublicIPv4 resolves host through public DNS servers and ignores fake-ip results.
func ResolvePublicIPv4(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil && !isFakeIPv4(ip4) {
			return []net.IP{ip4}, nil
		}
		return nil, fmt.Errorf("upstreamproxy: %s 不是可用公网 IPv4", host)
	}
	var lastErr error
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "tcp", server)
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		ips, err := r.LookupIP(ctx, "ip4", host)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var out []net.IP
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil && !isFakeIPv4(ip4) {
				out = append(out, append(net.IP(nil), ip4...))
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("upstreamproxy: %s 没有公网 IPv4 记录", host)
}

// WithDialHost returns a copy of u whose Host is replaced with hostOrIP while preserving the port.
func WithDialHost(u *url.URL, hostOrIP string) *url.URL {
	c := *u
	port := c.Port()
	if port == "" {
		switch strings.ToLower(c.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	c.Host = net.JoinHostPort(hostOrIP, port)
	return &c
}

// Dial connects to target host:port via the upstream proxy.
func Dial(u *url.URL, targetHost string, targetPort uint16) (net.Conn, error) {
	return DialVia(u, targetHost, targetPort, func(host string, port uint16) (net.Conn, error) {
		return net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)), 20*time.Second)
	})
}

// Connector connects to a host:port. Callers can supply a connector backed by
// feizhu's TLS tunnel so the upstream proxy handshake is inside that tunnel.
type Connector func(host string, port uint16) (net.Conn, error)

// DialVia connects to target host:port via u, using connect to reach u itself.
func DialVia(u *url.URL, targetHost string, targetPort uint16, connect Connector) (net.Conn, error) {
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return dialHTTP(u, targetHost, targetPort, connect)
	case "socks5", "socks5h":
		return dialSOCKS5(u, targetHost, targetPort, connect)
	default:
		return nil, fmt.Errorf("upstreamproxy: 不支持协议 %q", u.Scheme)
	}
}

func isFakeIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19)
}

func dialHTTP(u *url.URL, targetHost string, targetPort uint16, connect Connector) (net.Conn, error) {
	proxyPort, err := proxyPort(u)
	if err != nil {
		return nil, err
	}
	c, err := connect(u.Hostname(), proxyPort)
	if err != nil {
		return nil, fmt.Errorf("upstreamproxy: 连接上级代理 %s: %w", u.Host, err)
	}
	target := net.JoinHostPort(targetHost, fmt.Sprintf("%d", targetPort))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if auth := proxyAuthorization(u); auth != "" {
		req += "Proxy-Authorization: " + auth + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("upstreamproxy: 读取 CONNECT 响应: %w", err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("upstreamproxy: CONNECT %s 失败: %s", target, resp.Status)
	}
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: c, r: br}, nil
	}
	return c, nil
}

func dialSOCKS5(u *url.URL, targetHost string, targetPort uint16, connect Connector) (net.Conn, error) {
	proxyPort, err := proxyPort(u)
	if err != nil {
		return nil, err
	}
	c, err := connect(u.Hostname(), proxyPort)
	if err != nil {
		return nil, fmt.Errorf("upstreamproxy: 连接 SOCKS5 %s: %w", u.Host, err)
	}
	var authMethods []byte
	if u.User != nil {
		authMethods = []byte{5, 2, 0, 2}
	} else {
		authMethods = []byte{5, 1, 0}
	}
	if _, err := c.Write(authMethods); err != nil {
		c.Close()
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		c.Close()
		return nil, err
	}
	if buf[0] != 5 {
		c.Close()
		return nil, fmt.Errorf("upstreamproxy: SOCKS5 版本错误")
	}
	if buf[1] == 2 {
		user, pass := "", ""
		if u.User != nil {
			user = u.User.Username()
			pass, _ = u.User.Password()
		}
		auth := []byte{1, byte(len(user))}
		auth = append(auth, user...)
		auth = append(auth, byte(len(pass)))
		auth = append(auth, pass...)
		if _, err := c.Write(auth); err != nil {
			c.Close()
			return nil, err
		}
		if _, err := io.ReadFull(c, buf[:2]); err != nil {
			c.Close()
			return nil, err
		}
		if buf[1] != 0 {
			c.Close()
			return nil, fmt.Errorf("upstreamproxy: SOCKS5 认证失败")
		}
	} else if buf[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("upstreamproxy: SOCKS5 无可接受认证方法")
	}

	host := targetHost
	if strings.EqualFold(u.Scheme, "socks5h") {
		// remote DNS: send domain name
	} else if ip := net.ParseIP(host); ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			c.Close()
			return nil, fmt.Errorf("upstreamproxy: 解析 %s: %w", host, err)
		}
		host = ips[0].String()
	}

	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ip...)
		}
	} else {
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(targetPort>>8), byte(targetPort))
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	resp := make([]byte, 4)
	if _, err := io.ReadFull(c, resp); err != nil {
		c.Close()
		return nil, err
	}
	if resp[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("upstreamproxy: SOCKS5 CONNECT 失败 rep=%d", resp[1])
	}
	switch resp[3] {
	case 1:
		_, _ = io.ReadFull(c, make([]byte, 4+2))
	case 3:
		lenBuf := make([]byte, 1)
		_, _ = io.ReadFull(c, lenBuf)
		_, _ = io.ReadFull(c, make([]byte, int(lenBuf[0])+2))
	case 4:
		_, _ = io.ReadFull(c, make([]byte, 16+2))
	}
	return c, nil
}

func proxyAuthorization(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user == "" && pass == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}
