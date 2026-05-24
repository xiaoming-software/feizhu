package wintun

import (
	"os"
	"path/filepath"
	"strings"
)

// CleanupStale 向临时目录下所有 helper stop 文件发停止信号，结束可能残留的提权 TUN 进程。
func CleanupStale() (bool, error) {
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
