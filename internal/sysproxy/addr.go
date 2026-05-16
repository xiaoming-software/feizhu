package sysproxy

import (
	"fmt"
	"net"
)

// ParseListenAddr 将 -listen 解析为可供系统代理使用的 host:port（空 host 视为 127.0.0.1）。
func ParseListenAddr(listen string) (host, port string, err error) {
	h, p, err := net.SplitHostPort(listen)
	if err != nil {
		return "", "", fmt.Errorf("解析 -listen: %w", err)
	}
	if h == "" || h == "0.0.0.0" {
		h = "127.0.0.1"
	}
	if h == "::" {
		h = "127.0.0.1"
	}
	return h, p, nil
}
