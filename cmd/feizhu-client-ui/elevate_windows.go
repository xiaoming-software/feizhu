//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func startElevatedSelf(configPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	args := []string{"-" + helperFlag, "-config", configPath}
	var psArgs []string
	for _, arg := range args {
		psArgs = append(psArgs, psQuote(arg))
	}
	script := fmt.Sprintf(
		"$p = Start-Process -FilePath %s -ArgumentList @(%s) -Verb RunAs -PassThru; if ($p) { $p.Id }",
		psQuote(exe),
		strings.Join(psArgs, ","),
	)
	out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
	if err != nil {
		return 0, fmt.Errorf("请求管理员授权失败或已取消: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("解析 helper PID 失败: %w", err)
	}
	return pid, nil
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
