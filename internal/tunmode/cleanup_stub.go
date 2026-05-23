//go:build !darwin && !windows

package tunmode

func CleanupStale() (bool, error) {
	return false, nil
}
