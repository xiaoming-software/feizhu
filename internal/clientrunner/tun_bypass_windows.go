//go:build windows

package clientrunner

import (
	"net/url"

	"github.com/feizhu/feizhu/internal/upstreamproxy"
)

func tunBypassIPsForPlatform(upstream *url.URL) []string {
	return upstreamproxy.BypassIPStrings(upstream)
}

func tunBootstrapBypassHosts(*url.URL) []string { return nil }
