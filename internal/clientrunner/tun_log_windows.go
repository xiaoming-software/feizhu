//go:build windows

package clientrunner

import "log"

func logTUNPlatformMessages(upstreamConfigured bool) {
	if upstreamConfigured {
		log.Println("[TUN] 已配置上级代理：链路为 本机 -> feizhu TLS -> feizhu-server -> 上级代理 -> 目标站。")
	} else {
		log.Println("[TUN] 提示：curl -x 外部代理 的 TCP 也会被透明拦截；若外部代理仅允许家庭宽带 IP 认证，请在 feizhu 配置「上级代理」并让应用走 127.0.0.1:7890。")
	}
	log.Println("[TUN] 指纹浏览器可在配置里填远程代理地址（或留空走透明拦截）；上级代理只需在飞猪填写。TCP 将透明经 feizhu TLS 转发，浏览器内无需再填 127.0.0.1。")
	log.Println("[TUN] Windows：透明拦截经 wintun+tun2socks -> 本地 SOCKS5 -> feizhu TLS。")
	log.Println("[TUN] Windows 备选：AdsPower 代理类型选 SOCKS5，地址 127.0.0.1:7891；若需经第三方代理出口，在飞猪填「上级代理」。")
}
