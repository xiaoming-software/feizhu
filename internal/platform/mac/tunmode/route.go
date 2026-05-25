package mactun

import (
	"bytes"
	"fmt"
	"log"
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
		// 仅用于检测网络/DNS 变化后刷新路由，不参与旁路表。
		DNSIPs: uniqueIPs(darwinDNSIPsForInterface(iface)),
	}, nil
}

func applyRoutes(c routeConfig) error {
	mask := net.IP(c.Netmask).String()
	peer := peerIPv4(c.AddressIP)
	if err := runDarwin("ifconfig", c.DeviceName, "inet", c.AddressIP.String(), peer.String(), "netmask", mask, "mtu", fmt.Sprintf("%d", c.MTU), "up"); err != nil {
		return fmt.Errorf("mactun: 配置 TUN 地址失败: %w", err)
	}
	for _, ip := range c.State.ServerIPs {
		// 网络切换/异常退出后，旧 host route 可能仍指向旧网关；先删再加。
		_ = runDarwin("route", "-n", "delete", "-host", ip.String())
		_ = runDarwin("route", "-n", "add", "-host", ip.String(), c.State.Gateway.String())
	}
	if err := applyPF(c, peer); err != nil {
		return err
	}
	if err := applySystemDNS(c.State.Interface); err != nil {
		log.Printf("[TUN] 设置系统 DNS 为 8.8.8.8/1.1.1.1 失败（公司内网 DNS 可能导致 fake-ip）: %v", err)
	} else if svc := networkServiceForInterface(c.State.Interface); svc != "" {
		log.Printf("[TUN] macOS 系统 DNS(%s) 已设为 8.8.8.8/1.1.1.1，解析走 TUN UDP/53 → feizhu", svc)
	}
	return nil
}

func restoreRoutes(c routeConfig) error {
	_ = restoreSystemDNS(c.State.Interface, c.State.DNSIPs)
	_ = runDarwin("pfctl", "-a", "com.apple/feizhu", "-F", "all")
	for _, ip := range c.State.ServerIPs {
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
	for _, ip := range c.State.ServerIPs {
		bypass = append(bypass, ip.String())
	}
	// macOS 无 Windows 式 0.0.0.0/1 路由；公网 TCP 与 DNS(UDP/TCP 53) 均靠 pf route-to 进 TUN。
	rules := fmt.Sprintf(`table <feizhu_bypass> const { %s }
pass out quick route-to (%s %s) inet proto udp from any to any port 53 keep state
pass out quick route-to (%s %s) inet proto tcp from any to any port 53 flags S/SA keep state
pass out quick route-to (%s %s) inet proto tcp from any to ! <feizhu_bypass> flags S/SA keep state
`, strings.Join(bypass, ", "), c.DeviceName, peer.String(), c.DeviceName, peer.String(), c.DeviceName, peer.String())
	path, err := writePFRules(rules)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if err := runDarwin("pfctl", "-a", "com.apple/feizhu", "-f", path); err != nil {
		return fmt.Errorf("mactun: 加载 pf 规则失败: %w", err)
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

// darwinDNSIPsForInterface 仅用于 refresh 时比较 DNS 是否变化（与 Windows 同类检测，不改 mac 引流路径）。
func darwinDNSIPsForInterface(iface string) []net.IP {
	service := networkServiceForInterface(iface)
	if service == "" {
		return darwinDNSIPs()
	}
	out, err := exec.Command("networksetup", "-getdnsservers", service).Output()
	if err != nil {
		return darwinDNSIPs()
	}
	text := strings.TrimSpace(string(out))
	if text == "" || strings.Contains(text, "There aren't any DNS Servers") {
		return darwinDNSIPs()
	}
	var ips []net.IP
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if ip := net.ParseIP(line); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	if len(ips) == 0 {
		return darwinDNSIPs()
	}
	return ips
}

func applySystemDNS(iface string) error {
	svc := networkServiceForInterface(iface)
	if svc == "" {
		return nil
	}
	return runDarwin("networksetup", "-setdnsservers", svc, "8.8.8.8", "1.1.1.1")
}

func restoreSystemDNS(iface string, saved []net.IP) error {
	svc := networkServiceForInterface(iface)
	if svc == "" {
		return nil
	}
	if len(saved) == 0 {
		return runDarwin("networksetup", "-setdnsservers", svc, "Empty")
	}
	args := []string{"-setdnsservers", svc}
	for _, ip := range saved {
		if ip != nil {
			args = append(args, ip.String())
		}
	}
	return runDarwin("networksetup", args...)
}

func networkServiceForInterface(iface string) string {
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return ""
	}
	for _, block := range strings.Split(string(out), "\n\n") {
		if !strings.Contains(block, "Device: "+iface) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Hardware Port:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
			}
		}
		break
	}
	return ""
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
