package socks5

import (
	"strings"
	"sync"
)

var fakeIPTable = extFakeMap{fakeToReal: make(map[string]string)}

// extFakeMap 记录外部 fake-ip（公司 DNS 等）到真实公网 IP 的会话内映射。
type extFakeMap struct {
	mu         sync.RWMutex
	fakeToReal map[string]string
}

func (m *extFakeMap) bind(fake, real string) {
	if fake == "" || real == "" || isFakeIPv4(real) {
		return
	}
	m.mu.Lock()
	m.fakeToReal[fake] = real
	m.mu.Unlock()
}

func (m *extFakeMap) lookup(fake string) (string, bool) {
	m.mu.RLock()
	real, ok := m.fakeToReal[fake]
	m.mu.RUnlock()
	return real, ok
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
