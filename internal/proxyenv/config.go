package proxyenv

// Config 描述要写入用户级「代理相关」环境变量的值（供 curl、git 等读取）。
type Config struct {
	HTTPProxyURL  string // 如 http://127.0.0.1:7890
	SOCKSProxyURL string // 如 socks5://127.0.0.1:7891
	EnableSOCKS   bool
}
