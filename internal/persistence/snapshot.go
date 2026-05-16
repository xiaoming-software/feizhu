// Package persistence 在进程被 kill / 强杀时仍能还原系统代理：Apply 前把快照写入磁盘，
// 下次启动若发现记录中的 PID 已不存在则按快照恢复。
package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dirName    = ".feizhu"
	markerName = "client-proxy-active.json"
)

// AppSnapshot 一次 feizhu-client 运行期间对系统/环境的修改记录。
type AppSnapshot struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	AutoProxy  bool      `json:"auto_proxy"`
	AutoEnv    bool      `json:"auto_env"`
	AutoCurlrc bool      `json:"auto_curlrc"`

	Sysproxy *SysproxySnap `json:"sysproxy,omitempty"`
	Proxyenv *ProxyenvSnap `json:"proxyenv,omitempty"`
	Curlrc   *CurlrcSnap   `json:"curlrc,omitempty"`
}

// SysproxySnap 平台相关；仅设置与 JSON 标签匹配的字段。
type SysproxySnap struct {
	Windows *WinSysproxy `json:"windows,omitempty"`
	Darwin  *DarwinSysproxy `json:"darwin,omitempty"`
}

type WinSysproxy struct {
	ProxyEnable   uint32 `json:"proxy_enable"`
	ProxyServer   string `json:"proxy_server"`
	ProxyOverride string `json:"proxy_override"`
	HadOverride   bool   `json:"had_override"`
}

type DarwinSysproxy struct {
	Service string       `json:"service"`
	Web     DarwinProxy  `json:"web"`
	Secure  DarwinProxy  `json:"secure"`
	Socks   DarwinProxy  `json:"socks"`
}

type DarwinProxy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    string `json:"port"`
}

// ProxyenvSnap 用户级代理环境变量（launchctl / 注册表 / systemd）。
type ProxyenvSnap struct {
	Vars map[string]EnvOrig `json:"vars"`
}

type EnvOrig struct {
	WasSet bool   `json:"was_set"`
	Value  string `json:"value"`
}

// CurlrcSnap ~/.curlrc 在 Apply 之前的全文（用于强杀后还原）。
type CurlrcSnap struct {
	PriorContent string `json:"prior_content"`
	HadFile      bool   `json:"had_file"`
}

func stateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

func markerPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, markerName), nil
}

// SavePending 在 Apply 系统代理/环境/curlrc 之前调用；写入当前 PID。
func SavePending(s *AppSnapshot) error {
	if s == nil {
		return errors.New("persistence: 快照为空")
	}
	s.PID = os.Getpid()
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now().UTC()
	}
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := markerPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

// ClearPending 正常退出并 Restore 后删除标记文件。
func ClearPending() {
	path, err := markerPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

func loadPending() (*AppSnapshot, error) {
	path, err := markerPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s AppSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	return &s, nil
}

// RecoverStaleApplication 应在 client 启动后、本次 Apply 之前调用。
// 若磁盘上留有「上次运行 PID」且该进程已不存在，则按快照还原代理设置。
func RecoverStaleApplication() error {
	s, err := loadPending()
	if err != nil || s == nil {
		return err
	}
	if s.PID > 0 && processAlive(s.PID) {
		// 仍标记为「本程序占用」；若用户同时开两个实例，后启动者不会误还原前者。
		return nil
	}
	var errs []string
	if s.AutoCurlrc && s.Curlrc != nil {
		if err := restoreCurlrc(s.Curlrc); err != nil {
			errs = append(errs, "curlrc: "+err.Error())
		}
	}
	if s.AutoEnv && s.Proxyenv != nil {
		if err := restoreProxyenv(s.Proxyenv); err != nil {
			errs = append(errs, "proxyenv: "+err.Error())
		}
	}
	if s.AutoProxy && s.Sysproxy != nil {
		if err := restoreSysproxy(s.Sysproxy); err != nil {
			errs = append(errs, "sysproxy: "+err.Error())
		}
	}
	ClearPending()
	if len(errs) > 0 {
		return fmt.Errorf("persistence: 孤儿代理还原: %s", strings.Join(errs, "; "))
	}
	return nil
}

// FormatStaleHint 供 UI/CLI 在日志中提示用户。
func FormatStaleHint() string {
	path, err := markerPath()
	if err != nil {
		return ""
	}
	s, err := loadPending()
	if err != nil || s == nil || (s.PID > 0 && processAlive(s.PID)) {
		return ""
	}
	return fmt.Sprintf("[代理] 检测到上次进程异常退出（PID %d），已尝试从 %s 还原系统代理。", s.PID, path)
}
