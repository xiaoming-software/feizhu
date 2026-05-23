package sysproxy

import "strings"

// isFeizhuProxyEndpoint 判断是否为飞猪 GUI 写入的本地代理地址。
func isFeizhuProxyEndpoint(host, port string) bool {
	if host != "127.0.0.1" && host != "localhost" {
		return false
	}
	return port == "7890" || port == "7891"
}

func isFeizhuProxyServerValue(server string) bool {
	server = strings.ToLower(strings.TrimSpace(server))
	if server == "" {
		return false
	}
	return strings.Contains(server, "127.0.0.1:7890") ||
		strings.Contains(server, "127.0.0.1:7891") ||
		strings.Contains(server, "localhost:7890") ||
		strings.Contains(server, "localhost:7891")
}
