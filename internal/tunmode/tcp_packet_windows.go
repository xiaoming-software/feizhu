//go:build windows

package tunmode

import "github.com/deblasis/godivert"

var errDropPacket = godivertError("drop packet")

type godivertError string

func (e godivertError) Error() string { return string(e) }

func tcpFlags(p *godivert.Packet) (syn, ack, rst bool) {
	if len(p.Raw) < 20 {
		return false, false, false
	}
	ihl := int(p.Raw[0]&0x0f) * 4
	if len(p.Raw) < ihl+14 {
		return false, false, false
	}
	flags := p.Raw[ihl+13]
	return flags&0x02 != 0, flags&0x10 != 0, flags&0x04 != 0
}
