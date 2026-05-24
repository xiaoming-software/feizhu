// Package wintun 为 Windows 专用 TUN/tun2socks 实现（冻结分支，勿与 mac 混改）。
package wintun

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/feizhu/feizhu/internal/socks5"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	t2stunnel "github.com/xjasonlyu/tun2socks/v2/tunnel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const (
	defaultAddressCIDR = "10.255.0.1/30"
	defaultMTU         = 1500
	defaultLogLevel    = "warn"
)

// Config 描述 Windows TUN 模式。
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

// Controller 持有运行中的 TUN 栈与路由状态。
type Controller struct {
	cfg      Config
	route    routeState
	device   device.Device
	stack    *stack.Stack
	stopFunc func()
	stopOnce sync.Once
}

// Start 创建 Wintun 网卡、启动 tun2socks 并安装 /1 路由。
func Start(ctx context.Context, cfg Config) (*Controller, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	cfg = withDefaults(cfg)
	initTUNDebugFromEnv(cfg)
	logWindowsTUNStartup(cfg)
	if cfg.SOCKSListen == "" {
		return nil, fmt.Errorf("wintun: SOCKSListen 为空")
	}
	if cfg.ServerAddr == "" {
		return nil, fmt.Errorf("wintun: ServerAddr 为空")
	}
	ctl, err := startTUNStack(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("wintun: Windows wintun 启动失败（请用 build.ps1 重新编译并管理员运行）: %w", err)
	}
	log.Printf("[TUN] Windows wintun+tun2socks 已启动（与 macOS 同路径）")
	return ctl, nil
}

func startTUNStack(ctx context.Context, cfg Config) (*Controller, error) {
	if err := ensureWintunLoaded(); err != nil {
		return nil, err
	}
	addr, ip, prefix, err := parseAddressCIDR(cfg.AddressCIDR)
	if err != nil {
		return nil, err
	}
	mask := net.CIDRMask(prefix, 32)
	gateway, err := captureRouteState(cfg.ServerAddr)
	if err != nil {
		return nil, err
	}

	socksAddr, err := socksProxyAddress(cfg.SOCKSListen)
	if err != nil {
		return nil, fmt.Errorf("wintun: 创建 SOCKS5 上游失败: %w", err)
	}
	socksProxy, err := proxy.NewSocks5(socksAddr, "", "")
	if err != nil {
		return nil, fmt.Errorf("wintun: 创建 SOCKS5 上游失败: %w", err)
	}
	t2stunnel.T().SetDialer(socksProxy)

	dev, err := tun.Open(cfg.DeviceName, uint32(cfg.MTU))
	if err != nil {
		return nil, fmt.Errorf("wintun: 创建 TUN 网卡失败: %w", err)
	}
	cfg.DeviceName = dev.Name()

	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: t2stunnel.T(),
	})
	if err != nil {
		closeDevice(dev)
		return nil, fmt.Errorf("wintun: 创建用户态网络栈失败: %w", err)
	}

	c := &Controller{cfg: cfg, route: gateway, device: dev, stack: st}
	if err := applyRoutes(routeConfig{
		DeviceName:  cfg.DeviceName,
		AddressCIDR: addr,
		AddressIP:   ip,
		Netmask:     mask,
		PrefixLen:   prefix,
		MTU:         cfg.MTU,
		State:       gateway,
		BypassIPs:   cfg.BypassIPs,
	}); err != nil {
		st.Close()
		closeDevice(dev)
		return nil, err
	}
	c.route = gateway
	c.startWatcher(ctx)
	return c, nil
}

func (c *Controller) startWatcher(ctx context.Context) {
	go func() {
		<-ctx.Done()
		c.Stop()
	}()
}

// Stop 还原路由并关闭 TUN。
func (c *Controller) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		if c.stopFunc != nil {
			c.stopFunc()
		}
		if c.stack != nil {
			if err := restoreRoutes(routeConfig{
				DeviceName: c.cfg.DeviceName,
				State:      c.route,
			}); err != nil {
				log.Printf("[TUN] 路由还原失败: %v", err)
			}
			c.stack.Close()
		}
		closeDevice(c.device)
	})
}

func withDefaults(c Config) Config {
	if c.DeviceName == "" {
		c.DeviceName = defaultDeviceName()
	}
	if c.AddressCIDR == "" {
		c.AddressCIDR = defaultAddressCIDR
	}
	if c.MTU == 0 {
		c.MTU = defaultMTU
	}
	if c.LogLevel == "" {
		c.LogLevel = defaultLogLevel
	}
	return c
}

func socksProxyAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("wintun: SOCKS 监听地址无效: %w", err)
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%s", host, port), nil
}

func closeDevice(dev device.Device) {
	if dev == nil {
		return
	}
	if c, ok := dev.(interface{ Close() }); ok {
		c.Close()
	}
}

func parseAddressCIDR(s string) (string, net.IP, int, error) {
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return "", nil, 0, fmt.Errorf("wintun: TUN 地址无效: %w", err)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", nil, 0, fmt.Errorf("wintun: MVP 仅支持 IPv4 TUN 地址")
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return "", nil, 0, fmt.Errorf("wintun: MVP 仅支持 IPv4 TUN 地址")
	}
	return s, ip4, ones, nil
}

func resolveIPv4Host(host string) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return []net.IP{ip4}
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("[TUN] 解析服务端地址失败 host=%s err=%v", host, err)
		return nil
	}
	var out []net.IP
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			out = append(out, ip4)
		}
	}
	return out
}

func uniqueIPs(ips []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(ips))
	var out []net.IP
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, ip)
	}
	return out
}

func dohBypassIPs() []net.IP {
	return []net.IP{
		net.IPv4(1, 1, 1, 1),
		net.IPv4(1, 0, 0, 1),
	}
}

type routeState struct {
	Gateway   net.IP
	Interface string
	IfIndex   int
	ServerIPs []net.IP
	DNSIPs    []net.IP
	PFToken   string
}

type routeConfig struct {
	DeviceName  string
	AddressCIDR string
	AddressIP   net.IP
	Netmask     net.IPMask
	PrefixLen   int
	MTU         int
	State       routeState
	BypassIPs   []string
}
