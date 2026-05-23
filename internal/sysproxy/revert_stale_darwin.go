//go:build darwin

package sysproxy

import "fmt"

// RevertStaleFeizhuProxy 在进程未持有 saved 快照时，关闭仍指向 127.0.0.1:7890/7891 的系统代理（异常退出残留）。
func RevertStaleFeizhuProxy() (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return false, nil
	}
	svc, err := resolveService("")
	if err != nil {
		return false, err
	}
	changed := false
	for _, kind := range []string{"web", "secureweb", "socks"} {
		st, err := getProxyState(kind, svc)
		if err != nil {
			continue
		}
		if st.Enabled && isFeizhuProxyEndpoint(st.Server, st.Port) {
			if err := restoreOne(svc, kind, proxyState{}); err != nil {
				return changed, fmt.Errorf("关闭 %s 代理: %w", kind, err)
			}
			changed = true
		}
	}
	return changed, nil
}
