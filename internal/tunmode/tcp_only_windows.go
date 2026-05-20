//go:build windows

package tunmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/deblasis/godivert"
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
	ctx       context.Context
	cancel    context.CancelFunc
	socksAddr string
	listener  net.Listener
	divert    *godivert.WinDivertHandle

	mu    sync.Mutex
	flows map[tcpFlowKey]tcpOriginalTarget
}

func startWindowsTCPOnly(ctx context.Context, cfg Config, socksAddr string) (*Controller, error) {
	childCtx, cancel := context.WithCancel(ctx)
	r := &tcpRedirector{
		ctx:       childCtx,
		cancel:    cancel,
		socksAddr: socksAddr,
		flows:     make(map[tcpFlowKey]tcpOriginalTarget),
	}
	if err := r.start(cfg); err != nil {
		cancel()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		r.stop()
	}()
	return &Controller{
		cfg: cfg,
		stopFunc: func() {
			r.stop()
		},
	}, nil
}

func (r *tcpRedirector) start(cfg Config) error {
	ln, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return fmt.Errorf("tunmode: Windows TCP-only 监听失败: %w", err)
	}
	r.listener = ln
	redirectPort := uint16(ln.Addr().(*net.TCPAddr).Port)

	filter := buildWinDivertFilter(cfg, redirectPort)
	wd, err := godivert.NewWinDivertHandle(filter)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("tunmode: 启动 WinDivert TCP-only 失败: %w（请确认以管理员运行，并已随程序放置 WinDivert.dll/WinDivert64.sys）", err)
	}
	r.divert = wd

	go r.acceptLoop()
	go r.packetLoop(redirectPort)
	go r.cleanupLoop()
	log.Printf("[TUN] Windows TCP-only 已启用（WinDivert），仅 TCP 会透明进入 feizhu，UDP/DNS 不拦截。redirectPort=%d", redirectPort)
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

func buildWinDivertFilter(cfg Config, redirectPort uint16) string {
	var excluded []string
	excluded = append(excluded,
		"ip.DstAddr >= 0.0.0.0 and ip.DstAddr <= 0.255.255.255",
		"ip.DstAddr >= 10.0.0.0 and ip.DstAddr <= 10.255.255.255",
		"ip.DstAddr >= 100.64.0.0 and ip.DstAddr <= 100.127.255.255",
		"ip.DstAddr >= 127.0.0.0 and ip.DstAddr <= 127.255.255.255",
		"ip.DstAddr >= 169.254.0.0 and ip.DstAddr <= 169.254.255.255",
		"ip.DstAddr >= 172.16.0.0 and ip.DstAddr <= 172.31.255.255",
		"ip.DstAddr >= 192.168.0.0 and ip.DstAddr <= 192.168.255.255",
		"ip.DstAddr >= 224.0.0.0",
	)
	for _, ip := range resolveIPv4Host(hostFromAddr(cfg.ServerAddr)) {
		excluded = append(excluded, "ip.DstAddr == "+ip.String())
	}
	for _, ip := range append(windowsDNSIPs(), dohBypassIPs()...) {
		excluded = append(excluded, "ip.DstAddr == "+ip.String())
	}
	for _, ip := range cfg.BypassIPs {
		ip = strings.TrimSpace(ip)
		if net.ParseIP(ip).To4() != nil {
			excluded = append(excluded, "ip.DstAddr == "+ip)
		}
	}
	return fmt.Sprintf(
		"tcp and ip and ((outbound and tcp.DstPort != %d and not (%s)) or (loopback and tcp.SrcPort == %d))",
		redirectPort,
		strings.Join(excluded, " or "),
		redirectPort,
	)
}

func hostFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
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
			_, _ = p.Send(r.divert)
			continue
		}
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
		key := tcpFlowKey{ip: dstIP.String(), port: dstPort}
		orig, ok := r.lookup(key)
		if !ok {
			return nil
		}
		p.SetSrcIP(net.ParseIP(orig.host))
		return p.SetSrcPort(orig.port)
	}
	if dstPort != redirectPort {
		key := tcpFlowKey{ip: srcIP.String(), port: srcPort}
		r.remember(key, tcpOriginalTarget{host: dstIP.String(), port: dstPort, seen: time.Now()})
		p.SetDstIP(srcIP)
		return p.SetDstPort(redirectPort)
	}
	return nil
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
			log.Printf("[TUN] Windows TCP-only accept: %v", err)
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
	target, ok := r.lookup(tcpFlowKey{ip: ta.IP.String(), port: uint16(ta.Port)})
	if !ok {
		log.Printf("[TUN] Windows TCP-only 找不到原始目标 remote=%s", c.RemoteAddr())
		return
	}
	rc, err := dialSOCKS5Connect(r.socksAddr, target.host, target.port)
	if err != nil {
		log.Printf("[TUN] Windows TCP-only SOCKS CONNECT %s:%d: %v", target.host, target.port, err)
		return
	}
	defer rc.Close()
	relayConn(c, rc)
}

func dialSOCKS5Connect(socksAddr, host string, port uint16) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", socksAddr, 20*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		c.Close()
		return nil, err
	}
	buf := make([]byte, 260)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		c.Close()
		return nil, err
	}
	if buf[0] != 5 || buf[1] != 0 {
		c.Close()
		return nil, errors.New("socks5: no acceptable auth method")
	}
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			c.Close()
			return nil, errors.New("socks5: host too long")
		}
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		c.Close()
		return nil, err
	}
	if buf[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5: connect failed rep=%d", buf[1])
	}
	switch buf[3] {
	case 1:
		_, err = io.ReadFull(c, buf[:4+2])
	case 3:
		if _, err = io.ReadFull(c, buf[:1]); err == nil {
			_, err = io.ReadFull(c, buf[:int(buf[0])+2])
		}
	case 4:
		_, err = io.ReadFull(c, buf[:16+2])
	default:
		err = errors.New("socks5: bad atyp")
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
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
