// Package socks5 实现本地 SOCKS5 子集（无认证、仅 CONNECT），由上层提供经 TLS 的拨号函数。
// 目标地址仅出现在 dial() 内并最终以 tunnel.TypeDialReq 发往 feizhu-server，链路上为 TLS 密文。
package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// DialFunc 建立到目标 host:port 的已就绪连接（例如 *tls.Conn 已完成 ClientHandshake）。
type DialFunc func(host string, port uint16) (net.Conn, error)

// Serve 处理一条客户端 TCP 连接：完成 SOCKS5 握手后调用 dial，再双向转发。
func Serve(c net.Conn, dial DialFunc) error {
	defer c.Close()

	buf := make([]byte, 260)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return err
	}
	if buf[0] != 5 {
		return errors.New("socks5: 不支持的协议版本")
	}
	nmeth := int(buf[1])
	if nmeth < 1 || nmeth > 255 {
		return errors.New("socks5: 非法 nmethods")
	}
	if _, err := io.ReadFull(c, buf[:nmeth]); err != nil {
		return err
	}
	noAuth := false
	for i := 0; i < nmeth; i++ {
		if buf[i] == 0 {
			noAuth = true
			break
		}
	}
	if !noAuth {
		_, _ = c.Write([]byte{5, 0xff})
		return errors.New("socks5: 需要无认证(0x00)方法")
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return err
	}

	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return err
	}
	if buf[0] != 5 {
		return errors.New("socks5: 请求版本错误")
	}
	cmd := buf[1]
	atyp := buf[3]
	host, port, err := readAddr(buf, c, atyp)
	if err != nil {
		return err
	}

	if cmd == 3 {
		return serveUDPAssociate(c, dial)
	}
	if cmd != 1 {
		_ = sendRep(c, 0x07) // command not supported
		return fmt.Errorf("socks5: 不支持的 CMD %d（仅支持 CONNECT/UDP ASSOCIATE）", cmd)
	}
	if isFakeIPv4(host) && (port == 443 || port == 80) {
		return serveFakeIPConnect(c, host, port, dial)
	}
	rc, err := dial(host, port)
	if err != nil {
		_ = sendRep(c, repForDialErr(err))
		return err
	}
	defer rc.Close()

	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(rc, c)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(c, rc)
		errc <- err
	}()
	<-errc
	return nil
}

func serveFakeIPConnect(c net.Conn, host string, port uint16, dial DialFunc) error {
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	first := make([]byte, 8192)
	n, err := c.Read(first)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}
	first = first[:n]
	target := ""
	if port == 443 {
		target = parseTLSSNI(first)
	} else if port == 80 {
		target = parseHTTPHost(first)
	}
	if target == "" {
		target = host
	}
	if target == host && isFakeIPv4(host) {
		log.Printf("[socks5] fake-ip %s:%d 未能从首包解析域名，将按假 IP 拨号（易失败）", host, port)
	}
	rc, err := dial(target, port)
	if err != nil {
		return err
	}
	defer rc.Close()
	if _, err := rc.Write(first); err != nil {
		return err
	}
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(rc, c)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(c, rc)
		errc <- err
	}()
	<-errc
	return nil
}

func isFakeIPv4(host string) bool {
	ip := net.ParseIP(host).To4()
	return ip != nil && ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

func parseHTTPHost(b []byte) string {
	headerEnd := strings.Index(string(b), "\r\n\r\n")
	if headerEnd < 0 {
		return ""
	}
	for _, line := range strings.Split(string(b[:headerEnd]), "\r\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "host") {
			host := strings.TrimSpace(v)
			if h, _, err := net.SplitHostPort(host); err == nil {
				return h
			}
			return host
		}
	}
	return ""
}

func parseTLSSNI(b []byte) string {
	if len(b) < 5 || b[0] != 0x16 {
		return ""
	}
	recordLen := int(binary.BigEndian.Uint16(b[3:5]))
	if len(b) < 5+recordLen || recordLen < 42 {
		return ""
	}
	p := b[5 : 5+recordLen]
	if len(p) < 4 || p[0] != 0x01 {
		return ""
	}
	hsLen := int(p[1])<<16 | int(p[2])<<8 | int(p[3])
	if len(p) < 4+hsLen {
		return ""
	}
	p = p[4 : 4+hsLen]
	if len(p) < 34 {
		return ""
	}
	i := 34
	if len(p) < i+1 {
		return ""
	}
	sessionLen := int(p[i])
	i += 1 + sessionLen
	if len(p) < i+2 {
		return ""
	}
	cipherLen := int(binary.BigEndian.Uint16(p[i : i+2]))
	i += 2 + cipherLen
	if len(p) < i+1 {
		return ""
	}
	compLen := int(p[i])
	i += 1 + compLen
	if len(p) < i+2 {
		return ""
	}
	extLen := int(binary.BigEndian.Uint16(p[i : i+2]))
	i += 2
	if len(p) < i+extLen {
		return ""
	}
	exts := p[i : i+extLen]
	for len(exts) >= 4 {
		typ := binary.BigEndian.Uint16(exts[0:2])
		ln := int(binary.BigEndian.Uint16(exts[2:4]))
		if len(exts) < 4+ln {
			return ""
		}
		data := exts[4 : 4+ln]
		if typ == 0x0000 {
			return parseSNIExtension(data)
		}
		exts = exts[4+ln:]
	}
	return ""
}

