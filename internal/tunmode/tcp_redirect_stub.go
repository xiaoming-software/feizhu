//go:build !windows

package tunmode

import "context"

func startWindowsTCPRedirect(context.Context, Config) (*Controller, error) {
	panic("startWindowsTCPRedirect is only available on Windows")
}
