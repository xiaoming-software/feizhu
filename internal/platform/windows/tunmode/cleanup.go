package wintun

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CleanupStale 向临时目录下所有 helper stop 文件发停止信号，结束可能残留的提权 TUN 进程。
func CleanupStale() (bool, error) {
	dir := filepath.Join(os.TempDir(), "feizhu-client-ui")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return cleanupStaleRoutes(), nil
		}
		return false, err
	}
	var n int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".stop") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.WriteFile(path, []byte("stop\n"), 0600); err == nil {
			n++
		}
	}
	routeCleaned := cleanupStaleRoutes()
	return n > 0 || routeCleaned, nil
}

func cleanupStaleRoutes() bool {
	changed := false
	for _, args := range [][]string{
		{"route", "delete", "0.0.0.0", "mask", "128.0.0.0"},
		{"route", "delete", "128.0.0.0", "mask", "128.0.0.0"},
	} {
		if exec.Command(args[0], args[1:]...).Run() == nil {
			changed = true
		}
	}
	script := `Get-NetRoute -InterfaceAlias 'FeizhuTunnel' -ErrorAction SilentlyContinue | Where-Object { $_.DestinationPrefix -in @('0.0.0.0/1','128.0.0.0/1') } | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue`
	if exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Run() == nil {
		changed = true
	}
	return changed
}
