package pages

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestSettingsTraySwitchTogglesAndPersists(t *testing.T) {
	service := settingsTestService(t, t.TempDir())
	s := settingsState()
	s.ctx = context.Background()
	s.service = service
	s.settings = service.Settings()
	s.loadSettings(s.settings)

	var mu sync.Mutex
	var applied []bool
	uiQueue := make(chan func(), 4)
	s.applyMinimizeToTray = func(v bool) error {
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
		return nil
	}
	s.updateWindow = func(fn func()) {
		uiQueue <- fn
	}

	tt := settingsTester(s)
	tt.Frame()
	if s.minimizeToTray {
		t.Fatal("expected switch initially off")
	}

	click(t, tt, "最小化到系统托盘")
	if !s.minimizeToTray {
		t.Fatal("expected switch toggled on optimistically")
	}
	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for minimize_to_tray persist")
	}
	tt.Frame()
	if !service.MinimizeToTray() {
		t.Fatal("expected minimize_to_tray persisted to service")
	}
	if s.settingsUnsaved() {
		t.Fatal("immediate tray toggle must not mark settings form unsaved")
	}
	mu.Lock()
	gotApplied := append([]bool(nil), applied...)
	mu.Unlock()
	if len(gotApplied) != 1 || !gotApplied[0] {
		t.Fatalf("expected applyMinimizeToTray([true]), got %v", gotApplied)
	}
}

func TestSettingsTrayUnavailableRevertsWithoutPersisting(t *testing.T) {
	service := settingsTestService(t, t.TempDir())
	s := settingsState()
	s.ctx = context.Background()
	s.service = service
	s.settings = service.Settings()
	s.loadSettings(s.settings)

	s.applyMinimizeToTray = func(bool) error {
		return errors.New("appindicator missing")
	}

	tt := settingsTester(s)
	tt.Frame()
	click(t, tt, "最小化到系统托盘")
	tt.Frame()

	if s.minimizeToTray {
		t.Fatal("expected switch to remain off when tray creation fails")
	}
	if service.MinimizeToTray() {
		t.Fatal("expected minimize_to_tray not persisted when tray creation fails")
	}
	if s.failure == nil || s.failure.Code != "tray_unavailable" || s.failurePage != "settings" {
		t.Fatalf("expected tray_unavailable failure on settings page, got failure=%+v page=%q", s.failure, s.failurePage)
	}
}

func TestSettingsTrayRollbackOnPersistError(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()

	var mu sync.Mutex
	var applied []bool
	uiQueue := make(chan func(), 2)
	s.applyMinimizeToTray = func(v bool) error {
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
		return nil
	}
	s.persistMinimizeToTray = func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		return core.SettingsResponse{}, core.Err("internal_error")
	}
	s.updateWindow = func(fn func()) {
		uiQueue <- fn
	}

	s.changeMinimizeToTray(true)
	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for persist error handling")
	}
	if s.minimizeToTray {
		t.Fatal("expected minimizeToTray rolled back to false on error")
	}
	if s.failure == nil || s.failurePage != "settings" {
		t.Fatalf("expected settings failure recorded, got failure=%+v page=%q", s.failure, s.failurePage)
	}
	mu.Lock()
	gotApplied := append([]bool(nil), applied...)
	mu.Unlock()
	if len(gotApplied) != 2 || !gotApplied[0] || gotApplied[1] {
		t.Fatalf("expected apply sequence [true false] after rollback, got %v", gotApplied)
	}
}

