//go:build windows

package tunmode

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/deblasis/godivert"
	"github.com/feizhu/feizhu/internal/socks5"
)

const tcpRedirectMapTTL = 2 * time.Minute

var redirectLocalIP = net.IPv4(127, 0, 0, 1)

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
	tunnelDial socks5.DialFunc
	listener   net.Listener
	divert     *godivert.WinDivertHandle
	bypass     map[uint32]struct{}

	mu    sync.Mutex
	flows map[tcpFlowKey]tcpOriginalTarget
}

func startWindowsTCPRedirect(ctx context.Context, cfg Config) (*Controller, error) {
	if cfg.TunnelDial == nil {
		return nil, fmt.Errorf("tunmode: Windows 透明代理需要 TunnelDial")
	}
	childCtx, cancel := context.WithCancel(ctx)
	r := &tcpRedirector{
		ctx:        childCtx,
		cancel:     cancel,
		tunnelDial: cfg.TunnelDial,
		bypass:     buildTCPBypassSet(cfg),
		flows:      make(map[tcpFlowKey]tcpOriginalTarget),
	}
	if err := r.start(); err != nil {
		cancel()
		return nil, err
	}
	log.Printf("[TUN] Windows TCP 透明代理（WinDivert）：仅拦截出站 TCP -> feizhu TLS；UDP/DNS 不拦截（与 macOS pf 一致）")
	return &Controller{
		cfg: cfg,
		stopFunc: func() {
			r.stop()
		},
	}, nil
}

func (r *tcpRedirector) start() error {
	if err := ensureWinDivertLoaded(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("tunmode: 透明代理监听失败: %w", err)
	}
	r.listener = ln
	redirectPort := uint16(ln.Addr().(*net.TCPAddr).Port)

	filter := buildWinDivertFilter(redirectPort)
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
		if err := r.rewritePacket(p, redirectPort); err != nil {
			log.Printf("[TUN] WinDivert rewrite: %v", err)
		}
		p.CalcNewChecksum(r.divert)
		_, _ = p.Send(r.divert)
	}
}

func (r *tcpRedirector) rewritePacket(p *godivert.Packet, redirectPort uint16) error {
	p.ParseHeaders()
	srcIP := p.SrcIP()
	dstIP := p.DstIP()
	srcPort, err := p.SrcPort()
	if err != nil {
		return err
	}
	dstPort, err := p.DstPort()
	if err != nil {
		return err
	}

	if srcPort == redirectPort {
		orig, ok := r.lookupFlow(dstIP.String(), dstPort)
		if !ok {
			return nil
		}
		p.SetSrcIP(net.ParseIP(orig.host))
		return p.SetSrcPort(orig.port)
	}
	if dstPort == redirectPort {
		return nil
	}
	if shouldBypassIPv4(dstIP, r.bypass) {
		return nil
	}
	target := tcpOriginalTarget{host: dstIP.String(), port: dstPort, seen: time.Now()}
	r.rememberFlow(srcIP.String(), srcPort, target)
	p.SetDstIP(redirectLocalIP)
	return p.SetDstPort(redirectPort)
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
		log.Printf("[TUN] 找不到原始目标 remote=%s flows=%d", c.RemoteAddr(), r.flowCount())
		return
	}
	if err := socks5.RelayTCPThrough(c, target.host, target.port, r.tunnelDial); err != nil {
		log.Printf("[TUN] 隧道 %s:%d: %v", target.host, target.port, err)
	}
}

func (r *tcpRedirector) flowCount() int {
	r.mu.Lock()
	n := len(r.flows)
	r.mu.Unlock()
	return n
}

func relayConn(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		_ = a.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		_ = b.Close()
	}()
	wg.Wait()
}
