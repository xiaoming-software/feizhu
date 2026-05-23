//go:build windows

package proxyenv

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// RevertStaleFeizhuEnv 清除用户环境变量里残留的飞猪代理设置。
func RevertStaleFeizhuEnv() (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return false, nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.READ|registry.WRITE)
	if err != nil {
		return false, fmt.Errorf("registry Environment: %w", err)
	}
	defer k.Close()
	changed := false
	for _, name := range feizhuEnvKeys() {
		v, _, err := k.GetStringValue(name)
		if err != nil || !isFeizhuEnvValue(v) {
			continue
		}
		if err := k.DeleteValue(name); err != nil {
			return changed, err
		}
		changed = true
	}
	if changed {
		notifyWindowsEnv()
	}
	return changed, nil
}
