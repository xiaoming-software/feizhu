#!/usr/bin/env bash
# 在仓库根目录编译 feizhu-client-ui，输出到本目录下的 dist/。
# - macOS：生成 FeizhuClientUI.app（双击从 Finder 启动，不附带终端窗口）+ Windows amd64 .exe。
# - Linux：仅生成 Windows .exe（需 MinGW）；.app 需在 macOS 上构建。
# - Windows（MSYS2/MinGW）：生成无控制台窗口的 .exe（PE 子系统 windowsgui）。
#
# Windows 交叉编译（在 Mac 上）：brew install mingw-w64
# 若编译器不在 PATH：export MINGW_CC=/path/to/x86_64-w64-mingw32-gcc

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DIST="$SCRIPT_DIR/dist"
mkdir -p "$DIST"

cd "$ROOT"

pick_mingw_cc() {
	local c
	for c in x86_64-w64-mingw32-gcc x86_64-w64-mingw32-gcc-posix; do
		if command -v "$c" >/dev/null 2>&1; then
			command -v "$c"
			return 0
		fi
	done
	for c in /opt/homebrew/opt/mingw-w64/bin/x86_64-w64-mingw32-gcc \
		/usr/local/opt/mingw-w64/bin/x86_64-w64-mingw32-gcc; do
		if [[ -x "$c" ]]; then
			echo "$c"
			return 0
		fi
	done
	return 1
}

# 注意：不得使用与外层 build_windows_cross 中相同的 local 变量名（如 cc），
# 否则在 set -u 与命令替换 $(...) 下，部分 bash 会出现外层 cc「未绑定」误报。
mingw_cxx() {
	local gcc_bin="$1"
	case "$gcc_bin" in
	*-gcc-posix) echo "${gcc_bin%-gcc-posix}-g++-posix" ;;
	*-gcc) echo "${gcc_bin%-gcc}-g++" ;;
	*)
		echo "错误: 无法从 MinGW gcc 推导 g++：$gcc_bin" >&2
		return 1
		;;
	esac
}

GOHOST="$(uname -s 2>/dev/null || echo unknown)"
GOHOSTARCH_RAW="$(uname -m 2>/dev/null || echo unknown)"
case "$GOHOSTARCH_RAW" in
arm64 | aarch64) GOHOSTARCH=arm64 ;;
x86_64 | amd64) GOHOSTARCH=amd64 ;;
*) GOHOSTARCH="$GOHOSTARCH_RAW" ;;
esac

write_macos_info_plist() {
	local plist="$1"
	local bundle_id="$2"
	local version="$3"
	local tmp
	tmp="$(mktemp)"
	# 必须使用 <<'…'：未加引号的 heredoc 会展开 $，例如 public.app-category… 中的 $a 会触发 set -u。
	cat >"$tmp" <<'TEMPLATE'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>zh_CN</string>
	<key>CFBundleExecutable</key>
	<string>feizhu-client-ui</string>
	<key>CFBundleIdentifier</key>
	<string>@BUNDLE_ID@</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>FeizhuClientUI</string>
	<key>CFBundleDisplayName</key>
	<string>飞猪客户端</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>@VERSION@</string>
	<key>CFBundleVersion</key>
	<string>@VERSION@</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.utilities</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
</dict>
</plist>
TEMPLATE
	local bid_esc ver_esc
	bid_esc=$(printf '%s' "$bundle_id" | sed 's/[\/&]/\\&/g')
	ver_esc=$(printf '%s' "$version" | sed 's/[\/&]/\\&/g')
	sed -e "s#@BUNDLE_ID@#${bid_esc}#g" -e "s#@VERSION@#${ver_esc}#g" "$tmp" >"$plist"
	rm -f "$tmp"
}

