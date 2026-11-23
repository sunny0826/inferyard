package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestResolveStudioThemeAndNativeThemeSource(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want mygo.ThemeSource
	}{
		{core.ThemeSystem, mygo.ThemeSystem},
		{core.ThemeLight, mygo.ThemeLight},
		{core.ThemeDark, mygo.ThemeDark},
		{"", mygo.ThemeSystem},
		{"unknown", mygo.ThemeSystem},
	} {
		if got := nativeThemeSource(tc.mode); got != tc.want {
			t.Errorf("nativeThemeSource(%q) = %q, want %q", tc.mode, got, tc.want)
		}
	}

	var results [6]bool
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(&ui.Theme{Dark: false})
		results[0] = resolveStudioTheme(c, core.ThemeLight).Dark
		results[1] = resolveStudioTheme(c, core.ThemeDark).Dark
		results[2] = resolveStudioTheme(c, core.ThemeSystem).Dark

		c.SetTheme(&ui.Theme{Dark: true})
		results[3] = resolveStudioTheme(c, core.ThemeLight).Dark
		results[4] = resolveStudioTheme(c, core.ThemeDark).Dark
		results[5] = resolveStudioTheme(c, core.ThemeSystem).Dark
	}, 400, 300)
	tt.Frame()

	want := [6]bool{false, true, false, false, true, true}
	if results != want {
		t.Fatalf("resolveStudioTheme dark flags = %v, want %v", results, want)
	}
}

