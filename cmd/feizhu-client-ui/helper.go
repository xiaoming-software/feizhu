package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/feizhu/feizhu/internal/clientrunner"
	"github.com/feizhu/feizhu/internal/curlrc"
)

const helperFlag = "feizhu-helper-client"

type helperConfig struct {
	Client   clientrunner.Config `json:"client"`
	StopFile string              `json:"stop_file"`
	LogFile  string              `json:"log_file"`
}

type elevatedProcess struct {
	PID      int
	StopFile string
	LogFile  string
}

func runHelperIfRequested() bool {
	fs := flag.NewFlagSet("feizhu-client-ui-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "helper config")
	isHelper := fs.Bool(helperFlag, false, "run hidden helper")
	_ = fs.Parse(os.Args[1:])
	if !*isHelper {
		return false
	}

	if err := runHelper(*configPath); err != nil {
		log.Printf("[helper] %v", err)
		os.Exit(1)
	}
	return true
}

func runHelper(configPath string) error {
	if configPath == "" {
		return fmt.Errorf("缺少 helper 配置路径")
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取 helper 配置: %w", err)
	}
	_ = os.Remove(configPath)

	var hc helperConfig
	if err := json.Unmarshal(b, &hc); err != nil {
		return fmt.Errorf("解析 helper 配置: %w", err)
	}
	if hc.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(hc.LogFile), 0700); err == nil {
			if f, openErr := os.OpenFile(hc.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); openErr == nil {
				defer f.Close()
				hc.Client.LogWriter = f
				log.SetOutput(f)
			}
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if hc.StopFile != "" {
		go watchStopFile(ctx, cancel, hc.StopFile)
	}
	if err := clientrunner.Run(ctx, hc.Client); err != nil {
		log.Printf("[helper] clientrunner 退出: %v", err)
		return err
	}
	return nil
}

func watchStopFile(ctx context.Context, cancel context.CancelFunc, path string) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := os.Stat(path); err == nil {
				_ = os.Remove(path)
				cancel()
				return
			}
		}
	}
}

func startElevatedClient(cfg clientrunner.Config) (*elevatedProcess, error) {
	cfg.LogWriter = nil
	cfg.SuppressPerConnLogs = true
	// TUN helper already routes traffic at IP level; user-level proxy/curlrc
	// changes would target root/Admin's profile after elevation.
	cfg.AutoProxy = false
	cfg.AutoEnv = false
	cfg.AutoCurlrc = false
	cfg.TUN = true
	if !cfg.SOCKS {
		cfg.SOCKS = true
	}
	if err := curlrc.ClearManaged(); err != nil {
		log.Printf("[TUN] 清理 ~/.curlrc 中旧的 feizhu 代理段失败: %v", err)
	}

	dir := filepath.Join(os.TempDir(), "feizhu-client-ui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	configPath := filepath.Join(dir, "helper-"+stamp+".json")
	stopFile := filepath.Join(dir, "helper-"+stamp+".stop")
	logFile := filepath.Join(dir, "helper-"+stamp+".log")
	if err := os.WriteFile(logFile, nil, 0600); err != nil {
		return nil, err
	}
	hc := helperConfig{Client: cfg, StopFile: stopFile, LogFile: logFile}
	b, err := json.Marshal(hc)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		return nil, err
	}

	pid, err := startElevatedSelf(configPath)
	if err != nil {
		_ = os.Remove(configPath)
		return nil, err
	}
	return &elevatedProcess{PID: pid, StopFile: stopFile, LogFile: logFile}, nil
}

func waitElevatedHealthy(p *elevatedProcess) error {
	if p == nil {
		return fmt.Errorf("helper 进程为空")
	}
	// 首次安装/加载 wintun、创建虚拟网卡在部分 Windows 机器上会超过 6 秒。
	// 等待时间过短会把仍在启动中的 helper 误判为失败并主动停止。
	deadline := time.Now().Add(30 * time.Second)
	var lastLog string
	for time.Now().Before(deadline) {
		if p.LogFile != "" {
			if b, err := os.ReadFile(p.LogFile); err == nil {
				lastLog = string(b)
				if strings.Contains(lastLog, "[TUN] Windows wintun+tun2socks 已启动") ||
					strings.Contains(lastLog, "[TUN] 已启用虚拟网卡透明代理模式") ||
					strings.Contains(lastLog, "[运行] SOCKS5 监听") {
					return nil
				}
				if strings.Contains(lastLog, "[helper] clientrunner 退出:") {
					return fmt.Errorf(strings.TrimSpace(lastLog))
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if strings.TrimSpace(lastLog) != "" {
		return fmt.Errorf("helper 未进入 TUN 就绪状态，最近日志: %s", strings.TrimSpace(lastLog))
	}
	return fmt.Errorf("helper 未写入启动日志，可能未获得系统授权或启动失败")
}

func stopElevatedClient(p *elevatedProcess) error {
	if p == nil {
		return nil
	}
	if p.StopFile != "" {
		if err := os.WriteFile(p.StopFile, []byte("stop\n"), 0600); err != nil {
			return err
		}
	}
	return nil
}
