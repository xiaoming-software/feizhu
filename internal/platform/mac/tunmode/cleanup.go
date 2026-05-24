package mactun

import (
	"os/exec"
	"strings"
)

// CleanupStale 清理异常退出后可能残留的 TUN/pf 规则。
func CleanupStale() (bool, error) {
	out, err := exec.Command("pfctl", "-a", "com.apple/feizhu", "-sr").Output()
	hasRules := err == nil && strings.TrimSpace(string(out)) != ""
	_ = exec.Command("pfctl", "-a", "com.apple/feizhu", "-F", "all").Run()
	return hasRules, nil
}
