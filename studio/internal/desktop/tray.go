package desktop

import (
	"errors"
	"sync"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/l10n"
)

const trayID = "inferyard-studio"

type TrayEngineState string

const (
	TrayEngineIdle     TrayEngineState = "idle"
	TrayEngineStarting TrayEngineState = "starting"
	TrayEngineRunning  TrayEngineState = "running"
	TrayEngineStopping TrayEngineState = "stopping"
	TrayEngineFailed   TrayEngineState = "failed"
	TrayEngineOrphaned TrayEngineState = "orphaned"
)

type TrayServiceSummary struct {
	ModelName  string
	EngineName string
	State      TrayEngineState
	Port       int
	Endpoint   string
	LANAccess  bool
}

type TrayEngineStatus struct {
	State       TrayEngineState
	ActiveCount int
	Services    []TrayServiceSummary
}

func (s TrayEngineStatus) HasActive() bool {
	switch s.State {
	case TrayEngineStarting, TrayEngineRunning, TrayEngineStopping, TrayEngineFailed, TrayEngineOrphaned:
		return true
	default:
		return s.ActiveCount > 0
	}
}

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
	SetMenu(*mygo.Menu)
	SetToolTip(string)
	SetTitle(string)
	SetIcon([]byte, bool) error
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
	refreshMenu    func(icon TrayIcon, status TrayEngineStatus, onShow, onQuit func())
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
	hooks        trayHooks
	mu           sync.Mutex
	enabled      bool
	quitting     bool
	closed       bool
	hiddenToTray bool
	tray         TrayIcon
	status       TrayEngineStatus
	icons        *trayIconSet
	offs         []func()
}

