//go:build windows

package proxyenv

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	hwndBroadcast      = 0xffff
	wmSettingChange    = 0x001a
	smtoAbortifhung    = 0x0002
	sendMessageTimeout = 5000
)

// Apply 写入 HKCU\Environment 中的代理变量并广播 WM_SETTINGCHANGE。
func Apply(c Config) error {
	if c.HTTPProxyURL == "" {
		return errors.New("proxyenv: HTTPProxyURL 为空")
	}
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return errors.New("proxyenv: 已处于 Apply 状态")
	}
	keys, vals := plan(c)

	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.READ|registry.WRITE)
	if err != nil {
		return fmt.Errorf("registry Environment: %w", err)
	}
	defer k.Close()

	o := make(map[string]origVal)
	for _, name := range keys {
		s, _, err := k.GetStringValue(name)
		if err == registry.ErrNotExist {
			o[name] = origVal{}
		} else if err == nil {
			o[name] = origVal{WasSet: true, Value: s}
		} else {
			o[name] = origVal{}
		}
	}

	for _, name := range keys {
		if err := k.SetStringValue(name, vals[name]); err != nil {
			restoreKeysWindows(k, o, keys)
			return err
		}
	}
	saved = o
	notifyWindowsEnv()
	return nil
}

func Restore() error {
	mu.Lock()
	defer mu.Unlock()
	if saved == nil {
		return nil
	}
	o := saved
	saved = nil
	var keys []string
	for k := range o {
		keys = append(keys, k)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.READ|registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()
	restoreKeysWindows(k, o, keys)
	notifyWindowsEnv()
	return nil
}

func restoreKeysWindows(k registry.Key, o map[string]origVal, keys []string) {
	for _, name := range keys {
		ov := o[name]
		if ov.WasSet {
			_ = k.SetStringValue(name, ov.Value)
		} else {
			_ = k.DeleteValue(name)
		}
	}
}

func notifyWindowsEnv() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(env)),
		uintptr(smtoAbortifhung),
		sendMessageTimeout,
		0,
	)
}
