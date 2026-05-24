// Package clientrunner 承载 feizhu-client 的运行逻辑，供 CLI 与 GUI 共用。
package clientrunner

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/feizhu/feizhu/internal/curlrc"
	"github.com/feizhu/feizhu/internal/proxyenv"
	"github.com/feizhu/feizhu/internal/socks5"
	"github.com/feizhu/feizhu/internal/sysproxy"
	"github.com/feizhu/feizhu/internal/tunmode"
	"github.com/feizhu/feizhu/internal/tunnel"
	"github.com/feizhu/feizhu/internal/upstreamproxy"
)

var tunnelSeq atomic.Uint64

// Config 与 feizhu-client 命令行参数对应。
type Config struct {
	ServerAddr     string
	Password       string
	LocalListen    string
	TLSInsecure    bool
	SNI            string
	SkipLoginCheck bool
	AutoProxy      bool
	AutoEnv        bool
	AutoCurlrc     bool
	NetworkService string
	SOCKS          bool
	SOCKSListen    string
	TUN            bool
	TUNDevice      string
	TUNAddress     string
	TUNMTU         int
	TUNDebug       bool
	// UpstreamProxy 为本地 HTTP/SOCKS 出口链路上级（http/socks5 URL）。
	// 配置后：经 127.0.0.1:7890/7891 的流量可走上级；TUN 拦截的公网 TCP 一律 feizhu TLS。
	UpstreamProxy string
	// LogWriter 非 nil 时，标准 log 包输出会定向到此（例如 GUI 日志区）。
	LogWriter io.Writer
	// SuppressPerConnLogs 为 true 时不输出每个 HTTP/SOCKS 连接的「上线/转发/下线」流水日志，
	// 仅保留错误与启动提示。GUI 高频上网时若不关闭，极易拖垮 Fyne 的 binding 队列与内存。
	SuppressPerConnLogs bool
}

// VerifyRemoteLogin 向服务端校验密码（TLS + 探测帧），与 CLI 启动时一致。
func VerifyRemoteLogin(serverAddr, password string, tlsInsecure bool, sni string) error {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return fmt.Errorf("-server 格式应为 host:port: %w", err)
	}
	sniName := sni
	if sniName == "" {
		sniName = host
	}
	tlsCfg := &tls.Config{
		ServerName:         sniName,
		InsecureSkipVerify: tlsInsecure,
		MinVersion:         tls.VersionTLS12,
	}

	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.Dial("tcp", serverAddr)
	if err != nil {
		return fmt.Errorf("连接服务端: %w", err)
	}
	tlsConn := tls.Client(raw, tlsCfg)
	defer tlsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("TLS 握手: %w", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	if err := tunnel.ClientVerifyLogin(tlsConn, password, deadline); err != nil {
		return err
	}
	return nil
}

