package socks5

import (
	"fmt"
	"log"
	"net"
	"time"
)

// 保留 bridge.go 供 WinDivert 透明代理经本地 SOCKS5 转发（与 macOS tun2socks 路径一致）。

// RelayTransparent 经本地 SOCKS5 端口桥接（macOS tun2socks / Windows WinDivert 等场景）。
func RelayTransparent(client net.Conn, socksAddr, dstHost string, dstPort uint16) error {
	log.Printf("[TUN-trace] RelayTransparent 开始 dst=%s:%d via SOCKS %s", dstHost, dstPort, socksAddr)
	dialHost := dstHost
	var prefix []byte

	if isFakeIPv4(dstHost) && (dstPort == 443 || dstPort == 80) {
		log.Printf("[TUN-trace] fake-ip 嗅探 %s:%d", dstHost, dstPort)
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
				log.Printf("[TUN-trace] fake-ip SNI=%s (原 %s)", h, dstHost)
				dialHost = h
			}
		} else if h := parseHTTPHost(prefix); h != "" {
			log.Printf("[TUN-trace] fake-ip Host=%s (原 %s)", h, dstHost)
			dialHost = h
		}
	}

	remote, err := DialConnect(socksAddr, dialHost, dstPort)
	if err != nil {
		log.Printf("[TUN-trace] SOCKS CONNECT 失败 %s:%d via %s: %v", dialHost, dstPort, socksAddr, err)
		return err
	}
	defer remote.Close()
	log.Printf("[TUN-trace] SOCKS CONNECT 成功 %s:%d", dialHost, dstPort)

	if len(prefix) > 0 {
		if _, err := remote.Write(prefix); err != nil {
			return err
		}
	}
	err = relayTCPPair(client, remote)
	if err != nil {
		log.Printf("[TUN-trace] RelayTransparent 结束 %s:%d err=%v", dialHost, dstPort, err)
	}
	return err
}
