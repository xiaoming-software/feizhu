#!/usr/bin/env bash
# 在仓库根目录执行 go build，输出到本目录 dist/：
#   - macOS Intel:   feizhu-client-darwin-amd64
#   - macOS Apple:   feizhu-client-darwin-arm64
#   - Windows 64 位: feizhu-client-windows-amd64.exe
#
# 用法示例：
#   ./dist/feizhu-client-darwin-arm64 -server 1.2.3.4:8443 -password 'xxx' -listen 127.0.0.1:7890

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
OUT="$DIR/dist"
mkdir -p "$OUT"

cd "$ROOT"
export CGO_ENABLED=0

echo "building feizhu-client -> $OUT"

GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-client-darwin-amd64" ./cmd/feizhu-client
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-client-darwin-arm64" ./cmd/feizhu-client
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-client-windows-amd64.exe" ./cmd/feizhu-client

echo "done:"
ls -la "$OUT"
