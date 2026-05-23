package socks5

// 保留 bridge.go 供 RelayTransparent（经本地 SOCKS 端口）使用；Windows 透明代理优先用 RelayTCPThrough。

import (
	"fmt"
	"net"
	"time"
)

// RelayTransparent 经本地 SOCKS5 端口桥接（macOS tun2socks 等场景）。
func RelayTransparent(client net.Conn, socksAddr, dstHost string, dstPort uint16) error {
	dialHost := dstHost
	var prefix []byte

	if isFakeIPv4(dstHost) && (dstPort == 443 || dstPort == 80) {
		_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
		buf := make([]byte, 8192)
		n, err := client.Read(buf)
		_ = client.SetReadDeadline(time.Time{})
		if err != nil {
			return fmt.Errorf("socks5: fake-ip 嗅探首包: %w", err)
		}
		prefix = buf[:n]
		if dstPort == 443 {
			if h := parseTLSSNI(prefix); h != "" {
				dialHost = h
			}
		} else if h := parseHTTPHost(prefix); h != "" {
			dialHost = h
		}
	}

	remote, err := DialConnect(socksAddr, dialHost, dstPort)
	if err != nil {
		return err
	}
	defer remote.Close()

	if len(prefix) > 0 {
		if _, err := remote.Write(prefix); err != nil {
			return err
		}
	}
	return relayTCPPair(client, remote)
}
