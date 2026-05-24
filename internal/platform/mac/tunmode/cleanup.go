package mactun

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CleanupStale 清理异常退出后残留的 helper 与 pf 规则。
// 与 Windows 一致先发 stop；仅在确实停掉 helper 后再 down utun，避免误伤正在运行的实例。
func CleanupStale() (bool, error) {
	var changed bool
	stopped, err := stopStaleHelpers()
	if err != nil {
		return changed, err
	}
	if stopped {
		changed = true
		time.Sleep(400 * time.Millisecond)
	}
	if cleanupStaleRoutes(stopped) {
		changed = true
	}
	return changed, nil
}

func stopStaleHelpers() (bool, error) {
	dir := filepath.Join(os.TempDir(), "feizhu-client-ui")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
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
	return n > 0, nil
}

func cleanupStaleRoutes(stoppedHelper bool) bool {
	changed := false
	out, err := exec.Command("pfctl", "-a", "com.apple/feizhu", "-sr").Output()
	hasRules := err == nil && strings.TrimSpace(string(out)) != ""
	if hasRules {
		changed = true
	}
	_ = exec.Command("pfctl", "-a", "com.apple/feizhu", "-F", "all").Run()

	if stoppedHelper {
		for _, dev := range feizhuTUNDevices() {
			_ = exec.Command("ifconfig", dev, "down").Run()
		}
		changed = true
	}
	return changed
}

func feizhuTUNDevices() []string {
	out, err := exec.Command("ifconfig").Output()
	if err != nil {
		return nil
	}
	var devs []string
	for _, block := range strings.Split(string(out), "\n\n") {
		if !strings.Contains(block, "inet 10.255.0.1 ") && !strings.Contains(block, "inet 10.255.0.1\n") {
			continue
		}
		name := strings.TrimSuffix(strings.SplitN(block, ":", 2)[0], ":")
		if strings.HasPrefix(name, "utun") {
			devs = append(devs, name)
		}
	}
	return devs
}
