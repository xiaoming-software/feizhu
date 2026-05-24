package socks5

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// RelayTCPThrough 将一条已建立的 TCP（透明代理侧）经 dial 转发到远端，等价于本地 SOCKS CONNECT 但不经网络回环。
// 对 Clash fake-ip（198.18/15）会从首包解析 SNI/Host 再拨号。
func RelayTCPThrough(client net.Conn, dstHost string, dstPort uint16, dial DialFunc) error {
	log.Printf("[TUN-trace] RelayTCPThrough 开始 dst=%s:%d", dstHost, dstPort)
	dialHost := dstHost
	prefix, err := readRelayPrefix(client, dstHost, dstPort)
	if err != nil {
		log.Printf("[TUN-trace] RelayTCPThrough 等待首包失败 %s:%d: %v", dstHost, dstPort, err)
		return err
	}
	if len(prefix) > 0 {
		log.Printf("[TUN-trace] RelayTCPThrough 收到首包 %s:%d len=%d lead=% x", dstHost, dstPort, len(prefix), prefixLead(prefix))
	}
	if IsFakeIPv4(dstHost) && (dstPort == 443 || dstPort == 80) {
		if dstPort == 443 {
			if h := parseTLSSNI(prefix); h != "" {
				dialHost = h
			}
		} else if h := parseHTTPHost(prefix); h != "" {
			dialHost = h
		}
	}

	remote, err := dial(dialHost, dstPort)
	if err != nil {
		log.Printf("[TUN-trace] RelayTCPThrough 拨号失败 %s:%d: %v", dialHost, dstPort, err)
		return err
	}
	defer remote.Close()
	log.Printf("[TUN-trace] RelayTCPThrough 拨号成功 %s:%d", dialHost, dstPort)

	if len(prefix) > 0 {
		if _, err := remote.Write(prefix); err != nil {
			return err
		}
	}
	err = relayTCPPair(client, remote)
	if err != nil {
		log.Printf("[TUN-trace] RelayTCPThrough 结束 %s:%d err=%v", dialHost, dstPort, err)
	}
	return err
}

// IsFakeIPv4 是否为 Clash 等常用的 fake-ip 段。
func IsFakeIPv4(host string) bool {
	return isFakeIPv4(host)
}

func readRelayPrefix(client net.Conn, dstHost string, dstPort uint16) ([]byte, error) {
	wait := 30 * time.Second
	if IsFakeIPv4(dstHost) && (dstPort == 443 || dstPort == 80) {
		wait = 8 * time.Second
	}
	_ = client.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 8192)
	n, err := client.Read(buf)
	_ = client.SetReadDeadline(time.Time{})
	if err != nil {
		return nil, fmt.Errorf("socks5: 等待首包: %w", err)
	}
	return buf[:n], nil
}

func prefixLead(b []byte) []byte {
	if len(b) > 16 {
		return b[:16]
	}
	return b
}

func relayTCPPair(a, b net.Conn) error {
	var wg sync.WaitGroup
	wg.Add(2)
	errc := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, err := io.Copy(b, a)
		errc <- err
	}()
	go func() {
		defer wg.Done()
		_, err := io.Copy(a, b)
		errc <- err
	}()
	wg.Wait()
	select {
	case err := <-errc:
		if err != nil && !isClosedErr(err) {
			return err
		}
	default:
	}
	return nil
}

func isClosedErr(err error) bool {
	if err == nil || err == io.EOF {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "closed") ||
		strings.Contains(s, "reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "forcibly closed")
}
