//go:build darwin

package proxyenv

// RevertStaleFeizhuEnv 清除 launchctl 中残留的飞猪代理环境变量。
func RevertStaleFeizhuEnv() (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return false, nil
	}
	changed := false
	for _, name := range feizhuEnvKeys() {
		v, ok := launchGetenv(name)
		if !ok || !isFeizhuEnvValue(v) {
			continue
		}
		if err := launchUnsetenv(name); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
