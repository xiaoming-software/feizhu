package clientrunner

import (
	"fmt"
	"net"
	"time"
)

// PortInUse 检测 TCP 端口是否已有服务在监听。
func PortInUse(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// WaitListenReady 轮询直到 addr 可连接或超时（用于 TUN 提权 helper 前先启动用户态本地代理）。
func WaitListenReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		lastErr = err
		time.Sleep(120 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("等待 %s 就绪超时: %w", addr, lastErr)
	}
	return fmt.Errorf("等待 %s 就绪超时", addr)
}
