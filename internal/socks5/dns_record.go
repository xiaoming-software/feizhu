package socks5

import (
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"
)

var dnsResolveCache dnsNameCache

type dnsNameCache struct {
	mu      sync.Mutex
	entries []dnsCacheEntry
}

type dnsCacheEntry struct {
	domain string
	ips    []string
	at     time.Time
}

func recordDNSResponse(payload []byte) {
	if len(payload) < 12 {
		return
	}
	qdcount := int(binary.BigEndian.Uint16(payload[4:6]))
	ancount := int(binary.BigEndian.Uint16(payload[6:8]))
	off := 12
	var qname string
	for i := 0; i < qdcount; i++ {
		name, n := readDNSName(payload, off)
		if n < 0 {
			return
		}
		if i == 0 {
			qname = normalizeDNSName(name)
		}
		off += n + 4
	}
	if qname == "" {
		return
	}
	var reals []string
	for i := 0; i < ancount; i++ {
		_, n := readDNSName(payload, off)
		if n < 0 {
			break
		}
		off += n
		if off+10 > len(payload) {
			break
		}
		typ := binary.BigEndian.Uint16(payload[off : off+2])
		rdlen := int(binary.BigEndian.Uint16(payload[off+8 : off+10]))
		rdataOff := off + 10
		off = rdataOff + rdlen
		if typ != 1 || rdlen != 4 || rdataOff+4 > len(payload) {
			continue
		}
		ip := net.IP(payload[rdataOff : rdataOff+4]).String()
		parsed := net.ParseIP(ip)
		if parsed == nil || isFakeIPv4(ip) || parsed.IsPrivate() || parsed.IsLoopback() {
			continue
		}
		reals = append(reals, ip)
	}
	if len(reals) == 0 {
		return
	}
	dnsResolveCache.add(qname, reals)
}

func (c *dnsNameCache) add(domain string, ips []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, dnsCacheEntry{domain: domain, ips: append([]string(nil), ips...), at: time.Now()})
	if len(c.entries) > 64 {
		c.entries = c.entries[len(c.entries)-64:]
	}
}

// latestPublicIP 返回最近一次公网 A 记录（指纹浏览器解析代理域名后通常会立刻建连）。
func (c *dnsNameCache) latestPublicIP() (domain, ip string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for i := len(c.entries) - 1; i >= 0; i-- {
		e := c.entries[i]
		if now.Sub(e.at) > 2*time.Minute {
			continue
		}
		if len(e.ips) == 0 {
			continue
		}
		return e.domain, e.ips[0], true
	}
	return "", "", false
}

func isProxyPort(port uint16) bool {
	switch port {
	case 1000, 2000, 1080, 1081, 8080, 8888, 7890, 7891:
		return true
	default:
		return false
	}
}

func resolveDialHost(host string, port uint16) string {
	if !isFakeIPv4(host) {
		return host
	}
	if real, ok := fakeIPTable.lookup(host); ok {
		return real
	}
	if isProxyPort(port) {
		if domain, real, ok := dnsResolveCache.latestPublicIP(); ok {
			fakeIPTable.bind(host, real)
			log.Printf("[socks5] fake-ip %s:%d 已用最近 DNS 解析 %s -> %s 还原", host, port, domain, real)
			return real
		}
	}
	return host
}