ensure_app_icons() {
	local assets="$SCRIPT_DIR/assets"
	local icns="$assets/AppIcon.icns"
	local png="$assets/icon.png"
	local src="$assets/icon-src.png"
	if [[ ! -f "$src" ]] && [[ ! -f "$png" ]]; then
		echo "错误: 缺少 $src（或至少 $png）" >&2
		return 1
	fi
	local need=0
	[[ ! -f "$png" ]] && need=1
	[[ -f "$src" && "$src" -nt "$png" ]] && need=1
	[[ ! -f "$icns" || ( -f "$png" && "$png" -nt "$icns" ) ]] && need=1
	local syso="$SCRIPT_DIR/rsrc_windows_amd64.syso"
	[[ ! -f "$syso" || ( -f "$png" && "$png" -nt "$syso" ) ]] && need=1
	if [[ "$need" -eq 1 ]]; then
		if [[ -f "$src" ]]; then
			bash "$assets/gen_icons.sh"
		else
			if [[ "$(uname -s)" == "Darwin" ]] && [[ ! -f "$icns" || ( -f "$png" && "$png" -nt "$icns" ) ]]; then
				echo "警告: 缺少 $src，无法生成圆角 icon；请添加源图后运行 assets/gen_icons.sh" >&2
			fi
			if [[ -f "$png" ]] && [[ ! -f "$syso" || "$png" -nt "$syso" ]]; then
				(cd "$SCRIPT_DIR" && go run github.com/tc-hib/go-winres@v0.3.2 simply --icon assets/icon.png --arch amd64) || {
					echo "警告: 未能生成 rsrc_windows_amd64.syso" >&2
				}
			fi
		fi
	fi
}

# 删除 dist 里旧的外置运行库（WinDivert/wintun 已嵌入 exe）。
clean_obsolete_windows_dist() {
	rm -f "$DIST/WinDivert.dll" "$DIST/WinDivert64.sys" "$DIST/wintun.dll"
}

# 下载 WinDivert 并写入 internal/tunmode/embed/，供 go:embed 打进 Windows exe。
ensure_windivert_embed() {
	local embed_dir="$ROOT/internal/tunmode/embed"
	local embed_dll="$embed_dir/WinDivert.dll"
	local embed_sys="$embed_dir/WinDivert64.sys"
	local vendor_dir="$SCRIPT_DIR/vendor/windivert"
	local cache_dll="$vendor_dir/WinDivert.dll"
	local cache_sys="$vendor_dir/WinDivert64.sys"

	if [[ -f "$embed_dll" && -f "$embed_sys" ]]; then
		echo "    使用已存在的 embed/WinDivert.{dll,sys}"
		return 0
	fi

	if [[ ! -f "$cache_dll" || ! -f "$cache_sys" ]]; then
		if ! command -v curl >/dev/null 2>&1 || ! command -v unzip >/dev/null 2>&1; then
			echo "错误: 缺少 curl/unzip，无法下载 WinDivert（Windows 构建必需）。" >&2
			return 1
		fi
		local tmp
		tmp="$(mktemp -d)"
		echo "==> 下载 WinDivert 2.2.2（嵌入 Windows exe）…"
		if ! curl -fsSL "https://reqrypt.org/download/WinDivert-2.2.2-A.zip" -o "$tmp/windivert.zip"; then
			rm -rf "$tmp"
			echo "错误: 下载 WinDivert 失败" >&2
			return 1
		fi
		if ! unzip -q "$tmp/windivert.zip" -d "$tmp"; then
			rm -rf "$tmp"
			echo "错误: 解压 WinDivert 失败" >&2
			return 1
		fi
		local dll_src="$tmp/WinDivert-2.2.2-A/x64/WinDivert.dll"
		local sys_src="$tmp/WinDivert-2.2.2-A/x64/WinDivert64.sys"
		if [[ ! -f "$dll_src" || ! -f "$sys_src" ]]; then
			rm -rf "$tmp"
			echo "错误: WinDivert zip 中缺少 x64/WinDivert.dll 或 WinDivert64.sys" >&2
			return 1
		fi
		mkdir -p "$vendor_dir"
		cp "$dll_src" "$cache_dll"
		cp "$sys_src" "$cache_sys"
		rm -rf "$tmp"
	fi

	mkdir -p "$embed_dir"
	cp "$cache_dll" "$embed_dll"
	cp "$cache_sys" "$embed_sys"
	echo "    已写入 embed/WinDivert.dll + WinDivert64.sys（将打入 exe）"
}

