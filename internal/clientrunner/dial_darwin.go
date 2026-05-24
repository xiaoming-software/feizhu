//go:build darwin

package clientrunner

import (
	"net"
	"net/url"

	macplat "github.com/feizhu/feizhu/internal/platform/mac"
)

func (d *targetDialer) macDialer() *macplat.TunnelDialer {
	return &macplat.TunnelDialer{
		Upstream:   d.upstream,
		ServerAddr: d.serverAddr,
		Password:   d.password,
		TLSConfig:  d.tlsCfg,
		LogTUN:     d.logTUN,
	}
}

func (d *targetDialer) dialLocal(host string, port uint16) (net.Conn, error) {
	return d.macDialer().DialLocal(host, port)
}

func (d *targetDialer) dialTUN(host string, port uint16) (net.Conn, error) {
	return d.macDialer().DialTUN(host, port)
}

func logTUNDialHintOnce(upstream *url.URL, host string, err error) {
	macplat.LogTUNDialHint(upstream, host, err)
}
