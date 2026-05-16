package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/feizhu/feizhu/internal/clientrunner"
)

func main() {
	serverAddr := flag.String("server", "", "服务端地址 host:port（必填）")
	password := flag.String("password", "", "与服务端一致的密码（必填）")
	local := flag.String("listen", "127.0.0.1:7890", "本地 HTTP 代理监听地址")
	tlsInsecure := flag.Bool("tls-insecure", true, "跳过服务端 TLS 证书校验（自签名时必须为 true）")
	sni := flag.String("sni", "", "TLS 握手 SNI；默认同 -server 的 host")
	skipLoginCheck := flag.Bool("skip-login-check", false, "启动时不向服务端校验密码（不推荐，仅排障）")
	autoProxy := flag.Bool("auto-proxy", true, "启动时自动设置系统 HTTP/HTTPS(/SOCKS) 代理，退出(Ctrl+C)时还原")
	autoEnv := flag.Bool("auto-env", true, "启动时设置用户级代理环境变量(launchctl/systemd/注册表)，退出时还原；macOS 上已运行的终端需 Cmd+Q 重开方继承")
	autoCurlrc := flag.Bool("auto-curlrc", true, "在 ~/.curlrc 追加 include，使 curl 走本地代理（退出时还原）；不依赖终端是否重启")
	networkService := flag.String("network-service", "", "仅 macOS：networksetup 服务名（如 Wi-Fi）；空则自动选择")
	socks := flag.Bool("socks", true, "启用本地 SOCKS5；关闭后不监听、不配置系统 SOCKS")
	socksListen := flag.String("socks-listen", "127.0.0.1:7891", "SOCKS5 监听地址（-socks=false 时忽略）")
	printProxyEnv := flag.Bool("print-proxy-env", false, "仅打印终端用 http(s)_proxy 等环境变量脚本并退出（无需 -server/-password）")
	flag.Parse()

	if *printProxyEnv {
		clientrunner.PrintShellProxyExports(*local, *socksListen, *socks)
		os.Exit(0)
	}

	if *serverAddr == "" || *password == "" {
		flag.Usage()
		os.Exit(2)
	}

	if _, _, err := net.SplitHostPort(*serverAddr); err != nil {
		log.Fatalf("-server 格式应为 host:port: %v", err)
	}

	cfg := clientrunner.Config{
		ServerAddr:     *serverAddr,
		Password:       *password,
		LocalListen:    *local,
		TLSInsecure:    *tlsInsecure,
		SNI:            *sni,
		SkipLoginCheck: *skipLoginCheck,
		AutoProxy:      *autoProxy,
		AutoEnv:        *autoEnv,
		AutoCurlrc:     *autoCurlrc,
		NetworkService: *networkService,
		SOCKS:          *socks,
		SOCKSListen:    *socksListen,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := clientrunner.Run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}
