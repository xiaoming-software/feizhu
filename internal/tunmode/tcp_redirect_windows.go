//go:build windows

package tunmode

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/deblasis/godivert"
	"github.com/feizhu/feizhu/internal/socks5"
)

const tcpRedirectMapTTL = 2 * time.Minute

type tcpFlowKey struct {
	ip   string
	port uint16
}

type tcpOriginalTarget struct {
	host string
	port uint16
	seen time.Time
}

type tcpRedirector struct {
	ctx        context.Context
	cancel     context.CancelFunc
	socksAddr  string
	tunnelDial socks5.DialFunc
	listener   net.Listener
	divert     *godivert.WinDivertHandle
	bypass     map[uint32]struct{}

	mu    sync.Mutex
	flows map[tcpFlowKey]tcpOriginalTarget
}

func startWindowsTCPRedirect(ctx context.Context, cfg Config) (*Controller, error) {
	if cfg.SOCKSListen == "" {
		return nil, fmt.Errorf("tunmode: Windows 透明代理需要 SOCKSListen")
	}
	socksAddr, err := socksProxyAddress(cfg.SOCKSListen)
	if err != nil {
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	r := &tcpRedirector{
		ctx:        childCtx,
		cancel:     cancel,
		socksAddr:  socksAddr,
		tunnelDial: cfg.TunnelDial,
		bypass:     buildTCPBypassSet(cfg),
		flows:      make(map[tcpFlowKey]tcpOriginalTarget),
	}
	if err := r.start(cfg); err != nil {
		cancel()
		return nil, err
	}
	log.Printf("[TUN] Windows WinDivert 透明代理：出站 TCP -> 本地 SOCKS5 %s -> feizhu TLS（与 macOS 路径一致）", socksAddr)
	return &Controller{
		cfg: cfg,
		stopFunc: func() {
			r.stop()
		},
	}, nil
}

func (r *tcpRedirector) start(cfg Config) error {
	if err := ensureWinDivertLoaded(); err != nil {
		return err
	}
	// 必须监听所有接口：WinDivert 改写的目标为本机出网 IP（如 192.168.x.x），
	// 若只绑 127.0.0.1，源为 LAN IP 的包无法完成 TCP 握手（accept 永远为 0）。
	ln, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return fmt.Errorf("tunmode: 透明代理监听失败: %w", err)
	}
	r.listener = ln
	redirectPort := uint16(ln.Addr().(*net.TCPAddr).Port)

	skipPorts := []uint16{
		parseListenPort(cfg.SOCKSListen),
		parseListenPort(cfg.LocalListen),
	}
	filter := buildWinDivertFilter(redirectPort, skipPorts)
	if err := validateWinDivertFilter(filter); err != nil {
		_ = ln.Close()
		return err
	}
	wd, err := godivert.NewWinDivertHandle(filter)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("tunmode: 启动 WinDivert 失败: %w（filter=%q）", err, filter)
	}
	r.divert = wd

	go r.acceptLoop()
	go r.packetLoop(redirectPort)
	go r.cleanupLoop()
	log.Printf("[TUN] WinDivert redirectPort=%d filter=%q", redirectPort, filter)
	return nil
}

func (r *tcpRedirector) stop() {
	r.cancel()
	if r.divert != nil {
		_ = r.divert.Close()
	}
	if r.listener != nil {
		_ = r.listener.Close()
	}
}

func (r *tcpRedirector) packetLoop(redirectPort uint16) {
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}
		p, err := r.divert.Recv()
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			log.Printf("[TUN] WinDivert recv: %v", err)
			continue
		}
		drop, err := r.rewritePacket(p, redirectPort)
		if err != nil {
			if drop {
				continue
			}
			log.Printf("[TUN] WinDivert rewrite: %v", err)
		}
		p.CalcNewChecksum(r.divert)
		_, _ = p.Send(r.divert)
	}
}

func (r *tcpRedirector) rewritePacket(p *godivert.Packet, redirectPort uint16) (drop bool, err error) {
	p.ParseHeaders()
	srcIP := p.SrcIP()
	dstIP := p.DstIP()
	srcPort, err := p.SrcPort()
	if err != nil {
		return false, err
	}
	dstPort, err := p.DstPort()
	if err != nil {
		return false, err
	}

	if p.Direction() == godivert.WinDivertDirectionInbound {
		if orig, ok := r.lookupFlow(dstIP.String(), dstPort); ok {
			if orig.host == srcIP.String() && orig.port == srcPort {
				Tracef("丢弃 inbound 泄漏包 %s:%d -> %s:%d（已劫持流）", srcIP, srcPort, dstIP, dstPort)
				return true, errDropPacket
			}
		}
		return false, nil
	}

	if srcPort == redirectPort {
		orig, ok := r.lookupFlow(dstIP.String(), dstPort)
		if !ok {
			Tracef("回程改包 lookup 失败 dst=%s:%d", dstIP, dstPort)
			return false, nil
		}
		p.SetSrcIP(net.ParseIP(orig.host))
		Tracef("回程改包 src -> %s:%d (client %s:%d)", orig.host, orig.port, dstIP, dstPort)
		return false, p.SetSrcPort(orig.port)
	}
	if dstPort == redirectPort {
		return false, nil
	}
	if shouldBypassIPv4(dstIP, r.bypass) {
		if reason := bypassReasonIPv4(dstIP, r.bypass); reason != "" {
			logBypassOnce(dstIP.String(), fmt.Sprintf("旁路 %s:%d 原因=%s", dstIP, dstPort, reason))
		}
		return false, nil
	}
	syn, ack, _ := tcpFlags(p)
	if syn && !ack {
		target := tcpOriginalTarget{host: dstIP.String(), port: dstPort, seen: time.Now()}
		r.rememberFlow(srcIP.String(), srcPort, target)
		Debugf("记录流(SYN) %s:%d -> %s:%d", srcIP, srcPort, dstIP, dstPort)
	} else if _, ok := r.lookupFlow(srcIP.String(), srcPort); !ok {
		// 非 SYN 且无映射：可能是漏网连接，补记一次避免回程 lookup 失败。
		target := tcpOriginalTarget{host: dstIP.String(), port: dstPort, seen: time.Now()}
		r.rememberFlow(srcIP.String(), srcPort, target)
	}
	redirectIP := srcIP.To4()
	if redirectIP == nil {
		return false, nil
	}
	Tracef("拦截 TCP %s:%d -> %s:%d 重定向到 %s:%d", srcIP, srcPort, dstIP, dstPort, redirectIP, redirectPort)
	p.SetDstIP(redirectIP)
	return false, p.SetDstPort(redirectPort)
}

