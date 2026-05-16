//go:build linux

package proxyenv

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Apply 使用 systemctl --user set-environment（需已登录的 systemd user session）。
func Apply(c Config) error {
	if c.HTTPProxyURL == "" {
		return errors.New("proxyenv: HTTPProxyURL 为空")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("proxyenv: 未找到 systemctl: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return errors.New("proxyenv: 已处于 Apply 状态")
	}

	keys, vals := plan(c)
	o, err := systemdUserSnapshot(keys)
	if err != nil {
		return err
	}

	args := []string{"--user", "set-environment"}
	for _, k := range keys {
		args = append(args, fmt.Sprintf("%s=%s", k, vals[k]))
	}
	cmd := exec.Command("systemctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl set-environment: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	saved = o
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
	// 先 unset 再写回旧值，避免旧值被新值污染逻辑
	unsetArgs := []string{"--user", "unset-environment"}
	unsetArgs = append(unsetArgs, keys...)
	_ = exec.Command("systemctl", unsetArgs...).Run()

	for _, k := range keys {
		ov := o[k]
		if ov.WasSet {
			cmd := exec.Command("systemctl", "--user", "set-environment", fmt.Sprintf("%s=%s", k, ov.Value))
			_ = cmd.Run()
		}
	}
	return nil
}

func systemdUserSnapshot(keys []string) (map[string]origVal, error) {
	cmd := exec.Command("systemctl", "--user", "show-environment")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl show-environment: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(string(out), "\n")
	env := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		env[line[:i]] = line[i+1:]
	}
	o := make(map[string]origVal)
	for _, k := range keys {
		v, ok := env[k]
		o[k] = origVal{WasSet: ok, Value: v}
	}
	return o, nil
}
