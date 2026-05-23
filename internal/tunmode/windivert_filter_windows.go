//go:build windows

package tunmode

import (
	"fmt"

	"github.com/deblasis/godivert"
)

func buildWinDivertFilter(redirectPort uint16) string {
	return fmt.Sprintf(
		"tcp and ip and ((outbound and !loopback and tcp.DstPort != %d) or (tcp.SrcPort == %d))",
		redirectPort,
		redirectPort,
	)
}

func validateWinDivertFilter(filter string) error {
	if !godivert.IsDLLLoaded() {
		return nil
	}
	ok, pos := godivert.HelperCompileFilter(filter)
	if ok {
		return nil
	}
	return fmt.Errorf("tunmode: WinDivert 过滤器语法错误（位置 %d）: %q", pos, filter)
}
