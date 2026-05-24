# 平台代码隔离

macOS 与 Windows 的 TUN/拨号逻辑已拆到独立目录，**两目录之间不共享实现文件**：

| 目录 | 用途 |
|------|------|
| `mac/tunmode/` | macOS pf + tun2socks |
| `mac/dial.go` | macOS TUN/本地 SOCKS 拨号 |
| `windows/tunmode/` | Windows wintun + tun2socks（冻结，仅调试 Windows 时改） |
| `windows/dial.go` | Windows 拨号（冻结） |

`internal/tunmode/` 仅保留 `Config` 与按平台的薄转发（`export_darwin.go` / `export_windows.go`）。

调试 mac 时只改 `mac/`；调试 Windows 时只改 `windows/`。
