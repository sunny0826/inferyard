package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"github.com/oneo-me/onevra/source/theme"
)

func pageShellOptions(title, stateKey string, left, content func(*ui.Context)) sidebar.ShellOptions {
	return sidebar.ShellOptions{
		Title:           title,
		Width:           1120,
		Height:          800,
		MinWidth:        800,
		MinHeight:       600,
		StateKey:        stateKey,
		LeftWidth:       205,
		Left:            left,
		Content:         content,
		Theme:           theme.Onevra,
		LeftToggleLabel: "切换侧栏",
		LeftResizeLabel: "调整侧栏宽度",
		MinimizeLabel:   "最小化",
		MaximizeLabel:   "最大化",
		RestoreLabel:    "还原",
		CloseLabel:      "关闭",
		CloseToastLabel: "关闭通知",
	}
}
