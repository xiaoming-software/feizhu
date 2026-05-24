//go:build darwin

package clientrunner

import "log"

func logTUNPlatformMessages(_ bool) {
	log.Println("[TUN] macOS：公网出站 TCP 与 DNS UDP/53 → tun2socks → 127.0.0.1:7891 → feizhu TLS。")
}