func parseSNIExtension(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	listLen := int(binary.BigEndian.Uint16(data[0:2]))
	data = data[2:]
	if len(data) < listLen {
		return ""
	}
	data = data[:listLen]
	for len(data) >= 3 {
		nameType := data[0]
		nameLen := int(binary.BigEndian.Uint16(data[1:3]))
		if len(data) < 3+nameLen {
			return ""
		}
		if nameType == 0 {
			return string(data[3 : 3+nameLen])
		}
		data = data[3+nameLen:]
	}
	return ""
}

func readAddr(buf []byte, r io.Reader, atyp byte) (string, uint16, error) {
	var host string
	switch atyp {
	case 1:
		if _, err := io.ReadFull(r, buf[:4]); err != nil {
			return "", 0, err
		}
		host = net.IP(buf[:4]).String()
	case 3:
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return "", 0, err
		}
		l := int(buf[0])
		if l <= 0 || l > 253 {
			return "", 0, errors.New("socks5: 域名长度非法")
		}
		if _, err := io.ReadFull(r, buf[:l]); err != nil {
			return "", 0, err
		}
		host = string(buf[:l])
	case 4:
		if _, err := io.ReadFull(r, buf[:16]); err != nil {
			return "", 0, err
		}
		host = net.IP(buf[:16]).String()
	default:
		return "", 0, errors.New("socks5: 不支持的地址类型")
	}
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return "", 0, err
	}
	return host, binary.BigEndian.Uint16(buf[:2]), nil
}

func sendRep(c net.Conn, rep byte) error {
	_, err := c.Write([]byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}

func repForDialErr(err error) byte {
	// RFC 1928: 0x05 Connection refused, 0x04 host unreachable, 0x02 not allowed, 0x01 general failure
	_ = err
	return 0x05
}

func serveUDPAssociate(c net.Conn, dial DialFunc) error {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = sendRep(c, 0x01)
		return err
	}
	defer udpConn.Close()

	addr := udpConn.LocalAddr().(*net.UDPAddr)
	resp := []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(resp[8:10], uint16(addr.Port))
	if _, err := c.Write(resp); err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, c)
		close(done)
		_ = udpConn.Close()
	}()

	buf := make([]byte, 64*1024)
	for {
		n, clientAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-done:
				return nil
			default:
				return err
			}
		}
		targetHost, targetPort, payload, err := parseUDPPacket(buf[:n])
		if err != nil {
			continue
		}
		// MVP: 只让 DNS UDP/53 直连，避免全局 TUN 因 DNS 失败而断网。
		// 其他 UDP（例如 QUIC/WebRTC）静默丢弃，通常会回退到 TCP。
		if targetPort != 53 {
			continue
		}
		go relayDNSOverTunnelTCP(udpConn, clientAddr, dial, targetHost, targetPort, payload)
	}
}

func parseUDPPacket(p []byte) (string, uint16, []byte, error) {
	if len(p) < 4 || p[0] != 0 || p[1] != 0 || p[2] != 0 {
		return "", 0, nil, errors.New("socks5: bad UDP header")
	}
	idx := 4
	var host string
	switch p[3] {
	case 1:
		if len(p) < idx+4+2 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		host = net.IP(p[idx : idx+4]).String()
		idx += 4
	case 3:
		if len(p) < idx+1 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		l := int(p[idx])
		idx++
		if len(p) < idx+l+2 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		host = string(p[idx : idx+l])
		idx += l
	case 4:
		if len(p) < idx+16+2 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		host = net.IP(p[idx : idx+16]).String()
		idx += 16
	default:
		return "", 0, nil, errors.New("socks5: bad UDP atyp")
	}
	port := binary.BigEndian.Uint16(p[idx : idx+2])
	return host, port, p[idx+2:], nil
}

func relayDNSOverTunnelTCP(server *net.UDPConn, client *net.UDPAddr, dial DialFunc, host string, port uint16, payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	type result struct {
		body []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		body, err := queryDNSTCP(dial, "8.8.8.8", payload)
		if err != nil {
			body, err = queryDNSTCP(dial, "1.1.1.1", payload)
		}
		ch <- result{body: body, err: err}
	}()
	select {
	case <-ctx.Done():
		return
	case res := <-ch:
		if res.err != nil || len(res.body) == 0 {
			return
		}
		out := buildUDPResponse(host, port, res.body)
		_, _ = server.WriteToUDP(out, client)
	}
}

func queryDNSTCP(dial DialFunc, resolver string, payload []byte) ([]byte, error) {
	c, err := dial(resolver, 53)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(payload)))
	if _, err := c.Write(lenBuf[:]); err != nil {
		return nil, err
	}
	if _, err := c.Write(payload); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(lenBuf[:])
	if n == 0 {
		return nil, errors.New("socks5: empty DNS TCP response")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c, body); err != nil {
		return nil, err
	}
	return body, nil
}

func buildUDPResponse(host string, port uint16, payload []byte) []byte {
	ip := net.ParseIP(host)
	ip4 := ip.To4()
	if ip4 != nil {
		out := make([]byte, 4+4+2+len(payload))
		out[3] = 1
		copy(out[4:8], ip4)
		binary.BigEndian.PutUint16(out[8:10], port)
		copy(out[10:], payload)
		return out
	}
	if ip16 := ip.To16(); ip16 != nil {
		out := make([]byte, 4+16+2+len(payload))
		out[3] = 4
		copy(out[4:20], ip16)
		binary.BigEndian.PutUint16(out[20:22], port)
		copy(out[22:], payload)
		return out
	}
	if len(host) > 255 {
		host = host[:255]
	}
	out := make([]byte, 4+1+len(host)+2+len(payload))
	out[3] = 3
	out[4] = byte(len(host))
	copy(out[5:5+len(host)], host)
	binary.BigEndian.PutUint16(out[5+len(host):7+len(host)], port)
	copy(out[7+len(host):], payload)
	return out
}
