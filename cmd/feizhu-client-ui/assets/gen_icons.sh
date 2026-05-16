#!/usr/bin/env bash
# 从 assets/icon-src.png 生成 assets/icon.png（透明底、圆角、主体缩放），再生成 AppIcon.icns 与 rsrc_windows_amd64.syso。
# 在仓库根目录执行：bash cmd/feizhu-client-ui/assets/gen_icons.sh
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
UI_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ICON="$SCRIPT_DIR/icon.png"
SRC="$SCRIPT_DIR/icon-src.png"
ICONSET="$SCRIPT_DIR/icon.iconset"
ICNS="$SCRIPT_DIR/AppIcon.icns"

if [[ ! -f "$SRC" ]]; then
	echo "错误: 缺少源图 $SRC（可编辑后重新生成 icon.png）" >&2
	exit 1
fi

echo "==> 生成 icon.png（圆角透明 + 主体缩放）…"
(
	cd "$ROOT"
	go run ./cmd/feizhu-icongen/ -src "cmd/feizhu-client-ui/assets/icon-src.png" -out "cmd/feizhu-client-ui/assets/icon.png"
)

if [[ "$(uname -s)" == "Darwin" ]] && command -v iconutil >/dev/null 2>&1; then
	echo "==> 生成 AppIcon.icns …"
	rm -rf "$ICONSET" "$ICNS"
	mkdir -p "$ICONSET"
	sips -z 16 16 "$ICON" --out "$ICONSET/icon_16x16.png" >/dev/null
	sips -z 32 32 "$ICON" --out "$ICONSET/icon_16x16@2x.png" >/dev/null
	sips -z 32 32 "$ICON" --out "$ICONSET/icon_32x32.png" >/dev/null
	sips -z 64 64 "$ICON" --out "$ICONSET/icon_32x32@2x.png" >/dev/null
	sips -z 128 128 "$ICON" --out "$ICONSET/icon_128x128.png" >/dev/null
	sips -z 256 256 "$ICON" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
	sips -z 256 256 "$ICON" --out "$ICONSET/icon_256x256.png" >/dev/null
	sips -z 512 512 "$ICON" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
	sips -z 512 512 "$ICON" --out "$ICONSET/icon_512x512.png" >/dev/null
	sips -z 1024 1024 "$ICON" --out "$ICONSET/icon_512x512@2x.png" >/dev/null
	# iconutil 需要可被识别的 PNG（经 tiff 往返以兼容部分 RGB 源图）
	for f in "$ICONSET"/*.png; do
		t="${f%.png}.tif"
		sips -s format tiff "$f" --out "$t" >/dev/null
		sips -s format png "$t" --out "$f" >/dev/null
		rm -f "$t"
	done
	iconutil -c icns "$ICONSET" -o "$ICNS"
	rm -rf "$ICONSET"
	echo "    $ICNS"
else
	echo "跳过 AppIcon.icns（非 macOS 或未安装 iconutil）" >&2
fi

echo "==> 生成 Windows 图标资源 rsrc_windows_amd64.syso …"
(
	cd "$UI_DIR"
	go run github.com/tc-hib/go-winres@v0.3.2 simply --icon assets/icon.png --arch amd64
)
echo "    $UI_DIR/rsrc_windows_amd64.syso"
