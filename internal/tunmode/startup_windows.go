//go:build windows

package tunmode

import (
	"os"
	"strings"
)

func logWindowsTUNStartup(cfg Config) {
	Tracef("TUN 排障日志已启用（详细旁路/流表: TUNDebug=%v FEIZHU_TUN_DEBUG=%q）",
		tunDebug, strings.TrimSpace(os.Getenv("FEIZHU_TUN_DEBUG")))
	Tracef("配置 SOCKSListen=%q LocalListen=%q ServerAddr=%q", cfg.SOCKSListen, cfg.LocalListen, cfg.ServerAddr)
	bypass := buildTCPBypassSet(cfg)
	var ips []string
	for _, ip := range resolveIPv4Host(hostFromAddr(cfg.ServerAddr)) {
		ips = append(ips, ip.String())
	}
	for _, ip := range append(windowsDNSIPs(), dohBypassIPs()...) {
		ips = append(ips, ip.String())
	}
	if len(ips) > 0 {
		Tracef("旁路 IP（不进透明拦截）: %s", strings.Join(uniqueIPStrings(ips), ", "))
	}
	Tracef("旁路表条目数=%d（含 DNS/服务端/显式 BypassIPs）", len(bypass))
	Tracef("提示: 私有网段 10/8 172.16/12 192.168/16 默认旁路；AdsPower 代理若为内网 IP 则不会走 feizhu")
}

func uniqueIPStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
