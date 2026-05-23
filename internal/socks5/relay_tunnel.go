package socks5

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// RelayTCPThrough 将一条已建立的 TCP（透明代理侧）经 dial 转发到远端，等价于本地 SOCKS CONNECT 但不经网络回环。
// 对 Clash fake-ip（198.18/15）会从首包解析 SNI/Host 再拨号。
func RelayTCPThrough(client net.Conn, dstHost string, dstPort uint16, dial DialFunc) error {
	dialHost := dstHost
	var prefix []byte

	if IsFakeIPv4(dstHost) && (dstPort == 443 || dstPort == 80) {
		_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
		buf := make([]byte, 8192)
		n, err := client.Read(buf)
		_ = client.SetReadDeadline(time.Time{})
		if err != nil {
			return fmt.Errorf("socks5: fake-ip 嗅探: %w", err)
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

	remote, err := dial(dialHost, dstPort)
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

// IsFakeIPv4 是否为 Clash 等常用的 fake-ip 段。
func IsFakeIPv4(host string) bool {
	return isFakeIPv4(host)
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
