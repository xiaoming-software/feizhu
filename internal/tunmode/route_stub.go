//go:build !darwin && !windows

package tunmode

import "fmt"

func defaultDeviceName() string { return "feizhu0" }

func captureRouteState(string) (routeState, error) {
	return routeState{}, fmt.Errorf("tunmode: 当前平台暂不支持 TUN 模式")
}

func applyRoutes(routeConfig) error {
	return fmt.Errorf("tunmode: 当前平台暂不支持 TUN 模式")
}

func restoreRoutes(routeConfig) error { return nil }
