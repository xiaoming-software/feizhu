//go:build windows

package clientrunner

import (
	"net"
	"net/url"

	winplat "github.com/feizhu/feizhu/internal/platform/windows"
)

func (d *targetDialer) winDialer() *winplat.TunnelDialer {
	return &winplat.TunnelDialer{
		Upstream:   d.upstream,
		ServerAddr: d.serverAddr,
		Password:   d.password,
		TLSConfig:  d.tlsCfg,
		LogTUN:     d.logTUN,
	}
}

func (d *targetDialer) dialLocal(host string, port uint16) (net.Conn, error) {
	return d.winDialer().DialLocal(host, port)
}

func (d *targetDialer) dialTUN(host string, port uint16) (net.Conn, error) {
	return d.winDialer().DialTUN(host, port)
}

func logTUNDialHintOnce(upstream *url.URL, host string, err error) {
	_ = upstream
	_ = host
	_ = err
}
