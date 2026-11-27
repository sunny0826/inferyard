package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

func pageShellOptions(title, stateKey string, left, content func(*ui.Context)) sidebar.ShellOptions {
	return sidebar.ShellOptions{
		Title:              title,
		Width:              1120,
		Height:             800,
		MinWidth:           800,
		MinHeight:          600,
		StateKey:           stateKey,
		LeftWidth:          232,
		LeftToggleTrailing: true,
		Left:               left,
		Content:            content,
		Theme:              studioTheme,
		Toasts:             components.ToastOptions{Placement: components.ToastTop},
	}
}
