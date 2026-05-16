//go:build windows

package sysproxy

import (
	"fmt"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var mu sync.Mutex
var saved *winState
var wininet = syscall.NewLazyDLL("wininet.dll")
var procInternetSetOptionW = wininet.NewProc("InternetSetOptionW")

type winState struct {
	ProxyEnable   uint32
	ProxyServer   string
	ProxyOverride string
	HadOverride   bool
}

// Apply 设置当前用户「Internet 设置」代理。若提供 SOCKS 地址则使用分号形式分别指定 http/https/socks。
func Apply(httpHost, httpPort, socksHost, socksPort, _ string) error {
	mu.Lock()
	defer mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.READ|registry.WRITE)
	if err != nil {
		return fmt.Errorf("打开注册表 Internet Settings: %w", err)
	}
	defer k.Close()

	st := &winState{}
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		st.ProxyEnable = uint32(v)
	}
	if s, _, err := k.GetStringValue("ProxyServer"); err == nil {
		st.ProxyServer = s
	}
	if s, _, err := k.GetStringValue("ProxyOverride"); err == nil {
		st.ProxyOverride = s
		st.HadOverride = true
	}
	saved = st

	proxyVal := buildProxyServer(httpHost, httpPort, socksHost, socksPort)
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		_ = restoreWinLocked(k)
		return fmt.Errorf("ProxyEnable: %w", err)
	}
	if err := k.SetStringValue("ProxyServer", proxyVal); err != nil {
		_ = restoreWinLocked(k)
		return fmt.Errorf("ProxyServer: %w", err)
	}
	notifyProxySettingsChanged()
	return nil
}

func buildProxyServer(httpHost, httpPort, socksHost, socksPort string) string {
	if socksHost != "" && socksPort != "" {
		return fmt.Sprintf("http=%s:%s;https=%s:%s;socks=%s:%s",
			httpHost, httpPort, httpHost, httpPort, socksHost, socksPort)
	}
	return fmt.Sprintf("%s:%s", httpHost, httpPort)
}

// Restore 恢复 Apply 前保存的代理相关注册表项。
func Restore() error {
	mu.Lock()
	defer mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.READ|registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()
	return restoreWinLocked(k)
}

func restoreWinLocked(k registry.Key) error {
	if saved == nil {
		return nil
	}
	st := saved
	saved = nil

	if err := k.SetDWordValue("ProxyEnable", st.ProxyEnable); err != nil {
		return err
	}
	if st.ProxyServer != "" {
		if err := k.SetStringValue("ProxyServer", st.ProxyServer); err != nil {
			return err
		}
	} else {
		_ = k.DeleteValue("ProxyServer")
	}
	if st.HadOverride {
		_ = k.SetStringValue("ProxyOverride", st.ProxyOverride)
	}
	notifyProxySettingsChanged()
	return nil
}

func notifyProxySettingsChanged() {
	r0, _, _ := procInternetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_ = r0
	r1, _, _ := procInternetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
	_ = r1
}