func TestSettingsAppearanceRendersInStandardAndBoundPathModes(t *testing.T) {
	s := settingsState()
	tt := settingsTester(s)
	tt.Frame()
	for _, label := range []string{"外观", "跟随系统", "浅色", "深色"} {
		if !tt.HasText(label) {
			t.Fatalf("standard settings missing %q: %v", label, tt.Texts())
		}
	}
	if tt.HasText("有未保存修改") {
		t.Fatalf("appearance section unexpectedly marked dirty: %v", tt.Texts())
	}

	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliPath, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	results := filepath.Join(root, "results")
	models := filepath.Join(root, "models")
	engines := filepath.Join(root, "engines")
	for _, dir := range []string{workspace, results, models, engines} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	svc, apiErr := core.New(context.Background(), core.Options{
		Paths: &core.BoundPaths{
			CLI: cliPath, Workspace: workspace, Results: results, Models: models, Engines: engines,
			Settings: filepath.Join(root, "settings.json"),
			Source:   "dev", WindowTitle: "Dev", SourceLabel: "dev",
		},
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	bound := settingsState()
	bound.service = svc
	boundTT := settingsTester(bound)
	boundTT.Frame()
	for _, label := range []string{"开发环境 · 固定路径", "外观", "跟随系统", "浅色", "深色"} {
		if !boundTT.HasText(label) {
			t.Fatalf("bound-path settings missing %q: %v", label, boundTT.Texts())
		}
	}
}

func TestSettingsAppearanceSwitchesImmediatelyWithoutDirtyPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	svc, apiErr := core.New(context.Background(), core.Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok"}`)}, nil
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	var nativeMu sync.Mutex
	var nativeCalls []string
	uiCh := make(chan func(), 8)

	s := settingsState()
	s.service = svc
	s.themeMode = svc.Theme()
	s.appliedThemeMode = s.themeMode
	s.applyNativeTheme = func(mode string) {
		nativeMu.Lock()
		nativeCalls = append(nativeCalls, mode)
		nativeMu.Unlock()
	}
	s.updateWindow = func(fn func()) { uiCh <- fn }

	var frameDark bool
	tt := ui.NewTester(func(c *ui.Context) {
		resolved := s.themeFor(c)
		frameDark = resolved.Dark
		c.SetTheme(resolved)
		s.settingsView(c)
	}, 1120, 1800)
	tt.Frame()

	drainUI := func() {
		t.Helper()
		select {
		case fn := <-uiCh:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for theme persistence UI callback")
		}
	}

	click(t, tt, "深色")
	if s.themeMode != core.ThemeDark || !frameDark {
		t.Fatalf("theme did not switch immediately to dark: mode=%q frameDark=%v", s.themeMode, frameDark)
	}
	if s.settingsUnsaved() {
		t.Fatal("theme switch marked settings unsaved")
	}
	s.selectPage("reports")
	if s.selected != "reports" || s.settingsLeaveConfirm {
		t.Fatalf("leaving settings after theme switch triggered confirmation: selected=%q confirm=%v", s.selected, s.settingsLeaveConfirm)
	}
	s.selected = "settings"
	drainUI()
	if svc.Theme() != core.ThemeDark {
		t.Fatalf("service theme = %q, want dark", svc.Theme())
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk core.UserSettings
	if err := json.Unmarshal(raw, &disk); err != nil || disk.Theme != core.ThemeDark {
		t.Fatalf("persisted settings.json theme mismatch: raw=%s err=%v", raw, err)
	}

	click(t, tt, "浅色")
	if s.themeMode != core.ThemeLight || frameDark {
		t.Fatalf("theme did not switch immediately to light: mode=%q frameDark=%v", s.themeMode, frameDark)
	}
	drainUI()

	click(t, tt, "跟随系统")
	if s.themeMode != core.ThemeSystem {
		t.Fatalf("theme did not switch immediately to system: mode=%q", s.themeMode)
	}
	drainUI()

	nativeMu.Lock()
	gotCalls := slices.Clone(nativeCalls)
	nativeMu.Unlock()
	if !slices.Equal(gotCalls, []string{core.ThemeDark, core.ThemeLight, core.ThemeSystem}) {
		t.Fatalf("native theme calls = %v, want [dark light system]", gotCalls)
	}

	// Switching theme remains responsive while busy and with unrelated draft edits.
	s.workspace = "/draft/workspace"
	s.busy = true
	click(t, tt, "深色")
	if s.themeMode != core.ThemeDark {
		t.Fatalf("busy/dirty state blocked theme switch: mode=%q", s.themeMode)
	}
	drainUI()
	if svc.Theme() != core.ThemeDark {
		t.Fatalf("service theme = %q, want dark with unrelated draft", svc.Theme())
	}
	if s.workspace != "/draft/workspace" {
		t.Fatalf("theme switch overwrote workspace draft: %q", s.workspace)
	}
}

func TestSettingsAppearancePersistenceFailureRollsBack(t *testing.T) {
	var nativeCalls []string
	uiCh := make(chan func(), 4)

	s := settingsState()
	s.themeMode = core.ThemeLight
	s.appliedThemeMode = core.ThemeLight
	s.settings.Settings.Theme = core.ThemeLight
	s.settings.Effective.Theme = core.ThemeLight
	s.applyNativeTheme = func(mode string) {
		nativeCalls = append(nativeCalls, mode)
	}
	s.updateWindow = func(fn func()) { uiCh <- fn }
	s.persistTheme = func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		return core.SettingsResponse{}, core.Err("internal_error")
	}

	var frameDark bool
	tt := ui.NewTester(func(c *ui.Context) {
		resolved := s.themeFor(c)
		frameDark = resolved.Dark
		c.SetTheme(resolved)
		s.contentView(c)
	}, 1120, 1400)
	tt.Frame()
	if frameDark {
		t.Fatal("initial frame unexpectedly dark")
	}

	click(t, tt, "深色")
	if s.themeMode != core.ThemeDark || !frameDark {
		t.Fatalf("expected optimistic dark switch before callback: mode=%q frameDark=%v", s.themeMode, frameDark)
	}

	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed theme callback")
	}
	tt.Frame()

	if s.themeMode != core.ThemeLight || frameDark {
		t.Fatalf("failed persistence did not roll back to light: mode=%q frameDark=%v", s.themeMode, frameDark)
	}
	if !slices.Equal(nativeCalls, []string{core.ThemeDark, core.ThemeLight}) {
		t.Fatalf("native rollback calls = %v, want [dark light]", nativeCalls)
	}
	if s.failure == nil || s.failure.Code != "internal_error" {
		t.Fatalf("expected surfaced internal_error, got %+v", s.failure)
	}
	if !tt.HasText("服务内部错误") {
		t.Fatalf("settings error banner did not surface failure: %v", tt.Texts())
	}
}

func TestSettingsAppearanceCoalescesRapidSwitchesAndRollsBackToLastSaved(t *testing.T) {
	uiCh := make(chan func(), 4)
	startedLight := make(chan struct{})
	releaseLight := make(chan struct{})

	var callMu sync.Mutex
	var recorded []string

	s := settingsState()
	s.themeMode = core.ThemeSystem
	s.appliedThemeMode = core.ThemeSystem
	s.settings.Settings.Theme = core.ThemeSystem
	s.settings.Effective.Theme = core.ThemeSystem
	s.updateWindow = func(fn func()) { uiCh <- fn }
	s.persistTheme = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		var mode string
		_ = json.Unmarshal(patch["theme"], &mode)
		callMu.Lock()
		recorded = append(recorded, mode)
		callMu.Unlock()
		if mode == core.ThemeLight {
			close(startedLight)
			<-releaseLight
			return core.SettingsResponse{
				Settings:  core.UserSettings{Theme: core.ThemeLight},
				Effective: core.EffectiveSettings{Theme: core.ThemeLight},
			}, nil
		}
		return core.SettingsResponse{}, core.Err("internal_error")
	}

	tt := settingsTester(s)
	tt.Frame()

	click(t, tt, "浅色")
	select {
	case <-startedLight:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first theme persist call")
	}

	click(t, tt, "深色")
	if s.themeMode != core.ThemeDark {
		t.Fatalf("second rapid click did not update UI immediately: mode=%q", s.themeMode)
	}
	close(releaseLight)

	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for coalesced theme callback")
	}

	callMu.Lock()
	gotCalls := slices.Clone(recorded)
	callMu.Unlock()
	if !slices.Equal(gotCalls, []string{core.ThemeLight, core.ThemeDark}) {
		t.Fatalf("persisted sequence = %v, want [light dark]", gotCalls)
	}
	if s.themeMode != core.ThemeLight || s.settings.Effective.Theme != core.ThemeLight {
		t.Fatalf("failed second switch did not roll back to saved light: mode=%q effective=%q",
			s.themeMode, s.settings.Effective.Theme)
	}
	if s.themeSaving || s.failure == nil || s.failure.Code != "internal_error" {
		t.Fatalf("unexpected post-rollback state: saving=%v failure=%+v", s.themeSaving, s.failure)
	}
}