// NewTrayController binds tray and close-to-tray handling to a MyGo window.
func NewTrayController(cfg TrayConfig) *TrayController {
	toolTip := cfg.ToolTip
	if toolTip == "" {
		toolTip = l10n.TR("app.name")
	}
	hooks := trayHooks{
		win:      cfg.Window,
		focusApp: mygo.App.Focus,
		quitApp:  mygo.App.Quit,
		logf:     cfg.Logf,
		createTray: func(onShow, onQuit func()) (TrayIcon, error) {
			tray, err := mygo.NewTray(mygo.TrayOptions{
				ID:             trayID,
				Icon:           cfg.IconPNG,
				IconIsTemplate: false,
				ToolTip:        toolTip,
				Menu:           buildTrayMenu(TrayEngineStatus{}, onShow, onQuit),
			})
			if err != nil {
				return nil, err
			}
			tray.OnClick(onShow)
			return tray, nil
		},
		refreshMenu: func(icon TrayIcon, status TrayEngineStatus, onShow, onQuit func()) {
			icon.SetMenu(buildTrayMenu(status, onShow, onQuit))
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
		onQuitCanceled: func(fn func()) func() { return mygo.App.OnQuitCanceled(fn) },
		onActivate:     func(fn func(bool)) func() { return mygo.App.OnActivate(fn) },
	}
	controller := newTrayController(hooks, false)
	controller.icons = newTrayIconSet(cfg.IconPNG)
	if cfg.InitialEnabled {
		_ = controller.SetEnabled(true)
	}
	return controller
}

func newTrayController(hooks trayHooks, initialEnabled bool) *TrayController {
	c := &TrayController{hooks: hooks, status: TrayEngineStatus{State: TrayEngineIdle}, icons: newTrayIconSet(nil)}
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

// SetEnabled enables or disables close-to-tray and reconciles the tray icon.
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
	restore := !enabled && c.tray != nil && c.hooks.win != nil && !c.hooks.win.IsVisible()
	c.mu.Unlock()
	if restore {
		c.RestoreWindow()
	}

	c.mu.Lock()
	needed := c.trayNeededLocked()
	var oldTray TrayIcon
	if !needed {
		oldTray, c.tray = c.tray, nil
	}
	c.mu.Unlock()
	if oldTray != nil {
		oldTray.Destroy()
	}
	if !needed {
		return nil
	}
	if err := c.ensureTray(); err != nil {
		if enabled {
			c.mu.Lock()
			if c.tray == nil {
				c.enabled = false
			}
			c.mu.Unlock()
		}
		return err
	}
	c.renderTray()
	return nil
}

func (c *TrayController) trayNeededLocked() bool {
	return c.enabled || c.status.HasActive() || c.hiddenToTray
}

func (c *TrayController) ensureTray() error {
	c.mu.Lock()
	if c.closed || c.tray != nil {
		c.mu.Unlock()
		return nil
	}
	create := c.hooks.createTray
	c.mu.Unlock()
	if create == nil {
		return errors.New("tray unavailable")
	}
	created, err := create(c.RestoreWindow, c.quit)
	if err != nil {
		if c.hooks.logf != nil {
			c.hooks.logf("Studio tray: %v", err)
		}
		return err
	}
	if created == nil {
		return errors.New("tray unavailable")
	}
	c.mu.Lock()
	if c.closed || c.tray != nil || !c.trayNeededLocked() {
		c.mu.Unlock()
		created.Destroy()
		return nil
	}
	c.tray = created
	c.mu.Unlock()
	return nil
}

// UpdateStatus refreshes the icon, title, tooltip, and menu for the latest engine state.
func (c *TrayController) UpdateStatus(status TrayEngineStatus) {
	if c == nil {
		return
	}
	if status.State == "" {
		status.State = TrayEngineIdle
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.status = status
	needed := c.trayNeededLocked()
	visible := c.hooks.win == nil || c.hooks.win.IsVisible()
	var oldTray TrayIcon
	if !needed && visible {
		oldTray, c.tray = c.tray, nil
	}
	c.mu.Unlock()
	if oldTray != nil {
		oldTray.Destroy()
	}
	if needed && c.ensureTray() == nil {
		c.renderTray()
	}
}

// MinimizeToTray hides the window while keeping engines running in the background.
func (c *TrayController) MinimizeToTray() error {
	if c == nil || c.hooks.win == nil {
		return errors.New("tray window unavailable")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	previous := c.hiddenToTray
	c.hiddenToTray = true
	c.mu.Unlock()
	if err := c.ensureTray(); err != nil {
		c.mu.Lock()
		c.hiddenToTray = previous
		c.mu.Unlock()
		return err
	}
	c.renderTray()
	c.hooks.win.Hide()
	return nil
}

// RefreshLocale rebuilds the active tray content in the current UI locale.
func (c *TrayController) RefreshLocale() { c.renderTray() }

func (c *TrayController) renderTray() {
	if c == nil {
		return
	}
	c.mu.Lock()
	icon, status, closed := c.tray, c.status, c.closed
	icons, refresh := c.icons, c.hooks.refreshMenu
	c.mu.Unlock()
	if closed || icon == nil {
		return
	}
	if icons != nil {
		if err := icon.SetIcon(icons.forState(status.State), false); err != nil && c.hooks.logf != nil {
			c.hooks.logf("Studio tray icon: %v", err)
		}
	}
	icon.SetTitle(trayTitle(status))
	icon.SetToolTip(trayToolTip(status))
	if refresh != nil {
		refresh(icon, status, c.RestoreWindow, c.quit)
	}
}

// RestoreWindow brings the window back from hidden or minimized state and focuses it.
func (c *TrayController) RestoreWindow() {
	if c == nil || c.hooks.win == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.hiddenToTray = false
	c.mu.Unlock()
	if c.hooks.win.IsMinimized() {
		c.hooks.win.Restore()
	}
	c.hooks.win.Show()
	c.hooks.win.Focus()
	if c.hooks.focusApp != nil {
		c.hooks.focusApp()
	}
	c.mu.Lock()
	var oldTray TrayIcon
	if !c.trayNeededLocked() {
		oldTray, c.tray = c.tray, nil
	}
	c.mu.Unlock()
	if oldTray != nil {
		oldTray.Destroy()
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
