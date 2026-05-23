package proxyenv

import "strings"

func isFeizhuEnvValue(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "127.0.0.1:7890") ||
		strings.Contains(v, "127.0.0.1:7891") ||
		strings.Contains(v, "localhost:7890") ||
		strings.Contains(v, "localhost:7891")
}

func feizhuEnvKeys() []string {
	return []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY"}
}
