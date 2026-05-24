package mactun

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

func defaultDeviceName() string { return "utun123" }

func captureRouteState(serverAddr string) (routeState, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return routeState{}, fmt.Errorf("mactun: serverAddr 无效: %w", err)
	}
	gw, iface, err := darwinDefaultRoute()
	if err != nil {
		return routeState{}, err
	}
	return routeState{
		Gateway:   gw,
		Interface: iface,
		ServerIPs: uniqueIPs(resolveIPv4Host(host)),
		DNSIPs:    uniqueIPs(append(darwinDNSIPs(), dohBypassIPs()...)),
	}, nil
}

func applyRoutes(c routeConfig) error {
	mask := net.IP(c.Netmask).String()
	peer := peerIPv4(c.AddressIP)
	if err := runDarwin("ifconfig", c.DeviceName, "inet", c.AddressIP.String(), peer.String(), "netmask", mask, "mtu", fmt.Sprintf("%d", c.MTU), "up"); err != nil {
		return fmt.Errorf("mactun: 配置 TUN 地址失败: %w", err)
	}
	for _, ip := range append(c.State.ServerIPs, c.State.DNSIPs...) {
		// 网络切换/异常退出后，旧 host route 可能仍指向旧网关；
		// 先删再加，确保重启飞猪即可恢复，不必重启系统。
		_ = runDarwin("route", "-n", "delete", "-host", ip.String())
		_ = runDarwin("route", "-n", "add", "-host", ip.String(), c.State.Gateway.String())
	}
	if err := applyPF(c, peer); err != nil {
		return err
	}
	return nil
}

func restoreRoutes(c routeConfig) error {
	_ = runDarwin("pfctl", "-a", "com.apple/feizhu", "-F", "all")
	for _, ip := range append(c.State.ServerIPs, c.State.DNSIPs...) {
		_ = runDarwin("route", "-n", "delete", "-host", ip.String())
	}
	if c.DeviceName != "" {
		_ = runDarwin("ifconfig", c.DeviceName, "down")
	}
	return nil
}

func applyPF(c routeConfig, peer net.IP) error {
	var bypass []string
	bypass = append(bypass,
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
		"224.0.0.0/4", "240.0.0.0/4",
	)
	for _, ip := range append(c.State.ServerIPs, c.State.DNSIPs...) {
		bypass = append(bypass, ip.String())
	}
	rules := fmt.Sprintf(`table <feizhu_bypass> const { %s }
pass out quick route-to (%s %s) inet proto tcp from any to ! <feizhu_bypass> flags S/SA keep state
`, strings.Join(bypass, ", "), c.DeviceName, peer.String())
	path, err := writePFRules(rules)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if err := runDarwin("pfctl", "-a", "com.apple/feizhu", "-f", path); err != nil {
		return fmt.Errorf("mactun: 加载 TCP-only pf 规则失败: %w", err)
	}
	if err := runDarwin("pfctl", "-E"); err != nil && !strings.Contains(err.Error(), "pf already enabled") {
		return fmt.Errorf("mactun: 启用 pf 失败: %w", err)
	}
	return nil
}

func writePFRules(rules string) (string, error) {
	f, err := os.CreateTemp("", "feizhu-pf-*.conf")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(rules); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func peerIPv4(ip net.IP) net.IP {
	ip4 := append(net.IP(nil), ip.To4()...)
	ip4[3]++
	return ip4
}

func darwinDefaultRoute() (net.IP, string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return nil, "", fmt.Errorf("mactun: 获取默认路由失败: %w", err)
	}
	var gw net.IP
	var iface string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gateway:") {
			gw = net.ParseIP(strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))).To4()
		}
		if strings.HasPrefix(line, "interface:") {
			iface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	if gw == nil || iface == "" {
		return nil, "", fmt.Errorf("mactun: 无法解析默认路由 gateway/interface")
	}
	return gw, iface, nil
}

func darwinDNSIPs() []net.IP {
	out, err := exec.Command("scutil", "--dns").Output()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "nameserver[") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if ip := net.ParseIP(strings.TrimSpace(parts[1])); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	return ips
}

func runDarwin(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %v: %w: %s", name, args, err, msg)
		}
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}
