//go:build !darwin && !windows

package clientrunner

import "net/url"

func tunBypassIPsForPlatform(upstream *url.URL) []string {
	return nil
}

func tunBootstrapBypassHosts(*url.URL) []string { return nil }
