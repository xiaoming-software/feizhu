//go:build windows

package sysproxy

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// RevertStaleFeizhuProxy 在进程未持有 saved 快照时，关闭仍指向飞猪本地端口的系统代理（异常退出残留）。
func RevertStaleFeizhuProxy() (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return false, nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.READ|registry.WRITE)
	if err != nil {
		return false, fmt.Errorf("打开 Internet Settings: %w", err)
	}
	defer k.Close()

	enable, _, _ := k.GetIntegerValue("ProxyEnable")
	server, _, _ := k.GetStringValue("ProxyServer")
	if enable == 0 || !isFeizhuProxyServerValue(server) {
		return false, nil
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return false, err
	}
	_ = k.DeleteValue("ProxyServer")
	notifyProxySettingsChanged()
	return true, nil
}
