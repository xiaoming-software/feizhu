//go:build darwin

package clientrunner

import "net/url"

// macOS：不为上级代理 IP 旁路；所有被 pf 拦下的公网 TCP 均进 TUN → feizhu TLS。
func tunBypassIPsForPlatform(*url.URL) []string {
	return nil
}

func tunBootstrapBypassHosts(*url.URL) []string { return nil }