// Run 启动本地 HTTP/SOCKS 代理与系统代理等逻辑；在 ctx 取消时关闭监听并退出。
func Run(ctx context.Context, cfg Config) error {
	prevLog := log.Writer()
	if cfg.LogWriter != nil {
		log.SetOutput(cfg.LogWriter)
		defer log.SetOutput(prevLog)
	}

	host, _, err := net.SplitHostPort(cfg.ServerAddr)
	if err != nil {
		return fmt.Errorf("-server 格式应为 host:port: %w", err)
	}
	sniName := cfg.SNI
	if sniName == "" {
		sniName = host
	}
	tlsCfg := &tls.Config{
		ServerName:         sniName,
		InsecureSkipVerify: cfg.TLSInsecure,
		MinVersion:         tls.VersionTLS12,
	}

	if cfg.SkipLoginCheck {
		log.Println("[启动] 已跳过远端登录校验（-skip-login-check），密码错误时仍会在首次访问网站时失败")
	} else {
		if err := VerifyRemoteLogin(cfg.ServerAddr, cfg.Password, cfg.TLSInsecure, cfg.SNI); err != nil {
			return fmt.Errorf("[启动] 远端登录校验失败: %w", err)
		}
		log.Printf("[启动] 远端登录校验成功 server=%s", cfg.ServerAddr)
	}

	var ln net.Listener
	var socksLn net.Listener
	var sh, sp string
	var ph, pp string
	var httpURL, socksURL string

	ln, err = net.Listen("tcp", cfg.LocalListen)
	if err != nil {
		return fmt.Errorf("本地监听失败: %w", err)
	}
	if cfg.SOCKS {
		socksLn, err = net.Listen("tcp", cfg.SOCKSListen)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("SOCKS5 监听失败: %w", err)
		}
		sh, sp, err = sysproxy.ParseListenAddr(cfg.SOCKSListen)
		if err != nil {
			_ = ln.Close()
			_ = socksLn.Close()
			return fmt.Errorf("[SOCKS] %w", err)
		}
	}
	if cfg.TUN && !cfg.SOCKS {
		_ = ln.Close()
		return fmt.Errorf("[TUN] TUN 模式依赖本地 SOCKS5，请保持 -socks=true")
	}

	ph, pp, err = sysproxy.ParseListenAddr(cfg.LocalListen)
	if err != nil {
		_ = ln.Close()
		if socksLn != nil {
			_ = socksLn.Close()
		}
		return fmt.Errorf("-listen: %w", err)
	}
	httpURL = fmt.Sprintf("http://%s:%s", ph, pp)
	if cfg.SOCKS {
		socksURL = fmt.Sprintf("socks5://%s:%s", sh, sp)
	}

	var upstream *url.URL
	if s := strings.TrimSpace(cfg.UpstreamProxy); s != "" {
		upstream, err = upstreamproxy.Parse(s)
		if err != nil {
			if ln != nil {
				_ = ln.Close()
			}
			if socksLn != nil {
				_ = socksLn.Close()
			}
			return fmt.Errorf("[上级代理] %w", err)
		}
		log.Printf("[上级代理] 已配置 %s；上级代理握手将包在 feizhu TLS 隧道内，由 feizhu-server 连接上级代理。", upstream.Host)
	}

	dialer := &targetDialer{
		upstream:   upstream,
		serverAddr: cfg.ServerAddr,
		password:   cfg.Password,
		tlsCfg:     tlsCfg,
		logTUN:     cfg.TUN,
	}

	var tunCtl *tunmode.Controller
	if cfg.TUN {
		if err := curlrc.ClearManaged(); err != nil {
			log.Printf("[TUN] 清理 ~/.curlrc 中旧的 feizhu 代理段失败: %v", err)
		}
		tunCtl, err = tunmode.Start(ctx, tunmode.Config{
			Enabled:     true,
			DeviceName:  cfg.TUNDevice,
			AddressCIDR: cfg.TUNAddress,
			MTU:         cfg.TUNMTU,
			SOCKSListen: cfg.SOCKSListen,
			LocalListen: cfg.LocalListen,
			ServerAddr:  cfg.ServerAddr,
			TUNDebug:    cfg.TUNDebug,
			BypassIPs:   tunBypassIPsForPlatform(upstream),
			TunnelDial:  dialer.dialTUN,
		})
		if err != nil {
			if ln != nil {
				_ = ln.Close()
			}
			if socksLn != nil {
				_ = socksLn.Close()
			}
			return fmt.Errorf("[TUN] 启动失败: %w", err)
		}
		log.Println("[TUN] 已启用虚拟网卡透明代理模式；TCP 流量将经本地 SOCKS5 再进入 feizhu TLS 隧道，DNS UDP/53 会转为隧道内 TCP 查询。")
		logTUNPlatformMessages(upstream != nil)
	}
	defer func() {
		if tunCtl != nil {
			tunCtl.Stop()
			log.Println("[TUN] 已停止并尝试还原路由")
		}
	}()

	if cfg.AutoProxy {
		if err := sysproxy.Apply(ph, pp, sh, sp, cfg.NetworkService); err != nil {
			_ = ln.Close()
			if socksLn != nil {
				_ = socksLn.Close()
			}
			return fmt.Errorf("[系统代理] 设置失败: %w\n提示: 可加 -auto-proxy=false 仅启动本地代理，再手动配置系统/浏览器代理", err)
		}
		if cfg.SOCKS {
			log.Printf("[系统代理] 已启用 HTTP/HTTPS -> %s:%s ，SOCKS5 -> %s:%s（退出本程序后将自动还原）", ph, pp, sh, sp)
		} else {
			log.Printf("[系统代理] 已启用 HTTP/HTTPS -> %s:%s（退出本程序后将自动还原）", ph, pp)
		}
	}

	if cfg.AutoEnv {
		if err := proxyenv.Apply(proxyenv.Config{
			HTTPProxyURL:  httpURL,
			SOCKSProxyURL: socksURL,
			EnableSOCKS:   cfg.SOCKS && socksURL != "",
		}); err != nil {
			log.Printf("[代理环境] 设置失败: %v（可加 -auto-env=false 关闭）", err)
		} else {
			log.Println("[代理环境] 已写入用户级 http_proxy 等（macOS: launchctl；Linux: systemctl --user；Windows: 用户环境）。退出 feizhu-client 后自动还原。")
			if runtime.GOOS == "darwin" {
				out, _ := exec.Command("launchctl", "getenv", "http_proxy").Output()
				log.Printf("[代理环境] launchctl getenv http_proxy=%q（若为空说明当前上下文读不到，可忽略并依赖 -auto-curlrc）", strings.TrimSpace(string(out)))
				log.Println("[代理环境] macOS: 若「终端」在 feizhu-client 启动前就开着，仅「新建窗口」通常仍无 http_proxy；请 **Cmd+Q 完全退出终端再打开**，或依赖 ~/.curlrc（-auto-curlrc，默认开启）。")
			}
		}
	}

	if cfg.AutoCurlrc {
		if err := curlrc.Apply(httpURL); err != nil {
			log.Printf("[curl] 写入 ~/.curlrc 失败: %v（可加 -auto-curlrc=false）", err)
		} else {
			log.Println("[curl] 已在 ~/.curlrc 写入代理段（proxy + noproxy；curl 8 均识别）。任意新终端执行 curl 即生效（勿用 curl -q）。退出 feizhu-client 后自动删除该段。")
		}
	}

	defer func() {
		if !cfg.AutoCurlrc {
			return
		}
		if err := curlrc.Restore(); err != nil {
			log.Printf("[curl] 还原 ~/.curlrc 失败: %v", err)
		}
	}()
	defer func() {
		if !cfg.AutoEnv {
			return
		}
		if err := proxyenv.Restore(); err != nil {
			log.Printf("[代理环境] 还原失败: %v（可手动检查 http_proxy 等）", err)
		} else {
			log.Println("[代理环境] 已还原用户级环境变量")
		}
	}()
	defer func() {
		if !cfg.AutoProxy {
			return
		}
		if err := sysproxy.Restore(); err != nil {
			log.Printf("[系统代理] 还原失败: %v（请手动检查系统代理设置）", err)
		} else {
			log.Println("[系统代理] 已还原")
		}
	}()

	go func() {
		<-ctx.Done()
		log.Println("[退出] 正在关闭监听…")
		_ = ln.Close()
		if socksLn != nil {
			_ = socksLn.Close()
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[运行] HTTP 代理 %s -> 远端 %s (TLS 隧道)", cfg.LocalListen, cfg.ServerAddr)
		for {
			c, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("http accept: %v", err)
				continue
			}
			go handleLocalConn(c, dialer, cfg.SuppressPerConnLogs)
		}
	}()

	if socksLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("[运行] SOCKS5 监听 %s -> 经 TLS 隧道转发（目标地址仅在加密帧中发往 server）", cfg.SOCKSListen)
			for {
				c, err := socksLn.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("socks accept: %v", err)
					continue
				}
				go handleSOCKS(c, dialer, cfg.SuppressPerConnLogs, cfg.TUN)
			}
		}()
	}

	logTerminalCurlHint(cfg.LocalListen, cfg.SOCKSListen, cfg.SOCKS, cfg.AutoEnv, cfg.AutoCurlrc)

	wg.Wait()
	return nil
}

