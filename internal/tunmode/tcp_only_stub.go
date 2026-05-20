//go:build !windows

package tunmode

import (
	"context"
)

func startWindowsTCPOnly(context.Context, Config, string) (*Controller, error) {
	panic("startWindowsTCPOnly is only available on Windows")
}
