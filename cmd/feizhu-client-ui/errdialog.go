package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// showErrorDialog 显示可多选、可一键复制的错误弹窗（替代 dialog.ShowError）。
func showErrorDialog(win fyne.Window, title string, err error) {
	if err == nil {
		return
	}
	showErrorDialogText(win, title, err.Error())
}

func showErrorDialogText(win fyne.Window, title, detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "（无错误详情）"
	}
	msg := detail

	entry := widget.NewMultiLineEntry()
	entry.SetText(msg)
	entry.Wrapping = fyne.TextWrapWord
	rows := strings.Count(msg, "\n") + 3
	if rows < 6 {
		rows = 6
	}
	if rows > 16 {
		rows = 16
	}
	entry.SetMinRowsVisible(rows)

	hint := widget.NewLabel("可选中文本后 Ctrl+C / Cmd+C，或点击下方按钮复制。")
	hint.Wrapping = fyne.TextWrapWord

	status := widget.NewLabel("")
	copyBtn := widget.NewButton("复制错误内容", func() {
		win.Clipboard().SetContent(msg)
		status.SetText("已复制到剪贴板")
	})

	footer := container.NewVBox(
		container.NewHBox(copyBtn, status, layout.NewSpacer()),
		hint,
	)
	body := container.NewBorder(
		nil,
		footer,
		nil,
		nil,
		container.NewScroll(entry),
	)

	d := dialog.NewCustom(title, "关闭", body, win)
	d.Resize(fyne.NewSize(640, minDialogHeight(rows)))
	d.Show()
}

func minDialogHeight(rows int) float32 {
	h := float32(120 + rows*22)
	if h < 280 {
		return 280
	}
	if h > 520 {
		return 520
	}
	return h
}
