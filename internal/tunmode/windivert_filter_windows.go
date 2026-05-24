//go:build windows

package tunmode

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/deblasis/godivert"
)

func buildWinDivertFilter(redirectPort uint16, skipDstPorts []uint16) string {
	var skip []string
	seen := map[uint16]struct{}{redirectPort: {}}
	add := func(p uint16) {
		if p == 0 {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		skip = append(skip, fmt.Sprintf("tcp.DstPort != %d", p))
	}
	for _, p := range skipDstPorts {
		add(p)
	}
	add(redirectPort)
	outbound := "outbound and !loopback"
	if len(skip) > 0 {
		outbound += " and " + strings.Join(skip, " and ")
	}
	// 需捕获 inbound：丢弃「真实远端」误入网的 SYN-ACK/数据，避免与透明劫持的伪造回包冲突。
	inbound := "inbound and !loopback"
	return fmt.Sprintf("tcp and ip and ((%s) or (tcp.SrcPort == %d) or (%s))", outbound, redirectPort, inbound)
}

func parseListenPort(listen string) uint16 {
	if listen == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || n == 0 {
		return 0
	}
	return uint16(n)
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
