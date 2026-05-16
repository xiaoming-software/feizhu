//go:build !darwin && !windows

package sysproxy

import (
	"fmt"
	"runtime"
)

// Apply 在非 macOS/Windows 上不可用。
func Apply(httpHost, httpPort, socksHost, socksPort, networkService string) error {
	_ = networkService
	_ = socksHost
	_ = socksPort
	return fmt.Errorf("自动系统代理不支持 GOOS=%s；请手动将系统代理指向 HTTP %s:%s，或使用 -auto-proxy=false", runtime.GOOS, httpHost, httpPort)
}

// Restore 无操作。
func Restore() error {
	return nil
}
