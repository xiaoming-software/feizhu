# 飞猪 Feizhu

飞猪（Feizhu）是一套轻量级的 **TLS 加密隧道代理** 方案：在远端部署服务端，在本地运行客户端后，即可通过本地 HTTP / SOCKS5 代理访问网络。控制帧与目标站点信息在 TLS 应用层内传输，链路上不以明文暴露密码与访问目标。

## 架构概览

```
┌─────────────────┐     TLS + FZ1 控制帧      ┌──────────────────┐     TCP      ┌─────────────┐
│  浏览器 / 应用   │ ──► 本地 HTTP/SOCKS 代理   │  feizhu-server   │ ──────────► │  目标站点    │
│  (系统代理)      │      feizhu-client        │  (远端监听)       │             │             │
└─────────────────┘                           └──────────────────┘             └─────────────┘
     127.0.0.1:7890 / :7891                         :8443 (默认)
```

| 组件 | 说明 |
|------|------|
| **feizhu-server** | 监听 TLS 端口，校验密码，按客户端请求拨号并双向转发 |
| **feizhu-client** | 命令行客户端：本地代理 + 可选自动配置系统/环境变量/curl |
| **feizhu-client-ui** | 图形界面客户端（Fyne），与 CLI 共用同一套运行逻辑 |

## 功能特性

- **TLS 隧道**：客户端先完成 TLS 握手，再在加密连接上使用 `FZ1` 帧协议认证与拨号
- **密码认证**：服务端与客户端使用相同密码；支持启动前远端登录探测
- **本地双代理**：默认 HTTP `127.0.0.1:7890`、SOCKS5 `127.0.0.1:7891`
- **一键接管系统流量**（退出时自动还原）：
  - macOS：`networksetup` 设置 HTTP/HTTPS/SOCKS
  - Windows：用户级注册表代理
  - Linux：系统代理需手动配置（见下方说明）；支持 `systemd --user` 写入 `http_proxy` 等
- **curl 友好**：默认向 `~/.curlrc` 写入代理段，新终端中 `curl` 即可走代理
- **自签名证书**：未指定证书时服务端自动生成；客户端默认 `-tls-insecure` 跳过校验

## 环境要求

- **Go 1.22+**（从源码构建）
- **服务端**：推荐 Linux / macOS（`feizhu-server` 无 GUI 依赖）
- **客户端 CLI**：macOS、Windows（`CGO_ENABLED=0` 静态编译）
- **客户端 GUI**：macOS 11+ 或 Windows（需 CGO；macOS 上生成 `.app`）

## 快速开始

### 1. 部署服务端

在具有公网 IP 或内网可达的机器上运行（**请将密码改为强密码**）：

```bash
# 使用仓库内构建脚本（产物在 cmd/feizhu-server/dist/）
./cmd/feizhu-server/build.sh

# 示例：监听 8443，密码 your-strong-password
./cmd/feizhu-server/dist/feizhu-server-linux-amd64 \
  -listen :8443 \
  -password 'your-strong-password'
```

使用自有证书（可选）：

```bash
./feizhu-server -listen :8443 -password 'xxx' -cert server.pem -key server.key
```

未指定 `-cert`/`-key` 时使用内置自签名证书，客户端需保持 `-tls-insecure`（GUI 已内置）。

### 2. 连接客户端（命令行）

```bash
./cmd/feizhu-client/build.sh

./cmd/feizhu-client/dist/feizhu-client-darwin-arm64 \
  -server 1.2.3.4:8443 \
  -password 'your-strong-password'
```

启动成功后，本机 HTTP 代理为 `127.0.0.1:7890`，SOCKS5 为 `127.0.0.1:7891`（默认）。按 `Ctrl+C` 退出时会还原系统代理与环境变量。

### 3. 图形界面客户端

```bash
./cmd/feizhu-client-ui/build.sh
```

- **macOS**：产物为 `cmd/feizhu-client-ui/dist/FeizhuClientUI.app`，可拖入「应用程序」文件夹
- **Windows**：`feizhu-client-ui-windows-amd64.exe`（无控制台窗口）

在界面中填写服务器地址、端口（默认 `8443`）与密码，点击「登录」。连接信息会保存到 `~/.feizhu/client-ui-settings.json`。

> 在 macOS 上交叉编译 Windows 版需安装 MinGW：`brew install mingw-w64`

## 从源码构建

在仓库根目录执行各组件下的 `build.sh`：

