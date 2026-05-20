//go:build windows

package tunmode

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func defaultDeviceName() string { return "FeizhuTunnel" }

func captureRouteState(serverAddr string) (routeState, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return routeState{}, fmt.Errorf("tunmode: serverAddr 无效: %w", err)
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
		DNSIPs:    uniqueIPs(append(windowsDNSIPs(), dohBypassIPs()...)),
	}, nil
}

func applyRoutes(c routeConfig) error {
	mask := net.IP(c.Netmask).String()
	if err := runWindows("netsh", "interface", "ipv4", "set", "address", "name="+c.DeviceName, "static", c.AddressIP.String(), mask); err != nil {
		return fmt.Errorf("tunmode: 配置 TUN 地址失败: %w", err)
	}
	if c.MTU > 0 {
		_ = runWindows("netsh", "interface", "ipv4", "set", "subinterface", c.DeviceName, fmt.Sprintf("mtu=%d", c.MTU), "store=active")
	}

	tunIndex, err := waitInterfaceIndex(c.DeviceName)
	if err != nil {
		return err
	}
	for _, ip := range append(c.State.ServerIPs, c.State.DNSIPs...) {
		_ = runWindows("route", "add", ip.String(), "mask", "255.255.255.255", c.State.Gateway.String(), "metric", "1", "if", strconv.Itoa(c.State.IfIndex))
	}
	if err := runWindows("route", "add", "0.0.0.0", "mask", "128.0.0.0", c.AddressIP.String(), "metric", "1", "if", strconv.Itoa(tunIndex)); err != nil {
		return fmt.Errorf("tunmode: 添加 0.0.0.0/1 路由失败: %w", err)
	}
	if err := runWindows("route", "add", "128.0.0.0", "mask", "128.0.0.0", c.AddressIP.String(), "metric", "1", "if", strconv.Itoa(tunIndex)); err != nil {
		_ = runWindows("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
		return fmt.Errorf("tunmode: 添加 128.0.0.0/1 路由失败: %w", err)
	}
	return nil
}

func restoreRoutes(c routeConfig) error {
	_ = runWindows("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = runWindows("route", "delete", "128.0.0.0", "mask", "128.0.0.0")
	for _, ip := range append(c.State.ServerIPs, c.State.DNSIPs...) {
		_ = runWindows("route", "delete", ip.String(), "mask", "255.255.255.255")
	}
	return nil
}

func windowsDefaultRoute() (net.IP, int, string, error) {
	script := `$r = Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric, InterfaceMetric | Select-Object -First 1; if ($null -eq $r) { exit 2 }; $a = Get-NetAdapter -InterfaceIndex $r.InterfaceIndex; Write-Output "$($r.NextHop)|$($r.InterfaceIndex)|$($a.Name)"`
	out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
	if err != nil {
		return nil, 0, "", fmt.Errorf("tunmode: 获取默认路由失败: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 3 {
		return nil, 0, "", fmt.Errorf("tunmode: 默认路由输出异常: %q", strings.TrimSpace(string(out)))
	}
	gw := net.ParseIP(parts[0]).To4()
	if gw == nil {
		return nil, 0, "", fmt.Errorf("tunmode: 默认网关不是 IPv4: %q", parts[0])
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, 0, "", fmt.Errorf("tunmode: 默认路由接口索引无效: %w", err)
	}
	return gw, idx, parts[2], nil
}

func windowsDNSIPs() []net.IP {
	script := `Get-DnsClientServerAddress -AddressFamily IPv4 | ForEach-Object { $_.ServerAddresses } | Sort-Object -Unique`
	out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
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

func waitInterfaceIndex(name string) (int, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		iface, err := net.InterfaceByName(name)
		if err == nil {
			return iface.Index, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("tunmode: 未找到 TUN 网卡 %q: %w", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runWindows(name string, args ...string) error {
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
