//go:build !darwin && !windows

package sysproxy

func RevertStaleFeizhuProxy() (bool, error) {
	return false, nil
}
