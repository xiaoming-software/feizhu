//go:build !darwin && !windows

package proxyenv

import "os"

func RevertStaleFeizhuEnv() (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	if saved != nil {
		return false, nil
	}
	changed := false
	for _, name := range feizhuEnvKeys() {
		v, ok := os.LookupEnv(name)
		if !ok || !isFeizhuEnvValue(v) {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
