package desktop

import (
	"errors"
	"sync"

	"github.com/egoist/mygo"
)

const trayID = "inferyard-studio"

// TrayWindow is the subset of mygo.Window used by TrayController.
type TrayWindow interface {
	Hide()
	Show()
	Restore()
	Focus()
	IsMinimized() bool
	IsVisible() bool
}

// TrayIcon is the subset of mygo.Tray used by TrayController.
type TrayIcon interface {
	Destroy()
}

// TrayConfig configures a TrayController bound to a MyGo window and app lifecycle.
type TrayConfig struct {
	Window         *mygo.Window
	IconPNG        []byte
	ToolTip        string
	InitialEnabled bool
	Logf           func(string, ...any)
}

type trayHooks struct {
	win            TrayWindow
	createTray     func(onShow, onQuit func()) (TrayIcon, error)
	focusApp       func()
	quitApp        func()
	logf           func(string, ...any)
	onClose        func(shouldHide func() bool) func()
	onClosed       func(func()) func()
	onBeforeQuit   func(func()) func()
	onQuitCanceled func(func()) func()
	onActivate     func(func(bool)) func()
}

// TrayController manages the Studio system tray icon and close-to-tray behavior.
type TrayController struct {
	hooks    trayHooks
	mu       sync.Mutex
	enabled  bool
	quitting bool
	closed   bool
	tray     TrayIcon
	offs     []func()
}

// NewTrayController binds tray and close-to-tray handling to a MyGo window.
func NewTrayController(cfg TrayConfig) *TrayController {
	toolTip := cfg.ToolTip
	if toolTip == "" {
		toolTip = "InferYard Studio"
	}
	hooks := trayHooks{
		win:      cfg.Window,
		focusApp: mygo.App.Focus,
		quitApp:  mygo.App.Quit,
		logf:     cfg.Logf,
		createTray: func(onShow, onQuit func()) (TrayIcon, error) {
			menu := mygo.NewMenu([]*mygo.MenuItem{
				{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { onShow() }},
				mygo.Separator(),
				{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { onQuit() }},
			})
			tray, err := mygo.NewTray(mygo.TrayOptions{
				ID:             trayID,
				Icon:           cfg.IconPNG,
				IconIsTemplate: false,
				ToolTip:        toolTip,
				Menu:           menu,
			})
			if err != nil {
				return nil, err
			}
			tray.OnClick(onShow)
			return tray, nil
		},
		onClose: func(shouldHide func() bool) func() {
			if cfg.Window == nil {
				return nil
			}
			return cfg.Window.OnClose(func(e *mygo.CloseEvent) {
				if shouldHide() {
					e.PreventDefault()
					cfg.Window.Hide()
				}
			})
		},
		onClosed: func(fn func()) func() {
			if cfg.Window == nil {
				return nil
			}
			return cfg.Window.OnClosed(fn)
		},
		onBeforeQuit: func(fn func()) func() {
			return mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { fn() })
		},
		onQuitCanceled: func(fn func()) func() {
			return mygo.App.OnQuitCanceled(fn)
		},
		onActivate: func(fn func(bool)) func() {
			return mygo.App.OnActivate(fn)
		},
	}
	return newTrayController(hooks, cfg.InitialEnabled)
}

func newTrayController(hooks trayHooks, initialEnabled bool) *TrayController {
	c := &TrayController{hooks: hooks}
	var offs []func()
	add := func(off func()) {
		if off != nil {
			offs = append(offs, off)
		}
	}
	if hooks.onBeforeQuit != nil {
		add(hooks.onBeforeQuit(c.markQuitting))
	}
	if hooks.onQuitCanceled != nil {
		add(hooks.onQuitCanceled(c.clearQuitting))
	}
	if hooks.onActivate != nil {
		add(hooks.onActivate(c.handleActivate))
	}
	if hooks.onClose != nil {
		add(hooks.onClose(c.shouldHideOnClose))
	}
	if hooks.onClosed != nil {
		add(hooks.onClosed(c.Close))
	}
	c.offs = offs
	if initialEnabled {
		_ = c.SetEnabled(true)
	}
	return c
}

// SetEnabled enables or disables minimize-to-tray and reconciles the tray icon.
func (c *TrayController) SetEnabled(enabled bool) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.enabled = enabled
	if !enabled {
		oldTray := c.tray
		c.tray = nil
		c.mu.Unlock()
		if oldTray != nil {
			if c.hooks.win != nil && !c.hooks.win.IsVisible() {
				c.RestoreWindow()
			}
			oldTray.Destroy()
		}
		return nil
	}
	if c.tray != nil {
		c.mu.Unlock()
		return nil
	}
	if c.hooks.createTray == nil {
		c.enabled = false
		c.mu.Unlock()
		return errors.New("tray unavailable")
	}
	c.mu.Unlock()

	created, err := c.hooks.createTray(c.RestoreWindow, c.quit)
	if err != nil {
		c.mu.Lock()
		if c.tray == nil {
			c.enabled = false
		}
		c.mu.Unlock()
		if c.hooks.logf != nil {
			c.hooks.logf("Studio tray: %v", err)
		}
		return err
	}
	if created == nil {
		c.mu.Lock()
		if c.tray == nil {
			c.enabled = false
		}
		c.mu.Unlock()
		return errors.New("tray unavailable")
	}

	c.mu.Lock()
	if c.closed || !c.enabled || c.tray != nil {
		c.mu.Unlock()
		created.Destroy()
		return nil
	}
	c.tray = created
	c.mu.Unlock()
	return nil
}

// RestoreWindow brings the window back from hidden or minimized state and focuses it.
func (c *TrayController) RestoreWindow() {
	if c == nil || c.hooks.win == nil {
		return
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	if c.hooks.win.IsMinimized() {
		c.hooks.win.Restore()
	}
	c.hooks.win.Show()
	c.hooks.win.Focus()
	if c.hooks.focusApp != nil {
		c.hooks.focusApp()
	}
}

// Close detaches lifecycle listeners and destroys any active tray icon.
func (c *TrayController) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	oldTray := c.tray
	c.tray = nil
	offs := c.offs
	c.offs = nil
	c.mu.Unlock()

	for _, off := range offs {
		off()
	}
	if oldTray != nil {
		oldTray.Destroy()
	}
}

func (c *TrayController) shouldHideOnClose() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && !c.quitting && !c.closed && c.tray != nil
}

func (c *TrayController) markQuitting() {
	c.mu.Lock()
	c.quitting = true
	c.mu.Unlock()
}

func (c *TrayController) clearQuitting() {
	c.mu.Lock()
	c.quitting = false
	c.mu.Unlock()
}

func (c *TrayController) handleActivate(hasVisibleWindows bool) {
	if hasVisibleWindows {
		return
	}
	c.mu.Lock()
	shouldRestore := !c.quitting && !c.closed
	c.mu.Unlock()
	if shouldRestore {
		c.RestoreWindow()
	}
}

func (c *TrayController) quit() {
	if c.hooks.quitApp != nil {
		c.hooks.quitApp()
	}
}
