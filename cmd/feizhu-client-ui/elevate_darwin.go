//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func startElevatedSelf(configPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	logFile := helperLogFile(configPath)
	launcherPath := strings.TrimSuffix(configPath, ".json") + ".sh"
	launcher := fmt.Sprintf("#!/bin/sh\nexec %s -%s -config %s >> %s 2>&1\n",
		shellQuote(exe),
		helperFlag,
		shellQuote(configPath),
		shellQuote(logFile),
	)
	if err := os.WriteFile(launcherPath, []byte(launcher), 0700); err != nil {
		return 0, fmt.Errorf("写入 helper launcher: %w", err)
	}
	cmdline := strings.Join([]string{
		"/bin/sh",
		shellQuote(launcherPath),
		">>", shellQuote(logFile),
		"2>&1",
		"&",
		"echo $!",
	}, " ")
	script := fmt.Sprintf("do shell script %s with administrator privileges", appleScriptQuote(cmdline))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return 0, fmt.Errorf("请求管理员授权失败或已取消: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("解析 helper PID 失败: %w", err)
	}
	return pid, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func appleScriptQuote(s string) string {
	return strconv.Quote(s)
}

func helperLogFile(configPath string) string {
	b, err := os.ReadFile(configPath)
	if err == nil {
		var hc helperConfig
		if json.Unmarshal(b, &hc) == nil && hc.LogFile != "" {
			return hc.LogFile
		}
	}
	return filepath.Join(filepath.Dir(configPath), strings.TrimSuffix(filepath.Base(configPath), ".json")+".log")
}
