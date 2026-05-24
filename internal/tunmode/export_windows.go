//go:build windows

package tunmode

import (
	"context"

	"github.com/feizhu/feizhu/internal/platform/windows/tunmode"
)

// Controller 为 Windows TUN 控制器。
type Controller = wintun.Controller

// Start 转发至 Windows 专用实现（冻结分支）。
func Start(ctx context.Context, cfg Config) (*Controller, error) {
	return wintun.Start(ctx, wintun.Config(cfg))
}

// CleanupStale 向 helper stop 文件发停止信号。
func CleanupStale() (bool, error) {
	return wintun.CleanupStale()
}
