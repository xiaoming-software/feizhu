//go:build darwin

package tunmode

import (
	"context"

	mactun "github.com/feizhu/feizhu/internal/platform/mac/tunmode"
)

// Controller 为 macOS TUN 控制器。
type Controller = mactun.Controller

// Start 转发至 macOS 专用实现。
func Start(ctx context.Context, cfg Config) (*Controller, error) {
	return mactun.Start(ctx, mactun.Config(cfg))
}

// CleanupStale 清理异常退出残留的 pf 规则。
func CleanupStale() (bool, error) {
	return mactun.CleanupStale()
}
