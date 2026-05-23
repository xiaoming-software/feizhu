package main

import (
	"strings"

	"github.com/feizhu/feizhu/internal/curlrc"
	"github.com/feizhu/feizhu/internal/proxyenv"
	"github.com/feizhu/feizhu/internal/sysproxy"
	"github.com/feizhu/feizhu/internal/tunmode"
)

// cleanupStaleFeizhuState 在启动时清理上次异常退出可能残留的 TUN/系统代理/环境变量。
func cleanupStaleFeizhuState() string {
	var parts []string
	if ok, err := tunmode.CleanupStale(); err != nil {
		parts = append(parts, "TUN清理失败:"+err.Error())
	} else if ok {
		parts = append(parts, "已停止残留TUN")
	}
	if ok, err := sysproxy.RevertStaleFeizhuProxy(); err != nil {
		parts = append(parts, "系统代理还原失败:"+err.Error())
	} else if ok {
		parts = append(parts, "已关闭残留系统代理(127.0.0.1:7890)")
	}
	if ok, err := proxyenv.RevertStaleFeizhuEnv(); err != nil {
		parts = append(parts, "环境变量还原失败:"+err.Error())
	} else if ok {
		parts = append(parts, "已清除残留代理环境变量")
	}
	if err := curlrc.ClearManaged(); err != nil {
		parts = append(parts, "curlrc清理失败:"+err.Error())
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "；")
}
