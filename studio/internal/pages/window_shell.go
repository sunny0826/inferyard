package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"github.com/oneo-me/onevra/source/theme"
)

func pageShellOptions(title, stateKey string, left, content func(*ui.Context)) sidebar.ShellOptions {
	return sidebar.ShellOptions{
		Title:     title,
		Width:     1120,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		StateKey:  stateKey,
		LeftWidth: 205,
		Left:      left,
		Content:   content,
		Theme:     theme.Onevra,
		Toasts:    components.ToastOptions{Placement: components.ToastTop},
	}
}

// studioToasts builds the Studio toast viewport. The main window and the
// preview window get it from the shell, which places the toasts at the top
// center and clears the floating header for them. The in-memory test view
// renders the same content without the shell, so it builds the same viewport
// here; otherwise the tests would show the toasts where the window does not.
func studioToasts(c *ui.Context) {
	o := components.ToastOptions{Placement: components.ToastTop}
	if o.Placement.Top() {
		o.Inset += sidebar.HeaderHeight(c)
	}
	components.Toasts(c, o)
}