func TestSettingsTrayRapidTogglesCoalesceToLatest(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	uiQueue := make(chan func(), 2)
	var mu sync.Mutex
	var calls []bool

	s.persistMinimizeToTray = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		var enabled bool
		_ = json.Unmarshal(patch["minimize_to_tray"], &enabled)
		mu.Lock()
		calls = append(calls, enabled)
		first := len(calls) == 1
		mu.Unlock()
		if first {
			close(firstStarted)
			<-releaseFirst
		}
		return core.SettingsResponse{
			Settings:  core.UserSettings{MinimizeToTray: enabled},
			Effective: core.EffectiveSettings{MinimizeToTray: enabled},
		}, nil
	}
	s.updateWindow = func(fn func()) {
		uiQueue <- fn
	}

	s.changeMinimizeToTray(true)
	<-firstStarted
	// Toggle false -> true -> false while the first write is still in flight.
	s.changeMinimizeToTray(false)
	s.changeMinimizeToTray(true)
	s.changeMinimizeToTray(false)
	// Refresh with stale value while saving must not overwrite pending choice.
	s.loadTraySettings(core.SettingsResponse{
		Settings:  core.UserSettings{MinimizeToTray: true},
		Effective: core.EffectiveSettings{MinimizeToTray: true},
	})
	if s.minimizeToTray {
		t.Fatal("loadTraySettings must not overwrite state while save is in flight")
	}
	close(releaseFirst)

	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for coalesced save")
	}
	if s.minimizeToTray || s.settings.Settings.MinimizeToTray {
		t.Fatalf("expected final minimizeToTray=false, got state=%v saved=%v", s.minimizeToTray, s.settings.Settings.MinimizeToTray)
	}
	mu.Lock()
	gotCalls := append([]bool(nil), calls...)
	mu.Unlock()
	if len(gotCalls) >= 4 || len(gotCalls) == 0 || gotCalls[len(gotCalls)-1] != false {
		t.Fatalf("expected coalesced writes ending with false, got %v", gotCalls)
	}
}

func TestSettingsTrayCloseFlushesPendingToggleAfterCancel(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	execFn := func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
	}
	service, err := core.New(context.Background(), core.Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = service.Shutdown(context.Background()) }()

	ctx, cancel := context.WithCancel(context.Background())
	s := settingsState()
	s.ctx = ctx
	s.service = service
	s.settings = service.Settings()
	s.loadSettings(s.settings)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstOnce sync.Once
	s.persistMinimizeToTray = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		resp, apiErr := service.UpdateSettings(patch)
		firstOnce.Do(func() {
			close(firstStarted)
			<-releaseFirst
		})
		return resp, apiErr
	}

	// Enable, wait until the first write (true) is in flight, then disable and immediately close.
	s.changeMinimizeToTray(true)
	<-firstStarted
	s.changeMinimizeToTray(false)
	cancel()
	close(releaseFirst)
	s.waitMinimizeToTrayPersist()

	if service.MinimizeToTray() {
		t.Fatal("expected service MinimizeToTray to be false after close flush")
	}
	reloaded, err := core.New(context.Background(), core.Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatalf("reload core.New: %v", err)
	}
	defer func() { _ = reloaded.Shutdown(context.Background()) }()
	if reloaded.MinimizeToTray() {
		t.Fatal("expected reloaded settings.json MinimizeToTray to be false after close flush")
	}
}

func TestMinimizeToTrayActionAndFailure(t *testing.T) {
	s := settingsState()
	calls := 0
	s.minimizeToTrayNow = func() error {
		calls++
		return nil
	}
	if !s.canMinimizeToTray() {
		t.Fatal("explicit tray callback should enable minimize action")
	}
	s.minimizeToTrayAction()
	if calls != 1 || s.trayUnavailable {
		t.Fatalf("unexpected successful minimize state: calls=%d unavailable=%v", calls, s.trayUnavailable)
	}

	s.minimizeToTrayNow = func() error { return errors.New("appindicator missing") }
	s.minimizeToTrayAction()
	if !s.trayUnavailable || s.failure == nil || s.failure.Code != "tray_unavailable" {
		t.Fatalf("minimize failure should mark tray unavailable: failure=%+v unavailable=%v", s.failure, s.trayUnavailable)
	}
	if s.canMinimizeToTray() {
		t.Fatal("unavailable tray must disable subsequent minimize actions")
	}
}

func TestQuitDialogMinimizesWithoutStoppingServices(t *testing.T) {
	s, _ := servicesP2State(t)
	s.engineState = core.EngineState{
		State: "running", Origin: core.EngineOriginStandalone, ModelName: "Qwen3-8B-Q4.gguf",
	}
	s.quitDialog.open = true
	minimized := 0
	s.minimizeToTrayNow = func() error {
		minimized++
		return nil
	}
	tt := newServicesTester(s)
	tt.Frame()
	click(t, tt, "最小化到系统托盘")
	tt.Frame()
	if minimized != 1 || s.quitDialog.open || s.quitDialog.bypassClose {
		t.Fatalf("unexpected minimize dialog state: calls=%d dialog=%+v", minimized, s.quitDialog)
	}
}
