// Package tunmode 为 clientrunner 提供跨平台 TUN 入口；实现分别在 internal/platform/mac 与 internal/platform/windows。
package tunmode

import "github.com/feizhu/feizhu/internal/socks5"

// Config 描述 TUN 模式（与平台实现包字段一致，由 facade 转发）。
type Config struct {
	Enabled     bool
	DeviceName  string
	AddressCIDR string
	MTU         int
	SOCKSListen string
	LocalListen string
	ServerAddr  string
	LogLevel    string
	TUNDebug    bool
	BypassIPs   []string
	TunnelDial  socks5.DialFunc
}
