package wintun

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed embed/wintun.dll
var embeddedWintunDLL []byte

func ensureWintunLoaded() error {
	if len(embeddedWintunDLL) == 0 {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("wintun: 无法定位程序目录: %w", err)
		}
		dllPath := filepath.Join(filepath.Dir(exe), "wintun.dll")
		if _, err := os.Stat(dllPath); err != nil {
			return fmt.Errorf("wintun: 未找到 wintun.dll（请用 build.ps1 重新编译 Windows 版）")
		}
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
	dllPath := filepath.Join(dir, "wintun.dll")
	if err := materializeWinDivertFile(dllPath, embeddedWintunDLL); err != nil {
		return err
	}
	return nil
}