// PrintShellProxyExports 打印供 eval 的 export 语句（与 CLI -print-proxy-env 一致）。
func PrintShellProxyExports(local, socksListen string, useSocks bool) {
	ph, pp, err := sysproxy.ParseListenAddr(local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	base := fmt.Sprintf("http://%s:%s", ph, pp)
	fmt.Fprintln(os.Stderr, "说明: 终端 curl/wget 读的是下列环境变量，不会用「系统设置」里的 HTTP 代理。")
	fmt.Fprintln(os.Stderr, "用法: eval \"$(feizhu-client -print-proxy-env ...)\"  （参数与正在运行的 client 一致）")
	fmt.Printf("export http_proxy=%q\n", base)
	fmt.Printf("export https_proxy=%q\n", base)
	fmt.Printf("export HTTP_PROXY=%q\n", base)
	fmt.Printf("export HTTPS_PROXY=%q\n", base)
	if useSocks {
		sh, sp, err := sysproxy.ParseListenAddr(socksListen)
		if err == nil {
			su := fmt.Sprintf("socks5://%s:%s", sh, sp)
			fmt.Printf("export all_proxy=%q\n", su)
			fmt.Printf("export ALL_PROXY=%q\n", su)
		}
	}
	fmt.Fprintf(os.Stderr, "# 单次 curl: curl -x %s https://www.baidu.com\n", base)
}

func logTerminalCurlHint(local, socksListen string, useSocks, autoEnv, autoCurlrc bool) {
	ph, pp, err := sysproxy.ParseListenAddr(local)
	if err != nil {
		return
	}
	base := fmt.Sprintf("http://%s:%s", ph, pp)
	var evalCmd string
	if useSocks {
		evalCmd = fmt.Sprintf("eval \"$(feizhu-client -print-proxy-env -listen %s -socks-listen %s)\"", local, socksListen)
	} else {
		evalCmd = fmt.Sprintf("eval \"$(feizhu-client -print-proxy-env -listen %s -socks=false)\"", local)
	}
	switch {
	case autoCurlrc:
		if runtime.GOOS == "windows" {
			_, _ = io.WriteString(log.Writer(), "[提示] 已默认启用 -auto-curlrc：curl 会读 %USERPROFILE%\\.curlrc 中的代理段（与是否新开 cmd 窗口无关）。若仍直连：勿用 curl -q；可在 cmd 执行: type %USERPROFILE%\\.curlrc | findstr feizhu\n")
		} else {
			log.Println("[提示] 已默认启用 -auto-curlrc：curl 直接读 ~/.curlrc 内嵌的代理段，**与「新建窗口 / 新标签」无关**，一般无需 Cmd+Q。若仍直连：确认未使用 curl -q，并执行 `grep feizhu ~/.curlrc` 是否有本程序写入的标记行。")
		}
	case autoEnv && runtime.GOOS == "darwin":
		log.Println("[提示] 仅依赖 launchctl 时：请 Cmd+Q 完全退出「终端」再打开；新建标签通常仍无 http_proxy。")
	default:
		log.Println("[提示] 可启用 -auto-env / -auto-curlrc，或手动执行:")
	}
	log.Printf("       %s", evalCmd)
	log.Println("       （若程序不在 PATH 里请写完整路径；参数需与当前 feizhu-client 一致）")
	log.Printf("       单次测试: curl -x %s https://www.baidu.com", base)
}

type targetDialer struct {
	upstream   *url.URL
	serverAddr string
	password   string
	tlsCfg     *tls.Config
	logTUN     bool
}

func handleSOCKS(client net.Conn, dialer *targetDialer, suppressPerConn, fromTUN bool) {
	sid := tunnelSeq.Add(1)
	peer := client.RemoteAddr().String()
	if fromTUN {
		log.Printf("[TUN-trace] [socks #%d] 来自透明拦截 peer=%s", sid, peer)
	} else if !suppressPerConn {
		log.Printf("[socks #%d] 连接 %s", sid, peer)
	}
	dialFn := dialer.dialLocal
	via := "feizhu TLS 隧道"
	if fromTUN {
		dialFn = dialer.dialTUN
	} else if dialer.upstream != nil {
		via = "上级代理"
	}
	err := socks5.Serve(client, func(host string, port uint16) (net.Conn, error) {
		if fromTUN {
			log.Printf("[TUN-trace] [socks #%d] CONNECT %s:%d（经 %s）", sid, host, port, via)
		} else if !suppressPerConn {
			log.Printf("[socks #%d] 请求 CONNECT %s:%d（经 %s）", sid, host, port, via)
		}
		conn, err := dialFn(host, port)
		if err != nil && fromTUN {
			log.Printf("[TUN-trace] [socks #%d] 拨号失败 %s:%d: %v", sid, host, port, err)
			logTUNDialHintOnce(dialer.upstream, host, err)
		}
		return conn, err
	})
	if err != nil {
		if fromTUN {
			log.Printf("[TUN-trace] [socks #%d] 结束 %s err=%v", sid, peer, err)
		} else {
			log.Printf("[socks #%d] 结束 %s err=%v", sid, peer, err)
		}
	} else if fromTUN {
		log.Printf("[TUN-trace] [socks #%d] 正常结束 %s", sid, peer)
	} else if !suppressPerConn {
		log.Printf("[socks #%d] 结束 %s", sid, peer)
	}
}

func handleLocalConn(client net.Conn, dialer *targetDialer, suppressPerConn bool) {
	defer client.Close()
	br := bufio.NewReader(client)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if req.Method == http.MethodConnect {
			handleConnect(client, br, req, dialer, suppressPerConn)
			return
		}
		if err := handleHTTP(client, br, req, dialer, suppressPerConn); err != nil {
			return
		}
		if req.Close {
			return
		}
	}
}

