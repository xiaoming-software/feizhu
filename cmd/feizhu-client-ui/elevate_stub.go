//go:build !darwin && !windows

package main

import "fmt"

func startElevatedSelf(string) (int, error) {
	return 0, fmt.Errorf("当前平台不支持 GUI 提权 TUN helper")
}
