//go:build darwin

package proxyenv

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Apply 通过 launchctl setenv 写入当前登录用户下新进程可继承的代理变量；失败则尽力回滚。
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
	o := make(map[string]origVal, len(keys))
	for _, k := range keys {
		v, ok := launchGetenv(k)
		o[k] = origVal{WasSet: ok, Value: v}
	}

	for _, k := range keys {
		if err := launchSetenv(k, vals[k]); err != nil {
			restoreFrom(o, keys)
			return err
		}
	}
	saved = o
	return nil
}

// Restore 用 launchctl unsetenv/setenv 恢复 Apply 之前的值。
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
	restoreFrom(o, keys)
	return nil
}

func restoreFrom(o map[string]origVal, keys []string) {
	for _, k := range keys {
		ov, ok := o[k]
		if !ok {
			continue
		}
		if ov.WasSet {
			_ = launchSetenv(k, ov.Value)
		} else {
			_ = launchUnsetenv(k)
		}
	}
}

func launchGetenv(k string) (val string, exists bool) {
	var stderr strings.Builder
	cmd := exec.Command("launchctl", "getenv", k)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	if s == "" || strings.EqualFold(s, "(null)") {
		return "", false
	}
	return s, true
}

func launchSetenv(k, v string) error {
	cmd := exec.Command("launchctl", "setenv", k, v)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func launchUnsetenv(k string) error {
	cmd := exec.Command("launchctl", "unsetenv", k)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return nil
}
