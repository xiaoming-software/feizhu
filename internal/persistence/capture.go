package persistence

import (
	"github.com/feizhu/feizhu/internal/curlrc"
	"github.com/feizhu/feizhu/internal/proxyenv"
	"github.com/feizhu/feizhu/internal/sysproxy"
)

// CaptureOptions 与 clientrunner.Config 中自动代理相关字段对应。
type CaptureOptions struct {
	AutoProxy      bool
	AutoEnv        bool
	AutoCurlrc     bool
	NetworkService string
	Proxyenv       proxyenv.Config
}

// Capture 在 Apply 之前采集当前系统/环境状态，供强杀后 Recover 使用。
func Capture(opts CaptureOptions) (*AppSnapshot, error) {
	s := &AppSnapshot{
		AutoProxy:  opts.AutoProxy,
		AutoEnv:    opts.AutoEnv,
		AutoCurlrc: opts.AutoCurlrc,
	}
	if opts.AutoProxy {
		sp, err := sysproxy.CaptureForPersistence(opts.NetworkService)
		if err != nil {
			return nil, err
		}
		s.Sysproxy = sp
	}
	if opts.AutoEnv {
		pe, err := proxyenv.CaptureForPersistence(opts.Proxyenv)
		if err != nil {
			return nil, err
		}
		s.Proxyenv = pe
	}
	if opts.AutoCurlrc {
		cr, err := curlrc.CaptureForPersistence()
		if err != nil {
			return nil, err
		}
		s.Curlrc = cr
	}
	return s, nil
}
