//go:build windows

package persistence

import "golang.org/x/sys/windows"

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimited = 0x1000
	h, err := windows.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}
