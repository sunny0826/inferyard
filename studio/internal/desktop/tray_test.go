package desktop

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
)

type fakeTrayWindow struct {
	visible     bool
	minimized   bool
	hideCalls   int
	showCalls   int
	restoreCall int
	focusCalls  int
	onIsVisible func()
}

func (w *fakeTrayWindow) Hide()             { w.visible = false; w.hideCalls++ }
func (w *fakeTrayWindow) Show()             { w.visible = true; w.showCalls++ }
func (w *fakeTrayWindow) Restore()          { w.minimized = false; w.restoreCall++ }
func (w *fakeTrayWindow) Focus()            { w.focusCalls++ }
func (w *fakeTrayWindow) IsMinimized() bool { return w.minimized }
func (w *fakeTrayWindow) IsVisible() bool {
	if w.onIsVisible != nil {
		w.onIsVisible()
	}
	return w.visible
}

type fakeTrayIcon struct {
	destroyed  int
	menu       *mygo.Menu
	toolTip    string
	title      string
	titleCalls int
	icon       []byte
}

func (t *fakeTrayIcon) SetMenu(menu *mygo.Menu)   { t.menu = menu }
func (t *fakeTrayIcon) SetToolTip(toolTip string) { t.toolTip = toolTip }
func (t *fakeTrayIcon) SetTitle(title string) {
	t.title = title
	t.titleCalls++
}
func (t *fakeTrayIcon) SetIcon(icon []byte, _ bool) error {
	t.icon = icon
	return nil
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
		refreshMenu: func(icon TrayIcon, status TrayEngineStatus, onShow, onQuit func()) {
			icon.SetMenu(buildTrayMenu(status, onShow, onQuit))
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
	if err := c.MinimizeToTray(); err == nil || !h.win.visible || c.hiddenToTray {
		t.Fatalf("failed explicit minimize must leave window visible: err=%v window=%+v hidden=%v", err, h.win, c.hiddenToTray)
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

func TestTrayIconSetBadgesAndCaching(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			base.Set(x, y, color.RGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, base); err != nil {
		t.Fatal(err)
	}
	icons := newTrayIconSet(encoded.Bytes())
	if got := icons.forState(TrayEngineIdle); !bytes.Equal(got, encoded.Bytes()) {
		t.Fatal("idle state must preserve the original icon")
	}
	running := icons.forState(TrayEngineRunning)
	if bytes.Equal(running, encoded.Bytes()) {
		t.Fatal("running state must add a status badge")
	}
	again := icons.forState(TrayEngineRunning)
	if len(running) == 0 || &running[0] != &again[0] {
		t.Fatal("encoded state icons must be cached")
	}
	decoded, err := png.Decode(bytes.NewReader(running))
	if err != nil {
		t.Fatal(err)
	}
	green := false
	for y := 8; y < 16 && !green; y++ {
		for x := 8; x < 16; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if g > r*2 && g > b*2 {
				green = true
				break
			}
		}
	}
	if !green {
		t.Fatal("running badge does not contain a green status dot")
	}
	invalid := []byte("not a png")
	if got := newTrayIconSet(invalid).forState(TrayEngineFailed); !bytes.Equal(got, invalid) {
		t.Fatal("invalid PNG input must safely fall back to original bytes")
	}
}

func TestTrayControllerStatusAndTemporaryResidency(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	c := newTrayController(hooks, false)
	status := TrayEngineStatus{
		State:       TrayEngineRunning,
		ActiveCount: 1,
		Services: []TrayServiceSummary{{
			ModelName: "Qwen3-8B", EngineName: "llama.cpp", State: TrayEngineRunning, Port: 8080,
		}},
	}
	c.UpdateStatus(status)
	if len(h.icons) != 1 {
		t.Fatalf("active engine should create a temporary tray, got %d", len(h.icons))
	}
	icon := h.icons[0]
	if icon.title != "● 1" || !strings.Contains(icon.toolTip, "Qwen3-8B") || !strings.Contains(icon.toolTip, ":8080") {
		t.Fatalf("unexpected tray title/tooltip: %q / %q", icon.title, icon.toolTip)
	}
	if icon.menu == nil || len(icon.menu.Items()) != 6 || !strings.Contains(icon.menu.Items()[1].Label, "llama.cpp") {
		t.Fatalf("expected status summary and service detail menu, got %+v", icon.menu)
	}

	c.UpdateStatus(TrayEngineStatus{State: TrayEngineIdle})
	if icon.destroyed != 1 {
		t.Fatal("idle visible window should destroy the temporary tray")
	}
	if err := c.MinimizeToTray(); err != nil {
		t.Fatalf("explicit minimize should work without permanent tray setting: %v", err)
	}
	if h.win.visible || !c.hiddenToTray || len(h.icons) != 2 {
		t.Fatalf("unexpected minimize state: window=%+v hiddenToTray=%v icons=%d", h.win, c.hiddenToTray, len(h.icons))
	}
	c.RestoreWindow()
	if !h.win.visible || c.hiddenToTray || h.icons[1].destroyed != 1 {
		t.Fatalf("restore should show window and remove idle temporary tray: window=%+v hidden=%v", h.win, c.hiddenToTray)
	}
}

func TestProjectTrayStatus(t *testing.T) {
	a := core.EngineState{ServiceID: "a", State: "running", ModelName: "Model A", EngineName: "llama.cpp", Port: 8080, Endpoint: "http://127.0.0.1:8080", LANAccess: true}
	b := core.EngineState{ServiceID: "b", State: "starting", Model: "/models/model-b.gguf", Family: "mlx", Port: 8081}
	tests := []struct {
		name     string
		engine   core.EngineState
		services []core.EngineState
		state    TrayEngineState
		active   int
		count    int
	}{
		{name: "idle", engine: core.EngineState{State: "stopped"}, state: TrayEngineIdle},
		{name: "single", engine: a, state: TrayEngineRunning, active: 1, count: 1},
		{name: "multiple deduplicated", engine: a, services: []core.EngineState{a, b}, state: TrayEngineStarting, active: 2, count: 2},
		{name: "stopping", engine: core.EngineState{State: "stopping"}, services: []core.EngineState{a}, state: TrayEngineStopping, active: 2, count: 2},
		{name: "failed", engine: core.EngineState{State: "failed", ModelName: "broken"}, state: TrayEngineFailed, count: 1},
		{name: "orphaned", engine: core.EngineState{State: "orphaned"}, state: TrayEngineOrphaned, count: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ProjectTrayStatus(test.engine, test.services)
			if got.State != test.state || got.ActiveCount != test.active || len(got.Services) != test.count {
				t.Fatalf("unexpected projection: %+v", got)
			}
			if got.HasActive() != (test.state != TrayEngineIdle) {
				t.Fatalf("unexpected HasActive for %+v", got)
			}
		})
	}
	single := ProjectTrayStatus(a, nil).Services[0]
	if single.ModelName != "Model A" || single.EngineName != "llama.cpp" || single.Endpoint != a.Endpoint || !single.LANAccess {
		t.Fatalf("service summary lost fields: %+v", single)
	}
}

func TestTrayStatusBridgeRefreshesOnEngineEvents(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	c := newTrayController(hooks, true)
	svc := &core.Service{Bus: core.NewEventBus()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartTrayStatusBridge(ctx, svc, c)
	icon := h.icons[0]
	before := icon.titleCalls
	if err := svc.Bus.Publish("services.state", map[string]any{"services": []core.EngineState{}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for icon.titleCalls == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if icon.titleCalls == before {
		t.Fatal("services.state event did not refresh tray status")
	}
}

func TestTrayControllerUpdateStatusDoesNotCallIsVisibleUnderLock(t *testing.T) {
	h, hooks := newFakeTrayHarness()
	c := newTrayController(hooks, false)
	c.UpdateStatus(TrayEngineStatus{State: TrayEngineRunning, ActiveCount: 1})
	if len(h.icons) != 1 {
		t.Fatalf("expected temporary tray icon, got %d", len(h.icons))
	}
	reentered := false
	h.win.onIsVisible = func() {
		reentered = true
		_ = h.shouldHide()
	}
	c.UpdateStatus(TrayEngineStatus{State: TrayEngineIdle})
	if !reentered {
		t.Fatal("expected IsVisible callback to run")
	}
	if h.icons[0].destroyed != 1 {
		t.Fatalf("expected idle temporary tray to be destroyed, got %d", h.icons[0].destroyed)
	}
}
