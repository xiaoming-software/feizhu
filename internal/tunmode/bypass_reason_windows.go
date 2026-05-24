//go:build windows

package tunmode

import "net"

func bypassReasonIPv4(ip net.IP, explicit map[uint32]struct{}) string {
	ip4 := ip.To4()
	if ip4 == nil {
		return "non-ipv4"
	}
	if _, ok := explicit[ipv4ToUint32(ip4)]; ok {
		return "explicit-bypass(server/dns/config)"
	}
	if ip4.IsLoopback() {
		return "loopback"
	}
	if ip4.IsLinkLocalUnicast() {
		return "link-local"
	}
	if ip4.IsPrivate() && !isClashFakeIPv4(ip4) {
		return "private-rfc1918"
	}
	if ip4[0] >= 224 {
		return "multicast/broadcast"
	}
	return ""
}
