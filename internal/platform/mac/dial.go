// Package macplat 为 macOS 专用拨号：TUN 拦截的 TCP 一律经 feizhu TLS（仅 127.0.0.0/8 回环例外）。
package macplat

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/feizhu/feizhu/internal/tunnel"
	"github.com/feizhu/feizhu/internal/upstreamproxy"
)

// TunnelDialer 承载 macOS TUN/代理出口所需字段。
type TunnelDialer struct {
	Upstream   *url.URL
	ServerAddr string
	Password   string
	TLSConfig  *tls.Config
	LogTUN     bool
}

// DialLocal 本地 HTTP/SOCKS（127.0.0.1:7890，不经 TUN）。
func (d *TunnelDialer) DialLocal(host string, port uint16) (net.Conn, error) {
	if isLoopbackHost(host) {
		return dialLoopback(host, port)
	}
	if isLocalDirectHost(host) {
		return dialDirect(host, port)
	}
	return d.dialViaUpstreamOrTunnel(host, port)
}

// DialTUN 凡被 pf 送进 TUN 的 TCP：一律 feizhu TLS 拨号到目标 host:port，无端口/IP 例外。
func (d *TunnelDialer) DialTUN(host string, port uint16) (net.Conn, error) {
	if isLoopbackHost(host) {
		return dialLoopback(host, port)
	}
	return dialTunnel(d.ServerAddr, d.Password, d.TLSConfig, host, port)
}

func (d *TunnelDialer) dialViaUpstreamOrTunnel(host string, port uint16) (net.Conn, error) {
	if d.Upstream != nil {
		if upstreamproxy.TargetIsUpstream(d.Upstream, host, port) {
			return d.connectViaTunnel(host, port)
		}
		return upstreamproxy.DialVia(d.Upstream, host, port, d.connectViaTunnel)
	}
	return dialTunnel(d.ServerAddr, d.Password, d.TLSConfig, host, port)
}

func (d *TunnelDialer) connectViaTunnel(host string, port uint16) (net.Conn, error) {
	return dialTunnel(d.ServerAddr, d.Password, d.TLSConfig, host, port)
}

func LogTUNDialHint(*url.URL, string, error) {}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLocalDirectHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19) {
		return false
	}
	return ip4.IsPrivate() || ip4.IsLinkLocalUnicast()
}

func dialDirect(host string, port uint16) (net.Conn, error) {
	d := net.Dialer{Timeout: 15 * time.Second}
	return d.Dial("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
}

func dialLoopback(host string, port uint16) (net.Conn, error) {
	h := host
	if strings.EqualFold(host, "localhost") {
		h = "127.0.0.1"
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	return d.Dial("tcp", net.JoinHostPort(h, fmt.Sprintf("%d", port)))
}

func dialTunnel(serverAddr, password string, tlsCfg *tls.Config, host string, port uint16) (*tls.Conn, error) {
	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("连接服务端 %s: %w", serverAddr, err)
	}
	tlsConn := tls.Client(raw, tlsCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("TLS 握手 server=%s: %w", serverAddr, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	if err := tunnel.ClientHandshake(tlsConn, password, host, port, deadline); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("FZ1 拨号 %s:%d: %w", host, port, err)
	}
	return tlsConn, nil
}
