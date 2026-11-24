package desktop

import (
	"errors"
	"testing"
)

type fakeTrayWindow struct {
	visible     bool
	minimized   bool
	hideCalls   int
	showCalls   int
	restoreCall int
	focusCalls  int
}

func (w *fakeTrayWindow) Hide()             { w.visible = false; w.hideCalls++ }
func (w *fakeTrayWindow) Show()             { w.visible = true; w.showCalls++ }
func (w *fakeTrayWindow) Restore()          { w.minimized = false; w.restoreCall++ }
func (w *fakeTrayWindow) Focus()            { w.focusCalls++ }
func (w *fakeTrayWindow) IsMinimized() bool { return w.minimized }
func (w *fakeTrayWindow) IsVisible() bool   { return w.visible }

type fakeTrayIcon struct {
	destroyed int
}

func (t *fakeTrayIcon) Destroy() { t.destroyed++ }

type fakeTrayHarness struct {
	win           *fakeTrayWindow
	icons         []*fakeTrayIcon
	createErr     error
	focusAppCalls int
	quitCalls     int
	logs          int
	detached      int

	onShow         func()
	onQuit         func()
	shouldHide     func() bool
	onClosed       func()
	onBeforeQuit   func()
	onQuitCanceled func()
	onActivate     func(bool)
}

func newFakeTrayHarness() (*fakeTrayHarness, trayHooks) {
	h := &fakeTrayHarness{win: &fakeTrayWindow{visible: true}}
	detach := func() func() {
		return func() { h.detached++ }
	}
	hooks := trayHooks{
		win: h.win,
		createTray: func(onShow, onQuit func()) (TrayIcon, error) {
			if h.createErr != nil {
				return nil, h.createErr
			}
			h.onShow = onShow
			h.onQuit = onQuit
			icon := &fakeTrayIcon{}
			h.icons = append(h.icons, icon)
			return icon, nil
		},
		focusApp: func() { h.focusAppCalls++ },
		quitApp:  func() { h.quitCalls++ },
		logf:     func(string, ...any) { h.logs++ },
		onClose: func(fn func() bool) func() {
			h.shouldHide = fn
			return detach()
		},
		onClosed: func(fn func()) func() {
			h.onClosed = fn
			return detach()
		},
		onBeforeQuit: func(fn func()) func() {
			h.onBeforeQuit = fn
			return detach()
		},
		onQuitCanceled: func(fn func()) func() {
			h.onQuitCanceled = fn
			return detach()
		},
		onActivate: func(fn func(bool)) func() {
			h.onActivate = fn
			return detach()
		},
	}
	return h, hooks
}

func TestTrayControllerToggleAndCloseToTray(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	c := newTrayController(hooks, false)

	if len(h.icons) != 0 {
		t.Fatalf("expected no tray icon when disabled, got %d", len(h.icons))
	}
	if h.shouldHide() {
		t.Fatal("close should not hide when minimize-to-tray is disabled")
	}

	if err := c.SetEnabled(true); err != nil {
		t.Fatalf("enable tray failed: %v", err)
	}
	if len(h.icons) != 1 {
		t.Fatalf("expected 1 tray icon after enable, got %d", len(h.icons))
	}
	// Idempotent enable should not create duplicate tray icons.
	if err := c.SetEnabled(true); err != nil {
		t.Fatalf("idempotent enable failed: %v", err)
	}
	if len(h.icons) != 1 {
		t.Fatalf("expected idempotent enable to keep 1 tray icon, got %d", len(h.icons))
	}
	if !h.shouldHide() {
		t.Fatal("close should hide to tray when enabled")
	}

	// Simulate close hiding the window, then restoring via tray click/menu.
	h.win.Hide()
	h.win.minimized = true
	h.onShow()
	if !h.win.visible || h.win.minimized || h.win.restoreCall != 1 || h.win.showCalls != 1 || h.win.focusCalls != 1 || h.focusAppCalls != 1 {
		t.Fatalf("unexpected window restore state: %+v, focusApp=%d", h.win, h.focusAppCalls)
	}

	// Simulate hiding again and re-activating app (e.g. macOS Dock click).
	h.win.Hide()
	h.onActivate(false)
	if !h.win.visible || h.win.showCalls != 2 {
		t.Fatalf("expected OnActivate(false) to restore hidden window: %+v", h.win)
	}

	// Disabling while hidden restores window before destroying tray.
	h.win.Hide()
	c.SetEnabled(false)
	if !h.win.visible {
		t.Fatal("disabling minimize-to-tray while hidden must restore window")
	}
	if h.icons[0].destroyed != 1 {
		t.Fatalf("expected tray icon destroyed once, got %d", h.icons[0].destroyed)
	}
	if h.shouldHide() {
		t.Fatal("close should not hide after disabling minimize-to-tray")
	}
}

func TestTrayControllerQuitLifecycleAndCleanup(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	c := newTrayController(hooks, true)

	// Tray quit action requests app quit.
	h.onQuit()
	if h.quitCalls != 1 {
		t.Fatalf("expected tray quit to call App.Quit, got %d", h.quitCalls)
	}

	// BeforeQuit allows the window to close normally instead of hiding.
	h.onBeforeQuit()
	if h.shouldHide() {
		t.Fatal("close must not be intercepted during quit")
	}

	// If quit is canceled, close-to-tray resumes.
	h.onQuitCanceled()
	if !h.shouldHide() {
		t.Fatal("close-to-tray should resume after quit is canceled")
	}

	// Closing the window cleans up tray and listeners.
	h.onBeforeQuit()
	h.onClosed()
	if h.icons[0].destroyed != 1 {
		t.Fatalf("expected tray destroyed on window close, got %d", h.icons[0].destroyed)
	}
	if h.detached != 5 {
		t.Fatalf("expected all 5 listeners detached, got %d", h.detached)
	}

	// Calls after Close are no-ops.
	c.SetEnabled(true)
	if len(h.icons) != 1 {
		t.Fatalf("expected no tray created after Close, got %d", len(h.icons))
	}
}

func TestTrayControllerCreateFailureDoesNotStrandWindow(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	h.createErr = errors.New("appindicator missing")
	c := newTrayController(hooks, false)

	if err := c.SetEnabled(true); err == nil {
		t.Fatal("expected SetEnabled(true) to return tray creation error")
	}
	if h.logs != 1 {
		t.Fatalf("expected tray creation error to be logged once, got %d", h.logs)
	}
	if h.shouldHide() {
		t.Fatal("must not hide window on close when tray creation failed")
	}

	// Recovering later succeeds directly on retry.
	h.createErr = nil
	if err := c.SetEnabled(true); err != nil {
		t.Fatalf("expected retry enable to succeed, got %v", err)
	}
	if len(h.icons) != 1 || !h.shouldHide() {
		t.Fatalf("expected retry enable to create tray and allow close-to-tray: icons=%d", len(h.icons))
	}
}
