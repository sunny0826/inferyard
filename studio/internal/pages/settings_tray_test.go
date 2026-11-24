package pages

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/core"
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
	s.applyMinimizeToTray = func(v bool) {
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
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

func TestSettingsTrayRollbackOnPersistError(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()

	var mu sync.Mutex
	var applied []bool
	uiQueue := make(chan func(), 2)
	s.applyMinimizeToTray = func(v bool) {
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
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
	// Toggle off then on while the first write is still in flight.
	s.changeMinimizeToTray(false)
	s.changeMinimizeToTray(true)
	// Refresh with old value while saving must not overwrite pending choice.
	s.loadTraySettings(core.SettingsResponse{
		Settings:  core.UserSettings{MinimizeToTray: false},
		Effective: core.EffectiveSettings{MinimizeToTray: false},
	})
	if !s.minimizeToTray {
		t.Fatal("loadTraySettings must not overwrite state while save is in flight")
	}
	close(releaseFirst)

	select {
	case fn := <-uiQueue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for coalesced save")
	}
	if !s.minimizeToTray || !s.settings.Settings.MinimizeToTray {
		t.Fatalf("expected final minimizeToTray=true, got state=%v saved=%v", s.minimizeToTray, s.settings.Settings.MinimizeToTray)
	}
	mu.Lock()
	gotCalls := append([]bool(nil), calls...)
	mu.Unlock()
	if len(gotCalls) != 2 || !gotCalls[0] || !gotCalls[1] {
		t.Fatalf("expected coalesced calls [true true], got %v", gotCalls)
	}
}
