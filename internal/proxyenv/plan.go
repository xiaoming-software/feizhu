package proxyenv

func plan(c Config) (keys []string, vals map[string]string) {
	vals = make(map[string]string)
	http := c.HTTPProxyURL
	keys = []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"}
	for _, k := range keys {
		vals[k] = http
	}
	if c.EnableSOCKS && c.SOCKSProxyURL != "" {
		for _, k := range []string{"all_proxy", "ALL_PROXY"} {
			keys = append(keys, k)
			vals[k] = c.SOCKSProxyURL
		}
	}
	return keys, vals
}
