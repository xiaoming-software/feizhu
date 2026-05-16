package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// sciFiTheme 全局「深空 / 全息终端」风格：背景、输入框、按钮、分隔线与滚动条统一调色。
type sciFiTheme struct {
	base fyne.Theme
}

func newSciFiTheme() fyne.Theme {
	return &sciFiTheme{base: theme.DarkTheme()}
}

func (t *sciFiTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x0c, G: 0x14, B: 0x24, A: 0xff}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xdc, G: 0xf4, B: 0xff, A: 0xff}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x00, G: 0xd4, B: 0xc4, A: 0xff}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0x04, G: 0x0c, B: 0x14, A: 0xff}
	case theme.ColorNameButton:
		return color.NRGBA{R: 0x14, G: 0x24, B: 0x38, A: 0xff}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0x12, G: 0x1a, B: 0x28, A: 0xff}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x08, G: 0x12, B: 0x1e, A: 0xff}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 0x2a, G: 0x7a, B: 0x8a, A: 0xcc}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0x38, G: 0xff, B: 0xe8, A: 0xff}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x2e, G: 0xff, B: 0xf0, A: 0x24}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x38}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0x58, G: 0x78, B: 0x82, A: 0xff}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x45, G: 0x7a, B: 0x88, A: 0xff}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0x1a, G: 0x5c, B: 0x5a, A: 0xaa}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x22, G: 0xb8, B: 0xaa, A: 0xb0}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x00, G: 0x6a, B: 0x62, A: 0x88}
	case theme.ColorNameHyperlink:
		return color.NRGBA{R: 0x5e, G: 0xe0, B: 0xff, A: 0xff}
	case theme.ColorNameError:
		return color.NRGBA{R: 0xff, G: 0x66, B: 0x88, A: 0xff}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x00, G: 0xd4, B: 0xc4, A: 0xff}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 0xff, G: 0xb8, B: 0x20, A: 0xff}
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x66}
	case theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 0x08, G: 0x10, B: 0x1c, A: 0xee}
	case theme.ColorNameMenuBackground:
		return color.NRGBA{R: 0x10, G: 0x1c, B: 0x2c, A: 0xff}
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 0x0e, G: 0x18, B: 0x26, A: 0xff}
	case theme.ColorNameForegroundOnError, theme.ColorNameForegroundOnSuccess, theme.ColorNameForegroundOnWarning:
		return color.NRGBA{R: 0x06, G: 0x0c, B: 0x12, A: 0xff}
	default:
		return t.base.Color(n, v)
	}
}

func (t *sciFiTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }
func (t *sciFiTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }
func (t *sciFiTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNamePadding:
		return t.base.Size(n) + 2
	case theme.SizeNameInputRadius:
		return 6
	case theme.SizeNameSelectionRadius:
		return 4
	case theme.SizeNameScrollBarRadius:
		return 8
	case theme.SizeNameSeparatorThickness:
		return 2
	case theme.SizeNameInnerPadding:
		return t.base.Size(n) + 1
	default:
		return t.base.Size(n)
	}
}

// logConsoleTheme 仅用于日志滚动区：略偏「终端荧光绿」前景，与主界面冰蓝字区分层次。
type logConsoleTheme struct {
	base fyne.Theme
}

func newLogConsoleTheme() fyne.Theme {
	return &logConsoleTheme{base: newSciFiTheme()}
}

func (t *logConsoleTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xa8, G: 0xff, B: 0xe8, A: 0xff}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x06, G: 0x0f, B: 0x18, A: 0xff}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0x5e, G: 0xb8, B: 0xa8, A: 0xff}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x4a, G: 0x8a, B: 0x7e, A: 0xff}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0x22, G: 0x55, B: 0x4a, A: 0x88}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x2e, G: 0xc4, B: 0xb4, A: 0xaa}
	default:
		return t.base.Color(n, v)
	}
}

func (t *logConsoleTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }
func (t *logConsoleTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }
func (t *logConsoleTheme) Size(n fyne.ThemeSizeName) float32 { return t.base.Size(n) }
