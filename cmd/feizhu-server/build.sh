#!/usr/bin/env bash
# 在仓库根目录执行 go build，输出到本目录 dist/：
#   - macOS Intel:   feizhu-server-darwin-amd64
#   - macOS Apple:   feizhu-server-darwin-arm64
#   - Linux x86_64:  feizhu-server-linux-amd64
#   - Linux ARM64:   feizhu-server-linux-arm64
#
# 用法示例：
#   ./dist/feizhu-server-linux-amd64 -listen :8443 -password 'xxx'

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
OUT="$DIR/dist"
mkdir -p "$OUT"

cd "$ROOT"
export CGO_ENABLED=0

echo "building feizhu-server -> $OUT"

GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-server-darwin-amd64" ./cmd/feizhu-server
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-server-darwin-arm64" ./cmd/feizhu-server
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-server-linux-amd64" ./cmd/feizhu-server
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$OUT/feizhu-server-linux-arm64" ./cmd/feizhu-server

echo "done:"
ls -la "$OUT"
