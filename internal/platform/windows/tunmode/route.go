package wintun

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func defaultDeviceName() string { return "FeizhuTunnel" }

var windowsTUNDNSServers = []string{"8.8.8.8", "1.1.1.1"}

// windowsBypassNets 与 macOS pf 表 <feizhu_bypass> 一致：这些目标走物理网卡，不进 TUN。
// 尤其 127.0.0.0/8 必须旁路，否则本机 7890/7891 会被 /1 路由 hijack 到 TUN。
var windowsBypassNets = []struct{ dest, mask string }{
	{"0.0.0.0", "255.0.0.0"},
	{"10.0.0.0", "255.0.0.0"},
	{"100.64.0.0", "255.192.0.0"},
	{"127.0.0.0", "255.0.0.0"},
	{"169.254.0.0", "255.255.0.0"},
	{"172.16.0.0", "255.240.0.0"},
	{"192.168.0.0", "255.255.0.0"},
	{"224.0.0.0", "240.0.0.0"},
	{"240.0.0.0", "240.0.0.0"},
}

func captureRouteState(serverAddr string) (routeState, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return routeState{}, fmt.Errorf("wintun: serverAddr 无效: %w", err)
	}
	gw, ifIndex, ifName, err := windowsDefaultRoute()
	if err != nil {
		return routeState{}, err
	}
	return routeState{
		Gateway:   gw,
		Interface: ifName,
		IfIndex:   ifIndex,
		ServerIPs: uniqueIPs(resolveIPv4Host(host)),
		DNSIPs:    uniqueIPs(windowsDNSIPsForInterface(ifIndex)),
	}, nil
}

