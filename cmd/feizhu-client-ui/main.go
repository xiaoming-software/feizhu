// feizhu-client-ui：飞猪客户端图形界面（Fyne，可交叉编译 macOS / Windows 桌面端）。
package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"net"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/feizhu/feizhu/internal/clientrunner"
	"github.com/feizhu/feizhu/internal/curlrc"
	"github.com/feizhu/feizhu/internal/proxyenv"
	"github.com/feizhu/feizhu/internal/sysproxy"
	"github.com/feizhu/feizhu/internal/uistore"
)

// maxLogBytes 日志文本在内存中的上限（按 UTF-8 字节计）。超过则从最早完整行开始丢弃，保留最近内容，
// 避免长期运行无界增长。注意：接近上限时单次刷新字符串较大，若仍勾选「每条连接日志」且机器较弱，界面可能略顿。
const maxLogBytes = 1024 * 10

const logFlushDebounce = 200 * time.Millisecond

// debouncedBindingLog 将多行 log 合并后再写入 binding，避免每条日志触发一次 Fyne binding 派发。
// fyne/data/binding 使用无界队列异步回调；代理每个连接多行日志时，高频 Set 会导致内存暴涨与界面卡死。
// 正文总量由 maxLogBytes 限制，超出部分从头部整行丢弃。
type debouncedBindingLog struct {
	b     binding.String
	mu    sync.Mutex
	raw   []byte
	lines bytes.Buffer
	tmu   sync.Mutex
	t     *time.Timer
}

func newDebouncedBindingLog(b binding.String) *debouncedBindingLog {
	return &debouncedBindingLog{b: b}
}

func (w *debouncedBindingLog) dropFirstLine() {
	b := w.lines.Bytes()
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		w.lines.Reset()
		return
	}
	_ = w.lines.Next(i + 1)
}

func (w *debouncedBindingLog) capTotal() {
	for w.lines.Len()+len(w.raw) > maxLogBytes {
		if w.lines.Len() == 0 {
			if len(w.raw) > maxLogBytes {
				w.raw = append([]byte(nil), w.raw[len(w.raw)-maxLogBytes:]...)
			}
			return
		}
		w.dropFirstLine()
	}
}

func (w *debouncedBindingLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.raw = append(w.raw, p...)
	for {
		i := bytes.IndexByte(w.raw, '\n')
		if i < 0 {
			break
		}
		_, _ = w.lines.Write(w.raw[:i+1])
		w.raw = w.raw[i+1:]
		w.capTotal()
	}
	w.capTotal()
	w.mu.Unlock()
	w.scheduleFlush()
	return len(p), nil
}

func (w *debouncedBindingLog) flushToBinding() {
	w.mu.Lock()
	prefix := w.lines.Bytes()
	tail := append([]byte(nil), w.raw...)
	n := len(prefix) + len(tail)
	if n == 0 {
		w.mu.Unlock()
		return
	}
	var display []byte
	if n <= maxLogBytes {
		display = make([]byte, 0, n)
		display = append(display, prefix...)
		display = append(display, tail...)
	} else {
		tmp := append(append([]byte(nil), prefix...), tail...)
		display = tmp[len(tmp)-maxLogBytes:]
		if nl := bytes.IndexByte(display, '\n'); nl >= 0 && nl < len(display)-1 {
			display = display[nl+1:]
		}
	}
	text := string(display)
	w.mu.Unlock()
	_ = w.b.Set(text)
}

// Flush 立即把缓冲区写入 binding（停止代理或退出前调用，避免末尾约 debounce 时间内日志未显示）。
func (w *debouncedBindingLog) Flush() {
	w.tmu.Lock()
	if w.t != nil {
		w.t.Stop()
		w.t = nil
	}
	w.tmu.Unlock()
	w.flushToBinding()
}

func (w *debouncedBindingLog) scheduleFlush() {
	w.tmu.Lock()
	defer w.tmu.Unlock()
	if w.t != nil {
		w.t.Stop()
	}
	w.t = time.AfterFunc(logFlushDebounce, func() {
		w.flushToBinding()
		w.tmu.Lock()
		w.t = nil
		w.tmu.Unlock()
	})
}

func (w *debouncedBindingLog) Clear() {
	w.tmu.Lock()
	if w.t != nil {
		w.t.Stop()
		w.t = nil
	}
	w.tmu.Unlock()
	w.mu.Lock()
	w.lines.Reset()
	w.raw = nil
	w.mu.Unlock()
	_ = w.b.Set("")
}

