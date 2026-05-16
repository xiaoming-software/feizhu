//go:build unix || windows

package curlrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	beginMarker = "# --- feizhu-client begin (auto, do not edit) ---"
	endMarker   = "# --- feizhu-client end ---"
)

var mu sync.Mutex
var applied bool

// Apply 在用户主目录下的 .curlrc 中写入带标记的一段配置，使 curl（含 Windows cmd 下）默认走本地 HTTP 代理。
func Apply(proxyURL string) error {
	if proxyURL == "" {
		return fmt.Errorf("curlrc: 代理 URL 为空")
	}
	mu.Lock()
	defer mu.Unlock()
	if applied {
		return fmt.Errorf("curlrc: 已应用")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	legacyInc := filepath.Join(home, ".feizhu", "curlrc.include")
	_ = os.Remove(legacyInc)

	curlrcPath := filepath.Join(home, ".curlrc")
	block := beginMarker + "\n" + buildBlock(proxyURL) + endMarker + "\n"

	old, _ := os.ReadFile(curlrcPath)
	newBody := mergeBlock(string(old), block)
	if err := os.WriteFile(curlrcPath, []byte(newBody), 0600); err != nil {
		return err
	}
	applied = true
	return nil
}

func buildBlock(proxyURL string) string {
	q := fmt.Sprintf("%q", proxyURL)
	lines := []string{
		"# 以下由 feizhu-client 管理；退出 client 后整段删除",
		`noproxy = "127.0.0.1,localhost,localhost.localdomain"`,
		"proxy = " + q,
	}
	return strings.Join(lines, "\n") + "\n"
}

// Restore 从用户主目录 .curlrc 去掉 feizhu 片段。
func Restore() error {
	mu.Lock()
	defer mu.Unlock()
	if !applied {
		return nil
	}
	applied = false
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(home, ".feizhu", "curlrc.include"))
	curlrcPath := filepath.Join(home, ".curlrc")
	if old, err := os.ReadFile(curlrcPath); err == nil {
		newBody := strings.TrimRight(stripBlock(string(old)), "\n")
		if strings.TrimSpace(newBody) == "" {
			_ = os.Remove(curlrcPath)
		} else {
			_ = os.WriteFile(curlrcPath, []byte(newBody+"\n"), 0600)
		}
	}
	return nil
}

func mergeBlock(existing, block string) string {
	cleaned := strings.TrimRight(stripBlock(existing), "\n")
	if cleaned == "" {
		return block
	}
	return cleaned + "\n\n" + block
}

func stripBlock(s string) string {
	out := s
	for {
		i := strings.Index(out, beginMarker)
		if i < 0 {
			return out
		}
		after := out[i+len(beginMarker):]
		j := strings.Index(after, endMarker)
		if j < 0 {
			return out[:i] + after
		}
		end := i + len(beginMarker) + j + len(endMarker)
		for end < len(out) && (out[end] == '\n' || out[end] == '\r') {
			end++
		}
		out = out[:i] + out[end:]
	}
}