func applyRoutes(c routeConfig) error {
	mask := net.IP(c.Netmask).String()
	if err := runWindows("netsh", "interface", "ipv4", "set", "address", "name="+c.DeviceName, "static", c.AddressIP.String(), mask); err != nil {
		return fmt.Errorf("wintun: 配置 TUN 地址失败: %w", err)
	}
	if c.MTU > 0 {
		_ = runWindows("netsh", "interface", "ipv4", "set", "subinterface", c.DeviceName, fmt.Sprintf("mtu=%d", c.MTU), "store=active")
	}

	tunIndex, err := waitInterfaceIndex(c.DeviceName)
	if err != nil {
		return err
	}
	if err := configureTUNDNS(c.DeviceName); err != nil {
		log.Printf("[TUN] 配置 Windows TUN DNS 失败: %v", err)
	} else {
		log.Printf("[TUN] Windows TUN DNS 已设置为 %s", strings.Join(windowsTUNDNSServers, ", "))
	}
	if err := applyPhysDNS(c.State.IfIndex); err != nil {
		log.Printf("[TUN] 设置物理网卡 DNS 为 8.8.8.8/1.1.1.1 失败（公司内网 DNS 可能导致指纹浏览器异常）: %v", err)
	} else if c.State.Interface != "" {
		log.Printf("[TUN] Windows 物理网卡 DNS(%s) 已设为 %s，解析走 TUN（与 macOS 一致）", c.State.Interface, strings.Join(windowsTUNDNSServers, ", "))
	}
	physIf := strconv.Itoa(c.State.IfIndex)
	gw := c.State.Gateway.String()

	// 旧实例异常退出时 /1 路由可能仍在，先删再建，避免 route add 因已存在而失败。
	_ = runWindows("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = runWindows("route", "delete", "128.0.0.0", "mask", "128.0.0.0")

	for _, n := range windowsBypassNets {
		_ = runWindows("route", "delete", n.dest, "mask", n.mask)
		if err := runWindows("route", "add", n.dest, "mask", n.mask, gw, "metric", "5", "if", physIf); err != nil {
			log.Printf("[TUN] 旁路路由 %s/%s（可忽略若已存在）: %v", n.dest, n.mask, err)
		}
	}
	for _, ip := range c.State.ServerIPs {
		_ = runWindows("route", "delete", ip.String(), "mask", "255.255.255.255")
		_ = runWindows("route", "add", ip.String(), "mask", "255.255.255.255", gw, "metric", "1", "if", physIf)
	}
	for _, ip := range c.BypassIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		_ = runWindows("route", "delete", ip, "mask", "255.255.255.255")
		_ = runWindows("route", "add", ip, "mask", "255.255.255.255", gw, "metric", "1", "if", physIf)
	}

	tunIf := strconv.Itoa(tunIndex)
	if err := runWindows("route", "add", "0.0.0.0", "mask", "128.0.0.0", c.AddressIP.String(), "metric", "1", "if", tunIf); err != nil {
		return fmt.Errorf("wintun: 添加 0.0.0.0/1 路由失败: %w", err)
	}
	if err := runWindows("route", "add", "128.0.0.0", "mask", "128.0.0.0", c.AddressIP.String(), "metric", "1", "if", tunIf); err != nil {
		_ = runWindows("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
		return fmt.Errorf("wintun: 添加 128.0.0.0/1 路由失败: %w", err)
	}
	log.Printf("[TUN] Windows 路由已安装：旁路私有/回环网段，其余 IPv4 默认走 %s（tun2socks）", c.DeviceName)
	return nil
}

func restoreRoutes(c routeConfig) error {
	_ = restorePhysDNS(c.State.IfIndex, c.State.DNSIPs)
	_ = resetTUNDNS(c.DeviceName)
	_ = runWindows("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = runWindows("route", "delete", "128.0.0.0", "mask", "128.0.0.0")
	for _, n := range windowsBypassNets {
		_ = runWindows("route", "delete", n.dest, "mask", n.mask)
	}
	for _, ip := range c.State.ServerIPs {
		_ = runWindows("route", "delete", ip.String(), "mask", "255.255.255.255")
	}
	for _, ip := range c.BypassIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		_ = runWindows("route", "delete", ip, "mask", "255.255.255.255")
	}
	return nil
}

func windowsDefaultRoute() (net.IP, int, string, error) {
	script := `$r = Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric, InterfaceMetric | Select-Object -First 1; if ($null -eq $r) { exit 2 }; $a = Get-NetAdapter -InterfaceIndex $r.InterfaceIndex; Write-Output "$($r.NextHop)|$($r.InterfaceIndex)|$($a.Name)"`
	cmd := hiddenCommand("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, 0, "", fmt.Errorf("wintun: 获取默认路由失败: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 3 {
		return nil, 0, "", fmt.Errorf("wintun: 默认路由输出异常: %q", strings.TrimSpace(string(out)))
	}
	gw := net.ParseIP(parts[0]).To4()
	if gw == nil {
		return nil, 0, "", fmt.Errorf("wintun: 默认网关不是 IPv4: %q", parts[0])
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, 0, "", fmt.Errorf("wintun: 默认路由接口索引无效: %w", err)
	}
	return gw, idx, parts[2], nil
}

func windowsDNSIPsForInterface(ifIndex int) []net.IP {
	script := fmt.Sprintf(`Get-DnsClientServerAddress -InterfaceIndex %d -AddressFamily IPv4 | ForEach-Object { $_.ServerAddresses } | Sort-Object -Unique`, ifIndex)
	cmd := hiddenCommand("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, line := range strings.Split(string(out), "\n") {
		if ip := net.ParseIP(strings.TrimSpace(line)); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	return ips
}

// applyPhysDNS 将默认物理网卡 DNS 设为公网解析器，避免公司内网 DNS 返回 198.18 fake-ip。
func applyPhysDNS(ifIndex int) error {
	if ifIndex <= 0 {
		return nil
	}
	quoted := make([]string, 0, len(windowsTUNDNSServers))
	for _, s := range windowsTUNDNSServers {
		quoted = append(quoted, "'"+s+"'")
	}
	script := fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s)`, ifIndex, strings.Join(quoted, ","))
	return runWindows("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func restorePhysDNS(ifIndex int, saved []net.IP) error {
	if ifIndex <= 0 {
		return nil
	}
	if len(saved) == 0 {
		script := fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses -ErrorAction SilentlyContinue`, ifIndex)
		return runWindows("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	}
	quoted := make([]string, 0, len(saved))
	for _, ip := range saved {
		if ip != nil {
			quoted = append(quoted, "'"+ip.String()+"'")
		}
	}
	script := fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s)`, ifIndex, strings.Join(quoted, ","))
	return runWindows("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func configureTUNDNS(deviceName string) error {
	quoted := make([]string, 0, len(windowsTUNDNSServers))
	for _, s := range windowsTUNDNSServers {
		quoted = append(quoted, "'"+s+"'")
	}
	script := fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceAlias %q -ServerAddresses @(%s)`, deviceName, strings.Join(quoted, ","))
	return runWindows("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func resetTUNDNS(deviceName string) error {
	script := fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceAlias %q -ResetServerAddresses -ErrorAction SilentlyContinue`, deviceName)
	return runWindows("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func waitInterfaceIndex(name string) (int, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		iface, err := net.InterfaceByName(name)
		if err == nil {
			return iface.Index, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("wintun: 未找到 TUN 网卡 %q: %w", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runWindows(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := hiddenCommand(name, args...)
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

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