| 脚本 | 输出目录 | 目标平台 |
|------|----------|----------|
| `cmd/feizhu-server/build.sh` | `cmd/feizhu-server/dist/` | darwin amd64/arm64，linux amd64/arm64 |
| `cmd/feizhu-client/build.sh` | `cmd/feizhu-client/dist/` | darwin amd64/arm64，windows amd64 |
| `cmd/feizhu-client-ui/build.sh` | `cmd/feizhu-client-ui/dist/` | macOS `.app`、Windows exe |

也可直接使用 `go build`：

```bash
go build -o feizhu-server ./cmd/feizhu-server
go build -o feizhu-client ./cmd/feizhu-client
# GUI 需 CGO
CGO_ENABLED=1 go build -o feizhu-client-ui ./cmd/feizhu-client-ui
```

## 命令行参数

### feizhu-server

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-listen` | `:8443` | 监听地址 |
| `-password` | （必填） | 与客户端一致的认证密码 |
| `-cert` / `-key` | 空 | PEM 证书与私钥；均未指定时使用自签名 |

### feizhu-client

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-server` | （必填） | 服务端 `host:port` |
| `-password` | （必填） | 认证密码 |
| `-listen` | `127.0.0.1:7890` | 本地 HTTP 代理地址 |
| `-socks-listen` | `127.0.0.1:7891` | 本地 SOCKS5 地址 |
| `-socks` | `true` | 是否启用 SOCKS5 |
| `-tls-insecure` | `true` | 跳过 TLS 证书校验（自签名场景） |
| `-sni` | 同 server 主机名 | TLS SNI |
| `-auto-proxy` | `true` | 自动设置系统代理，退出时还原 |
| `-auto-env` | `true` | 写入用户级 `http_proxy` 等 |
| `-auto-curlrc` | `true` | 维护 `~/.curlrc` 代理段 |
| `-network-service` | 空 | 仅 macOS：`networksetup` 网络服务名（如 `Wi-Fi`） |
| `-skip-login-check` | `false` | 跳过启动时远端密码校验（不推荐） |
| `-print-proxy-env` | `false` | 仅打印 shell `export` 脚本后退出 |

仅打印终端代理环境变量（无需连接服务端）：

```bash
feizhu-client -print-proxy-env
eval "$(feizhu-client -print-proxy-env)"
```

## 平台说明

| 能力 | macOS | Windows | Linux |
|------|-------|---------|-------|
| 本地 HTTP/SOCKS 代理 | ✅ | ✅ | ✅ |
| 自动系统代理 (`-auto-proxy`) | ✅ | ✅ | ❌ 请手动指向 `127.0.0.1:7890` 或 `-auto-proxy=false` |
| 用户级环境变量 (`-auto-env`) | ✅ launchctl | ✅ 注册表 | ✅ systemd --user |
| `~/.curlrc` (`-auto-curlrc`) | ✅ | ✅ | ✅ |

**macOS 提示**：若在 feizhu-client 启动前已打开「终端」，新建窗口可能读不到 `http_proxy`；请 **Cmd+Q 完全退出终端再打开**，或依赖默认开启的 `-auto-curlrc`。

**Linux 提示**：`-auto-proxy` 在 Linux 上会报错；可关闭后手动配置桌面环境或浏览器代理，并保留 `-auto-env` / `-auto-curlrc`。

## 项目结构

```
feizhu/
├── cmd/
│   ├── feizhu-server/      # 服务端入口
│   ├── feizhu-client/      # CLI 客户端
│   ├── feizhu-client-ui/   # GUI 客户端（Fyne）
│   └── feizhu-icongen/     # 图标生成工具
└── internal/
    ├── tunnel/             # FZ1 帧协议与握手
    ├── clientrunner/       # 客户端运行逻辑（CLI/GUI 共用）
    ├── socks5/             # 本地 SOCKS5
    ├── sysproxy/           # 系统代理（macOS / Windows）
    ├── proxyenv/           # 用户级代理环境变量
    ├── curlrc/             # ~/.curlrc 管理
    ├── tlscert/            # 自签名证书
    └── uistore/            # GUI 连接信息持久化
```

## 安全说明

- 密码与目标 `host:port` 在 **TLS 应用数据** 中传输，不会以明文出现在 TCP 载荷中。
- TLS 握手中的 **SNI** 仍可能暴露代理服务器主机名；访问的网站域名在加密层内。
- 默认自签名证书需客户端显式信任或跳过校验；生产环境建议使用正规证书并关闭 `-tls-insecure`。
- GUI 将密码明文保存在 `~/.feizhu/client-ui-settings.json`，请在可信环境中使用。
- 请使用强密码，并妥善保管服务端与客户端配置。

## 开发与测试

```bash
go test ./...
```

## 许可证

请参阅仓库中的许可证文件（若尚未添加，以项目维护者约定为准）。
