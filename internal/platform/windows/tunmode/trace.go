package wintun

import (
	"log"
	"os"
	"strings"
	"sync"
)

var (
	tunDebug   bool
	bypassSeen sync.Map
)

func initTUNDebugFromEnv(cfg Config) {
	if cfg.TUNDebug {
		tunDebug = true
		return
	}
	v := strings.TrimSpace(os.Getenv("FEIZHU_TUN_DEBUG"))
	tunDebug = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// Tracef 连接级追踪日志。
func Tracef(format string, args ...any) {
	log.Printf("[TUN-trace] "+format, args...)
}

// Debugf 仅在 TUNDebug 时输出。
func Debugf(format string, args ...any) {
	if !tunDebug {
		return
	}
	log.Printf("[TUN-debug] "+format, args...)
}
