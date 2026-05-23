# WinDivert 运行文件（构建时生成）

Windows 版 `build.sh` 会从官方包下载并写入：

- `WinDivert.dll`
- `WinDivert64.sys`

编译时 `go:embed` 打进 exe，首次启用 TUN 时释放到 exe 同目录。

勿手动提交上述二进制（已在 `.gitignore` 忽略）。
