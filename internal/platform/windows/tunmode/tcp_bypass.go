package wintun

import (
	"net"
	"strings"
)

func hostFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func ipv4ToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

func isClashFakeIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19)
}

func buildTCPBypassSet(cfg Config) map[uint32]struct{} {
	set := make(map[uint32]struct{})
	add := func(ip net.IP) {
		if ip4 := ip.To4(); ip4 != nil {
			set[ipv4ToUint32(ip4)] = struct{}{}
		}
	}
	for _, ip := range resolveIPv4Host(hostFromAddr(cfg.ServerAddr)) {
		add(ip)
	}
	for _, s := range cfg.BypassIPs {
		if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
			add(ip)
		}
	}
	return set
}

func shouldBypassIPv4(ip net.IP, explicit map[uint32]struct{}) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return true
	}
	if _, ok := explicit[ipv4ToUint32(ip4)]; ok {
		return true
	}
	if ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
		return true
	}
	if ip4.IsPrivate() && !isClashFakeIPv4(ip4) {
		return true
	}
	if ip4[0] >= 224 {
		return true
	}
	return false
}
