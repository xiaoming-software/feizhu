//go:build !darwin && !windows

package tunmode

import (
	"context"
	"fmt"
	"runtime"
)

// Controller 占位类型。
type Controller struct{}

func (c *Controller) Stop() {}

// Start 非 macOS/Windows 不支持 TUN。
func Start(context.Context, Config) (*Controller, error) {
	return nil, fmt.Errorf("tunmode: 当前仅支持 macOS 和 Windows，当前系统为 %s", runtime.GOOS)
}

// CleanupStale 无操作。
func CleanupStale() (bool, error) {
	return false, nil
}