func (r *tcpRedirector) rememberFlow(clientIP string, clientPort uint16, target tcpOriginalTarget) {
	r.remember(tcpFlowKey{ip: clientIP, port: clientPort}, target)
	if clientIP != "127.0.0.1" {
		r.remember(tcpFlowKey{ip: "127.0.0.1", port: clientPort}, target)
	}
}

func (r *tcpRedirector) lookupFlow(clientIP string, clientPort uint16) (tcpOriginalTarget, bool) {
	if t, ok := r.lookup(tcpFlowKey{ip: clientIP, port: clientPort}); ok {
		return t, true
	}
	if clientIP != "127.0.0.1" {
		if t, ok := r.lookup(tcpFlowKey{ip: "127.0.0.1", port: clientPort}); ok {
			return t, true
		}
	}
	return r.lookupByClientPort(clientPort)
}

func (r *tcpRedirector) lookupByClientPort(clientPort uint16) (tcpOriginalTarget, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found tcpOriginalTarget
	var n int
	for k, v := range r.flows {
		if k.port != clientPort {
			continue
		}
		n++
		found = v
	}
	if n == 1 {
		return found, true
	}
	return tcpOriginalTarget{}, false
}

func (r *tcpRedirector) remember(key tcpFlowKey, target tcpOriginalTarget) {
	r.mu.Lock()
	r.flows[key] = target
	r.mu.Unlock()
}

func (r *tcpRedirector) lookup(key tcpFlowKey) (tcpOriginalTarget, bool) {
	r.mu.Lock()
	target, ok := r.flows[key]
	r.mu.Unlock()
	return target, ok
}

func (r *tcpRedirector) cleanupLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-t.C:
			r.mu.Lock()
			for k, v := range r.flows {
				if now.Sub(v.seen) > tcpRedirectMapTTL {
					delete(r.flows, k)
				}
			}
			r.mu.Unlock()
		}
	}
}

func (r *tcpRedirector) acceptLoop() {
	for {
		c, err := r.listener.Accept()
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			log.Printf("[TUN] accept: %v", err)
			continue
		}
		log.Printf("[TUN-trace] 透明连接 accept remote=%s", c.RemoteAddr())
		go r.handleConn(c)
	}
}

func (r *tcpRedirector) handleConn(c net.Conn) {
	defer c.Close()
	ta, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	target, ok := r.lookupFlow(ta.IP.String(), uint16(ta.Port))
	if !ok {
		Tracef("找不到原始目标 remote=%s activeFlows=%d keys=%s",
			c.RemoteAddr(), r.flowCount(), r.flowKeysSummary())
		return
	}
	Tracef("透明连接 accept remote=%s 原始目标=%s:%d -> SOCKS %s",
		c.RemoteAddr(), target.host, target.port, r.socksAddr)
	var err error
	if r.tunnelDial != nil {
		Tracef("RelayTCPThrough 开始 dst=%s:%d（直连 feizhu TLS，不经 7891 回环）", target.host, target.port)
		err = socks5.RelayTCPThrough(c, target.host, target.port, r.tunnelDial)
	} else {
		err = socks5.RelayTransparent(c, r.socksAddr, target.host, target.port)
	}
	if err != nil {
		Tracef("经 SOCKS 转发失败 %s:%d: %v", target.host, target.port, err)
	} else {
		Debugf("经 SOCKS 转发完成 %s:%d", target.host, target.port)
	}
}

func (r *tcpRedirector) flowKeysSummary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.flows) == 0 {
		return "(empty)"
	}
	const max = 8
	var b strings.Builder
	n := 0
	for k, v := range r.flows {
		if n >= max {
			b.WriteString(" ...")
			break
		}
		if n > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s:%d->%s:%d", k.ip, k.port, v.host, v.port)
		n++
	}
	return b.String()
}

func (r *tcpRedirector) flowCount() int {
	r.mu.Lock()
	n := len(r.flows)
	r.mu.Unlock()
	return n
}
