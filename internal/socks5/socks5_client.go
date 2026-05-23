package socks5

import (
	"fmt"
	"io"
	"net"
	"time"
)

// DialConnect 向 SOCKS5 代理发起 CONNECT。
func DialConnect(socksAddr, host string, port uint16) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", socksAddr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("socks5: 连接 SOCKS %s: %w", socksAddr, err)
	}
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		c.Close()
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		c.Close()
		return nil, err
	}
	if buf[0] != 5 || buf[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5: 握手失败 %v", buf)
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
		if len(host) > 255 {
			c.Close()
			return nil, fmt.Errorf("socks5: 域名过长")
		}
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))

	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	rep := make([]byte, 4)
	if _, err := io.ReadFull(c, rep); err != nil {
		c.Close()
		return nil, err
	}
	if rep[0] != 5 || rep[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5: CONNECT %s:%d 被拒", host, port)
	}
	switch rep[3] {
	case 1:
		if _, err := io.ReadFull(c, make([]byte, 6)); err != nil {
			c.Close()
			return nil, err
		}
	case 3:
		ln := make([]byte, 1)
		if _, err := io.ReadFull(c, ln); err != nil {
			c.Close()
			return nil, err
		}
		if _, err := io.ReadFull(c, make([]byte, int(ln[0])+2)); err != nil {
			c.Close()
			return nil, err
		}
	case 4:
		if _, err := io.ReadFull(c, make([]byte, 18)); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}
