# WinDivert / wintun 运行文件（构建时由 build.ps1 下载写入）

- `WinDivert.dll` / `WinDivert64.sys` — WinDivert 降级路径
- `wintun.dll` — **与 macOS 相同的 tun2socks 路径（Windows 主路径，必需）**

编译时 `go:embed` 打进 exe，首次启用 TUN 时释放到 exe 同目录。

勿手动提交上述二进制（已在 `.gitignore` 忽略）。
