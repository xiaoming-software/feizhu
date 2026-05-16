//go:build !darwin && !linux && !windows

package proxyenv

import (
	"fmt"
	"runtime"
)

// Apply 当前平台不支持用户级代理环境注入。
func Apply(c Config) error {
	_ = c
	return fmt.Errorf("proxyenv: GOOS=%s 不支持 -auto-env，请加 -auto-env=false 并用 -print-proxy-env 手动 export", runtime.GOOS)
}

// Restore 无操作。
func Restore() error {
	return nil
}