func handleConnect(client net.Conn, br *bufio.Reader, req *http.Request, dialer *targetDialer, suppressPerConn bool) {
	host, portStr, err := net.SplitHostPort(req.Host)
	if err != nil {
		if strings.ContainsRune(req.Host, ':') {
			_ = writeHTTPLine(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
			return
		}
		host = req.Host
		portStr = "443"
	}
	var port uint64 = 443
	if portStr != "" {
		port, err = parsePort(portStr)
		if err != nil {
			_ = writeHTTPLine(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
			return
		}
	}

	sid := tunnelSeq.Add(1)
	browser := client.RemoteAddr().String()
	if !suppressPerConn {
		log.Printf("[隧道 #%d] 上线(浏览器) %s CONNECT %s", sid, browser, req.Host)
	}

	via := "feizhu TLS 隧道"
	if dialer.upstream != nil {
		via = "上级代理 " + dialer.upstream.Host
	}
	rc, err := dialer.dialLocal(host, uint16(port))
	if err != nil {
		log.Printf("[隧道 #%d] 建立失败(%s) CONNECT %s err=%v", sid, via, req.Host, err)
		_ = writeHTTPLine(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		log.Printf("[隧道 #%d] 下线(未建立转发) %s", sid, browser)
		return
	}
	defer func() {
		rc.Close()
		if !suppressPerConn {
			log.Printf("[隧道 #%d] 下线 %s -> %s", sid, browser, req.Host)
		}
	}()

	if err := writeHTTPLine(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	if !suppressPerConn {
		log.Printf("[隧道 #%d] 开始双向转发 %s <-> %s（%s）", sid, browser, req.Host, via)
	}
	relay(br, client, rc)
}

func handleHTTP(client net.Conn, br *bufio.Reader, req *http.Request, dialer *targetDialer, suppressPerConn bool) error {
	if req.URL == nil {
		_ = writeHTTPLine(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return fmt.Errorf("bad url")
	}
	u := req.URL
	if !u.IsAbs() {
		_ = writeHTTPLine(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return fmt.Errorf("absolute url required")
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		if u.Scheme == "https" {
			portStr = "443"
		} else {
			portStr = "80"
		}
	}
	port, err := parsePort(portStr)
	if err != nil || host == "" {
		_ = writeHTTPLine(client, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return fmt.Errorf("bad host/port")
	}

	sid := tunnelSeq.Add(1)
	browser := client.RemoteAddr().String()
	if !suppressPerConn {
		log.Printf("[隧道 #%d] 上线(浏览器) %s %s %s", sid, browser, req.Method, u.String())
	}

	rc, err := dialer.dialLocal(host, uint16(port))
	if err != nil {
		log.Printf("[隧道 #%d] 建立失败 %s err=%v", sid, u.String(), err)
		_ = writeHTTPLine(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		log.Printf("[隧道 #%d] 下线(未建立转发) %s", sid, browser)
		return err
	}
	defer func() {
		rc.Close()
		if !suppressPerConn {
			log.Printf("[隧道 #%d] 下线 %s", sid, browser)
		}
	}()

	req.RequestURI = ""
	if err := req.Write(rc); err != nil {
		return err
	}

	if !suppressPerConn {
		log.Printf("[隧道 #%d] 开始双向转发 %s <-> %s", sid, browser, u.Host)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(rc, br)
		rc.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, rc)
	}()
	wg.Wait()
	return nil
}

func relay(br *bufio.Reader, client net.Conn, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(remote, br)
		remote.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, remote)
		client.Close()
	}()
	wg.Wait()
}

func writeHTTPLine(c net.Conn, s string) error {
	_, err := io.WriteString(c, s)
	return err
}

func parsePort(s string) (uint64, error) {
	var p uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("bad port")
		}
		p = p*10 + uint64(ch-'0')
		if p > 65535 {
			return 0, fmt.Errorf("bad port")
		}
	}
	if p == 0 {
		return 0, fmt.Errorf("bad port")
	}
	return p, nil
}
