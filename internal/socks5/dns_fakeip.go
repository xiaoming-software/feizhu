package socks5

import (
	"strings"
)

// readDNSName 解析 DNS 报文中的域名（支持压缩指针）。
func readDNSName(msg []byte, off int) (string, int) {
	if off >= len(msg) {
		return "", -1
	}
	start := off
	var parts []string
	for jumps := 0; off < len(msg) && jumps < 8; {
		l := int(msg[off])
		if l == 0 {
			off++
			break
		}
		if l&0xC0 == 0xC0 {
			if off+1 >= len(msg) {
				return "", -1
			}
			ptr := int(l&0x3f)<<8 | int(msg[off+1])
			suffix, _ := readDNSName(msg, ptr)
			if suffix != "" {
				parts = append(parts, suffix)
			}
			off += 2
			break
		}
		off++
		if off+l > len(msg) {
			return "", -1
		}
		parts = append(parts, string(msg[off:off+l]))
		off += l
		jumps++
	}
	if len(parts) == 0 {
		return "", off - start
	}
	return strings.Join(parts, "."), off - start
}
