package tunmode

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

// SetTUNDebug 开启更详细的 TUN/WinDivert 排障日志（旁路原因、流表等）。
func SetTUNDebug(on bool) {
	tunDebug = on
}

func TUNDebugEnabled() bool {
	return tunDebug
}

func initTUNDebugFromEnv(cfg Config) {
	if cfg.TUNDebug {
		tunDebug = true
		return
	}
	v := strings.TrimSpace(os.Getenv("FEIZHU_TUN_DEBUG"))
	tunDebug = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// Tracef 连接级追踪，TUN 模式下默认输出，便于排障。
func Tracef(format string, args ...any) {
	log.Printf("[TUN-trace] "+format, args...)
}

// Debugf 仅在 TUNDebug / FEIZHU_TUN_DEBUG 时输出。
func Debugf(format string, args ...any) {
	if !tunDebug {
		return
	}
	log.Printf("[TUN-debug] "+format, args...)
}

func logBypassOnce(key, msg string) {
	if !tunDebug {
		return
	}
	if _, loaded := bypassSeen.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	Debugf("%s", msg)
}
