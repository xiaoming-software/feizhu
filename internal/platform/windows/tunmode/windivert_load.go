package wintun

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/deblasis/godivert"
)

//go:embed embed/WinDivert.dll
var embeddedWinDivertDLL []byte

//go:embed embed/WinDivert64.sys
var embeddedWinDivertSYS []byte

func ensureWinDivertLoaded() error {
	if godivert.IsDLLLoaded() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("wintun: 无法定位程序目录: %w", err)
	}
	dir, err := filepath.Abs(filepath.Dir(exe))
	if err != nil {
		return fmt.Errorf("wintun: 无法解析程序目录: %w", err)
	}

	dllPath := filepath.Join(dir, "WinDivert.dll")
	sysPath := filepath.Join(dir, "WinDivert64.sys")
	if err := materializeWinDivertFile(dllPath, embeddedWinDivertDLL); err != nil {
		return err
	}
	if err := materializeWinDivertFile(sysPath, embeddedWinDivertSYS); err != nil {
		return err
	}

	if err := godivert.LoadDLL(dllPath, dllPath); err != nil {
		return fmt.Errorf("wintun: 加载 WinDivert.dll 失败: %w", err)
	}
	if err := godivert.CheckDLL(); err != nil {
		return fmt.Errorf("wintun: WinDivert 驱动未就绪（需管理员权限）: %w", err)
	}
	return nil
}

func materializeWinDivertFile(path string, data []byte) error {
	if len(data) == 0 {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("wintun: 未找到 %s，请使用 build.sh 编译 Windows 版", path)
		}
		return nil
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("wintun: 释放 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}