build_macos() {
	ensure_app_icons
	local bundle_dir="$DIST/FeizhuClientUI.app"
	local Contents="$bundle_dir/Contents"
	local MacOS="$Contents/MacOS"
	local Resources="$Contents/Resources"
	local bin_name="feizhu-client-ui"
	local bundle_id="${FEIZHU_BUNDLE_ID:-io.feizhu.clientui}"
	local version="${FEIZHU_VERSION:-1.0.0}"

	echo "==> 编译 macOS 图形应用 ($GOHOSTARCH) → FeizhuClientUI.app …"
	rm -rf "$bundle_dir"
	mkdir -p "$MacOS" "$Resources"
	if [[ -f "$SCRIPT_DIR/assets/AppIcon.icns" ]]; then
		cp "$SCRIPT_DIR/assets/AppIcon.icns" "$Resources/AppIcon.icns"
	fi

	CGO_ENABLED=1 GOOS=darwin GOARCH="$GOHOSTARCH" \
		go build -trimpath -ldflags="-s -w" -o "$MacOS/$bin_name" ./cmd/feizhu-client-ui
	chmod +x "$MacOS/$bin_name"

	write_macos_info_plist "$Contents/Info.plist" "$bundle_id" "$version"
	# 旧式类型标记（可选，部分工具会读）
	printf 'APPL????' >"$Contents/PkgInfo"

	if command -v plutil >/dev/null 2>&1; then
		plutil -lint "$Contents/Info.plist" >/dev/null
	fi
	# 临时签名，便于本机/拷贝后首次打开（分发正式版请用开发者证书 codesign + notarize）
	if command -v codesign >/dev/null 2>&1; then
		codesign --force --deep --sign - "$bundle_dir" 2>/dev/null || true
	fi

	printf '    输出: %s（可拖入「应用程序」或双击运行，无终端窗口）\n' "$bundle_dir"
}

build_windows_cross() {
	clean_obsolete_windows_dist
	ensure_app_icons
	ensure_windivert_embed
	local cc="${MINGW_CC:-}"
	if [[ -z "$cc" ]]; then
		cc="$(pick_mingw_cc)" || {
			echo "错误: 未找到 MinGW 的 x86_64-w64-mingw32-gcc，无法交叉编译 Windows 版。" >&2
			echo "      在 macOS 上可执行: brew install mingw-w64" >&2
			echo "      或设置环境变量 MINGW_CC 指向该 gcc。" >&2
			return 1
		}
	fi
	local cxx
	cxx="$(mingw_cxx "$cc")" || return 1
	printf '==> 编译 Windows amd64（CC=%s）…\n' "${cc}"
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="$cc" CXX="$cxx" \
		go build -trimpath -ldflags="-s -w -H windowsgui" -o "$DIST/feizhu-client-ui-windows-amd64.exe" ./cmd/feizhu-client-ui
	echo "    输出: $DIST/feizhu-client-ui-windows-amd64.exe"
}

build_windows_native() {
	clean_obsolete_windows_dist
	ensure_app_icons
	ensure_windivert_embed
	echo "==> 编译 Windows amd64（本机）…"
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
		go build -trimpath -ldflags="-s -w -H windowsgui" -o "$DIST/feizhu-client-ui-windows-amd64.exe" ./cmd/feizhu-client-ui
	echo "    输出: $DIST/feizhu-client-ui-windows-amd64.exe"
}

case "$GOHOST" in
Darwin)
	build_macos
	build_windows_cross
	;;
Linux)
	echo "提示: 当前为 Linux，仅生成 Windows 版；macOS 可执行文件请在 macOS 上运行本脚本生成。" >&2
	build_windows_cross
	;;
MINGW* | MSYS* | CYGWIN*)
	echo "提示: 当前为 Windows/MinGW 环境，仅生成 Windows 版；macOS 请在 Mac 上运行本脚本。" >&2
	build_windows_native
	;;
*)
	if [[ "${OS:-}" == "Windows_NT" ]]; then
		echo "提示: 检测到 Windows，仅生成 Windows 版。" >&2
		build_windows_native
	else
		echo "提示: 未识别主机系统 \"$GOHOST\"，尝试仅交叉编译 Windows（需 MinGW）…" >&2
		build_windows_cross
	fi
	;;
esac

echo "完成。产物目录: $DIST"