func main() {
	if runHelperIfRequested() {
		return
	}

	a := app.NewWithID("github.com/feizhu.feizhu.feizhu-client-ui")
	a.Settings().SetTheme(newSciFiTheme())
	icon := appIcon()
	a.SetIcon(icon)
	w := a.NewWindow("飞猪 Feizhu 客户端")
	w.SetIcon(icon)
	w.Resize(fyne.NewSize(840, 600))
	w.SetFixedSize(false)

	// 自签名证书场景：与 CLI 默认一致，始终跳过 TLS 证书校验（界面不再暴露开关）。
	const tlsInsecure = true

	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("IP 或域名")
	portEntry := widget.NewEntry()
	portEntry.SetText("8443")
	portEntry.SetPlaceHolder("端口")

	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("密码")

	upstreamEntry := widget.NewEntry()
	upstreamEntry.SetPlaceHolder("上级代理（可选）如 http://user:pass@15.235.183.47:2000")

	if saved, err := uistore.Load(); err == nil && saved != nil {
		if saved.Host != "" {
			hostEntry.SetText(saved.Host)
		}
		if saved.Port != "" {
			portEntry.SetText(saved.Port)
		}
		if saved.Password != "" {
			passEntry.SetText(saved.Password)
		}
		if saved.UpstreamProxy != "" {
			upstreamEntry.SetText(saved.UpstreamProxy)
		}
	}

	// 端口仅 1～65535（最多 5 位），用固定单元格宽度；勿用 Stack(透明块+Entry)：
	// Stack 的 MinSize 为子项 Max，Entry 默认 Min 很宽，会把端口列撑得和地址框一样宽。
	portCellH := portEntry.MinSize().Height
	if portCellH < 1 {
		portCellH = 36
	}
	portCell := container.NewGridWrap(fyne.NewSize(96, portCellH), portEntry)

	statusLabel := widget.NewLabel("")
	if hostEntry.Text != "" && passEntry.Text != "" {
		statusLabel.SetText("已加载上次保存的连接信息，可直接点击「登录」。")
	} else {
		statusLabel.SetText("填写服务器与密码后点击「登录」。")
	}

	logStr := binding.NewString()
	logLabel := widget.NewLabelWithData(logStr)
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logScroll := container.NewScroll(container.NewMax(logLabel))
	// 日志绑定变更后滚到底部；勿再对 Label 重复 Refresh（LabelWithData 已会更新）。
	logStr.AddListener(binding.NewDataListener(func() {
		logScroll.ScrollToBottom()
	}))
	logBg := canvas.NewRectangle(color.NRGBA{R: 0x06, G: 0x0f, B: 0x18, A: 0xff})
	logBg.CornerRadius = 10
	logBg.StrokeColor = color.NRGBA{R: 0x2e, G: 0xc4, B: 0xb4, A: 0x55}
	logBg.StrokeWidth = 1
	logPanel := container.NewStack(
		logBg,
		container.NewThemeOverride(logScroll, newLogConsoleTheme()),
	)

	logWriter := newDebouncedBindingLog(logStr)

	// 默认不输出每条连接的流水日志（避免拖垮界面）；需要排障时可勾选，下次点击「登录」生效。
	verboseConnCheck := widget.NewCheck("显示每条连接的隧道日志（量多易卡）", nil)
	tunCheck := widget.NewCheck("启用 TUN 全局透明代理（会弹系统授权，仅 TCP）", nil)

	var (
		runMu              sync.Mutex
		runCancel          context.CancelFunc
		elevatedChild      *elevatedProcess
		userProxyFallback  bool
		userCurlrcFallback bool
		userEnvFallback    bool
		runWG              sync.WaitGroup
		running            bool
	)

	loginBtn := widget.NewButton("登录", nil)
	loginBtn.Importance = widget.HighImportance
	stopBtn := widget.NewButton("停止", nil)
	stopBtn.Importance = widget.DangerImportance
	stopBtn.Disable()

	setRunning := func(on bool) {
		runMu.Lock()
		running = on
		runMu.Unlock()
		if on {
			loginBtn.Disable()
			stopBtn.Enable()
			verboseConnCheck.Disable()
			tunCheck.Disable()
		} else {
			loginBtn.Enable()
			stopBtn.Disable()
			verboseConnCheck.Enable()
			tunCheck.Enable()
		}
	}

	isRunning := func() bool {
		runMu.Lock()
		defer runMu.Unlock()
		return running
	}

	stopClient := func() {
		runMu.Lock()
		c := runCancel
		ep := elevatedChild
		elevatedChild = nil
		restoreUserProxy := userProxyFallback
		restoreCurlrc := userCurlrcFallback
		restoreEnv := userEnvFallback
		userProxyFallback = false
		userCurlrcFallback = false
		userEnvFallback = false
		runMu.Unlock()
		if ep != nil {
			if err := stopElevatedClient(ep); err != nil {
				statusLabel.SetText("停止 TUN helper 失败：" + err.Error())
			}
		} else if c != nil {
			c()
		}
		if restoreCurlrc {
			if err := curlrc.Restore(); err != nil {
				logWriter.Write([]byte("[curl] 还原 ~/.curlrc 失败: " + err.Error() + "\n"))
			}
		}
		if restoreEnv {
			if err := proxyenv.Restore(); err != nil {
				logWriter.Write([]byte("[代理环境] 还原失败: " + err.Error() + "\n"))
			}
		}
		if restoreUserProxy {
			if err := sysproxy.Restore(); err != nil {
				logWriter.Write([]byte("[系统代理] 还原失败: " + err.Error() + "\n"))
			}
		}
		runWG.Wait()
		runMu.Lock()
		runCancel = nil
		runMu.Unlock()
		setRunning(false)
		logWriter.Flush()
	}

	loginBtn.OnTapped = func() {
		if isRunning() {
			return
		}
		host := strings.TrimSpace(hostEntry.Text)
		port := strings.TrimSpace(portEntry.Text)
		pass := passEntry.Text
		if host == "" || port == "" {
			dialog.ShowInformation("提示", "请填写服务器地址与端口。", w)
			return
		}
		if pass == "" {
			dialog.ShowInformation("提示", "请填写密码。", w)
			return
		}
		serverAddr := net.JoinHostPort(host, port)
		if _, _, err := net.SplitHostPort(serverAddr); err != nil {
			dialog.ShowError(fmt.Errorf("地址格式无效: %w", err), w)
			return
		}

		statusLabel.SetText("正在校验登录…")
		loginBtn.Disable()

		go func() {
			err := clientrunner.VerifyRemoteLogin(serverAddr, pass, tlsInsecure, "")
			if err != nil {
				statusLabel.SetText("登录失败：" + err.Error())
				dialog.ShowError(fmt.Errorf("登录失败: %w", err), w)
				loginBtn.Enable()
				return
			}

			if saveErr := uistore.Save(&uistore.Settings{
				Host:          host,
				Port:          port,
				Password:      pass,
				UpstreamProxy: strings.TrimSpace(upstreamEntry.Text),
			}); saveErr != nil {
				statusLabel.SetText("登录成功，但保存连接信息失败：" + saveErr.Error())
			} else {
				statusLabel.SetText("登录成功，正在启动本地代理…")
			}

			logWriter.Clear()

			ctx, cancel := context.WithCancel(context.Background())
			runMu.Lock()
			runCancel = cancel
			runMu.Unlock()
			setRunning(true)

			cfg := clientrunner.Config{
				ServerAddr:     serverAddr,
				Password:       pass,
				LocalListen:    "127.0.0.1:7890",
				TLSInsecure:    tlsInsecure,
				SNI:            "",
				SkipLoginCheck: true,
				AutoProxy:      true,
				AutoEnv:        true,
				AutoCurlrc:     true,
				NetworkService: "",
				SOCKS:          true,
				SOCKSListen:    "127.0.0.1:7891",
				TUN:            tunCheck.Checked,
				UpstreamProxy:  strings.TrimSpace(upstreamEntry.Text),
				LogWriter:      logWriter,
				// 未勾选「显示每条连接…」时省略逐连接流水，保留启动/错误等日志；防抖仍限制 UI 更新频率。
				SuppressPerConnLogs: !verboseConnCheck.Checked,
			}

			if tunCheck.Checked {
				ep, err := startElevatedClient(cfg)
				if err != nil {
					statusLabel.SetText("TUN 授权启动失败：" + err.Error())
					dialog.ShowError(fmt.Errorf("TUN 授权启动失败: %w", err), w)
					cancel()
					runMu.Lock()
					runCancel = nil
					runMu.Unlock()
					setRunning(false)
					return
				}
				if err := waitElevatedHealthy(ep); err != nil {
					_ = stopElevatedClient(ep)
					statusLabel.SetText("TUN helper 未就绪：" + err.Error())
					dialog.ShowError(fmt.Errorf("TUN helper 未就绪: %w", err), w)
					cancel()
					runMu.Lock()
					runCancel = nil
					runMu.Unlock()
					setRunning(false)
					return
				}
				runMu.Lock()
				elevatedChild = ep
				runMu.Unlock()
				if err := sysproxy.Apply("127.0.0.1", "7890", "127.0.0.1", "7891", ""); err != nil {
					logWriter.Write([]byte("[系统代理] TUN fallback 设置失败: " + err.Error() + "\n"))
				} else {
					runMu.Lock()
					userProxyFallback = true
					runMu.Unlock()
					logWriter.Write([]byte("[系统代理] 已为当前用户启用 HTTP/HTTPS/SOCKS -> 127.0.0.1:7890/7891（浏览器 fallback）\n"))
				}
				if err := proxyenv.Apply(proxyenv.Config{
					HTTPProxyURL:  "http://127.0.0.1:7890",
					SOCKSProxyURL: "socks5://127.0.0.1:7891",
					EnableSOCKS:   true,
				}); err != nil {
					logWriter.Write([]byte("[代理环境] TUN fallback 设置失败: " + err.Error() + "\n"))
				} else {
					runMu.Lock()
					userEnvFallback = true
					runMu.Unlock()
					logWriter.Write([]byte("[代理环境] 已为新进程写入 127.0.0.1 代理变量\n"))
				}
				if err := curlrc.Apply("http://127.0.0.1:7890"); err != nil {
					logWriter.Write([]byte("[curl] TUN fallback 写入 ~/.curlrc 失败: " + err.Error() + "\n"))
				} else {
					runMu.Lock()
					userCurlrcFallback = true
					runMu.Unlock()
					logWriter.Write([]byte("[curl] 已写入 ~/.curlrc fallback，普通 curl 也会走 127.0.0.1:7890\n"))
				}
				statusLabel.SetText(fmt.Sprintf("TUN helper 已通过系统授权启动（PID %d）。停止时会自动关闭。", ep.PID))
				logWriter.Write([]byte(fmt.Sprintf("[TUN] 已启动提权 helper PID=%d\n[TUN] helper 日志: %s\n", ep.PID, ep.LogFile)))
				return
			}

			runWG.Add(1)
			go func() {
				defer runWG.Done()
				if err := clientrunner.Run(ctx, cfg); err != nil {
					statusLabel.SetText("运行错误：" + err.Error())
					dialog.ShowError(fmt.Errorf("代理启动失败: %w", err), w)
				} else {
					statusLabel.SetText("已停止。")
				}
				setRunning(false)
			}()
		}()
	}

	stopBtn.OnTapped = func() {
		if !isRunning() {
			return
		}
		statusLabel.SetText("正在停止…")
		stopClient()
	}

	w.SetCloseIntercept(func() {
		if isRunning() {
			stopClient()
		}
		time.Sleep(50 * time.Millisecond)
		w.Close()
	})

	// 服务器行排版说明（避免再出现「整条只有手指宽」）：
	// - fyne 的 HBox 会把普通子控件宽度锁在 MinSize，不能把 HSplit 直接塞进 HBox，否则整段被压到极小。
	// - 用 Border：中间区域吃掉剩余宽度；端口条固定在右侧一列，地址框在中间拉伸。
	portStrip := container.NewHBox(widget.NewLabel("端口"), portCell)
	addrBlock := container.NewBorder(nil, nil, nil, portStrip, hostEntry)

	leftChunk := container.NewBorder(nil, nil, widget.NewLabel("服务器"), nil, addrBlock)

	rightChunk := container.NewBorder(nil, nil, widget.NewLabel("密码"), nil, passEntry)

	// 外层 HSplit：左右两块内部都已能拉伸，这里只分配大致比例（仍带可拖分隔条）。
	serverRow := container.NewHSplit(leftChunk, rightChunk)
	serverRow.SetOffset(0.46) // 左侧约 46%，密码侧略宽；可改为 0.42–0.52

	btnRow := container.NewHBox(loginBtn, layout.NewSpacer(), stopBtn)

	upstreamRow := container.NewBorder(nil, nil, widget.NewLabel("上级代理"), nil, upstreamEntry)
	optionRow := container.NewHBox(tunCheck, layout.NewSpacer(), verboseConnCheck)
	logHeaderRow := container.NewHBox(widget.NewLabel("◆ 运行日志"), layout.NewSpacer())
	header := container.NewVBox(
		serverRow,
		optionRow,
		upstreamRow,
		btnRow,
		statusLabel,
		widget.NewSeparator(),
		logHeaderRow,
	)
	// 纵向渐变衬底 + 内边距，空白区也有「深空 HUD」质感；日志区占满剩余高度。
	winBg := canvas.NewVerticalGradient(
		color.NRGBA{R: 0x0e, G: 0x1a, B: 0x2e, A: 0xff},
		color.NRGBA{R: 0x04, G: 0x08, B: 0x12, A: 0xff},
	)
	inner := container.NewBorder(header, nil, nil, nil, logPanel)
	w.SetContent(container.NewStack(winBg, container.NewPadded(inner)))
	w.ShowAndRun()
}
