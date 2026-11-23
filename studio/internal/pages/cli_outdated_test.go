package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func hasSubstring(texts []string, sub string) bool {
	for _, txt := range texts {
		if strings.Contains(txt, sub) {
			return true
		}
	}
	return false
}

func TestDashboardShowsCLIUpgradeCardWhenOnlyCLIOutdated(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	s := testState()
	s.selected = "dashboard"
	s.progress = nil
	s.ready.CLI = core.InferyardProbe{
		Path:    ptr("/Users/test/.local/bin/inferyard"),
		Version: ptr("0.0.3"),
		Status:  "outdated",
	}
	s.prepLocationsLoaded = true
	s.prepLocations = core.PreparationLocations{
		ManagedPython: "3.15.2",
		CLI:           ptr("/Users/test/.local/bin/inferyard"),
		CLIStatus:     "outdated",
		CLIVersion:    ptr("0.0.3"),
	}

	tt := ui.NewTester(s.dashboardView, 1120, 1200)
	texts := tt.Texts()
	if !tt.HasText("CLI 需要升级") || !slices.Contains(texts, "升级 CLI") {
		t.Fatalf("expected CLI upgrade card and button, got: %v", texts)
	}
	if !hasSubstring(texts, "0.0.3") || !hasSubstring(texts, "0.0.9") {
		t.Fatalf("expected current and required version in dashboard upgrade card: %v", texts)
	}
	if tt.HasText("无法读取工作区进度") {
		t.Fatalf("unexpected generic progress failure message: %v", texts)
	}
}

func TestOutdatedCLIBannerShownOutsideDashboardAndHiddenInBoundMode(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	s := testState()
	s.selected = "reports"
	s.ready.CLI = core.InferyardProbe{
		Path:    ptr("/Users/test/.local/bin/inferyard"),
		Version: ptr("0.0.3"),
		Status:  "outdated",
	}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.feedback(c) })
	}, 1120, 400)
	texts := tt.Texts()
	if !slices.Contains(texts, "升级 CLI") || !hasSubstring(texts, "0.0.3") || !hasSubstring(texts, "0.0.9") {
		t.Fatalf("expected global outdated CLI banner with upgrade button on reports page, got: %v", texts)
	}

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	bound := &core.BoundPaths{
		CLI:         filepath.Join(root, "bin", "inferyard"),
		Workspace:   filepath.Join(root, "ws"),
		Results:     filepath.Join(root, "ws", "results"),
		Models:      filepath.Join(root, "models"),
		Engines:     filepath.Join(root, "engines"),
		Settings:    filepath.Join(root, "settings.json"),
		Source:      "开发环境 · dev",
		WindowTitle: "InferYard Studio · DEV · dev",
		SourceLabel: "dev",
	}
	if err := os.MkdirAll(filepath.Dir(bound.CLI), 0o755); err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(context.Background(), core.Options{Paths: bound})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	s.service = svc
	tt.Frame()
	texts = tt.Texts()
	if !hasSubstring(texts, "0.0.3") || !hasSubstring(texts, "0.0.9") {
		t.Fatalf("expected outdated CLI versions in dev mode, got: %v", texts)
	}
	if slices.Contains(texts, "升级 CLI") {
		t.Fatalf("did not expect 升级 CLI button in bound dev mode: %v", texts)
	}
}

func TestSetupActionsDistinguishesReuseAndUpgrade(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	outdatedLocs := core.PreparationLocations{
		ManagedPython: "3.15.2",
		CLI:           ptr("/Users/test/.local/bin/inferyard"),
		CLIStatus:     "outdated",
		CLIVersion:    ptr("0.0.3"),
	}
	ttOutdated := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			setupActions(c, outdatedLocs, true, core.InferyardProbe{Status: "outdated", Version: ptr("0.0.3")})
		})
	}, 900, 400)
	outdatedTexts := ttOutdated.Texts()
	for _, txt := range outdatedTexts {
		if strings.HasPrefix(txt, "CLI：复用") {
			t.Fatalf("outdated CLI rendered reuse copy: %v", outdatedTexts)
		}
	}
	if !hasSubstring(outdatedTexts, "升级") || !hasSubstring(outdatedTexts, "0.0.3") || !hasSubstring(outdatedTexts, "0.0.9") {
		t.Fatalf("expected CLI upgrade action with versions, got: %v", outdatedTexts)
	}

	okLocs := core.PreparationLocations{
		ManagedPython: "3.15.2",
		CLI:           ptr("/Users/test/.local/bin/inferyard"),
		CLIStatus:     "ok",
	}
	ttOK := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			setupActions(c, okLocs, true, core.InferyardProbe{Status: "ok", Version: ptr("0.0.9")})
		})
	}, 900, 400)
	if !hasSubstring(ttOK.Texts(), "CLI：复用") {
		t.Fatalf("expected CLI reuse copy when CLIStatus==ok, got: %v", ttOK.Texts())
	}
}

func TestApplyDashboardAndSettingsShowOutdatedCLICopy(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	s := settingsState()
	s.applyDashboard(dashboardSnapshot{}, core.Err("cli_outdated"))
	if strings.Contains(s.refreshFailure, "CLI 执行失败") {
		t.Fatalf("cli_outdated mapped to generic CLI execution failure: %q", s.refreshFailure)
	}
	if s.refreshFailure != human("cli_outdated") {
		t.Fatalf("refreshFailure = %q, want %q", s.refreshFailure, human("cli_outdated"))
	}

	cliPath := "/Users/test/.local/bin/inferyard"
	s.settings.Settings.InferyardPath = ptr(cliPath)
	s.cliPath = cliPath
	s.ready.CLI = core.InferyardProbe{
		Path:    ptr(cliPath),
		Version: ptr("0.0.3"),
		Status:  "outdated",
	}
	tt := settingsTester(s)
	texts := tt.Texts()
	if !hasSubstring(texts, "0.0.3") || !hasSubstring(texts, "0.0.9") || !hasSubstring(texts, "升级") {
		t.Fatalf("settings CLI field did not show outdated version info: %v", texts)
	}
}

func TestSettingsDraftSavePreservesUpgradedCLIPath(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	root := t.TempDir()
	oldCLI := filepath.Join(root, "old-inferyard")
	newCLI := filepath.Join(root, "new-inferyard")
	for _, p := range []string{oldCLI, newCLI} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ws := filepath.Join(root, "ws")
	oldResults := filepath.Join(root, "results-old")
	newResults := filepath.Join(root, "results-new")
	modelsDir := filepath.Join(root, "models")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc, err := core.New(ctx, core.Options{
		SettingsPath:  filepath.Join(root, "settings.json"),
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				if path == oldCLI {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
				}
				if path == newCLI {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
				}
			}
			return inferyard.Execution{ExitCode: 4}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	initPatch := map[string]json.RawMessage{}
	for k, v := range map[string]string{
		"inferyard_path": oldCLI,
		"workspace_root": ws,
		"results_root":   oldResults,
		"models_root":    modelsDir,
	} {
		initPatch[k], _ = json.Marshal(v)
	}
	if _, apiErr := svc.UpdateSettings(initPatch); apiErr != nil {
		t.Fatal(apiErr)
	}

	s := testState()
	s.selected = "settings"
	s.settingsLoaded = false
	s.service = svc
	updates := make(chan func(), 16)
	s.updateWindow = func(fn func()) { updates <- fn }
	s.applySettings(svc.Settings())

	// User has an unsaved results_root draft while clicking "升级 CLI".
	s.results = newResults
	if !s.settingsEdited() || !s.canSavePaths() {
		t.Fatalf("expected results draft to enable save: edited=%v canSave=%v", s.settingsEdited(), s.canSavePaths())
	}

	upgradedPatch, _ := json.Marshal(newCLI)
	upgradedSettings, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"inferyard_path": upgradedPatch})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	s.applySettings(upgradedSettings)

	if s.results != newResults {
		t.Fatalf("applySettings overwrote unsaved results draft: got %q want %q", s.results, newResults)
	}
	if s.cliPath != newCLI {
		t.Fatalf("applySettings did not sync untouched cliPath to upgraded CLI: got %q want %q", s.cliPath, newCLI)
	}

	tt := settingsTester(&s)
	click(t, tt, "保存")

	deadline := time.After(3 * time.Second)
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("timed out waiting for settings save")
		}
	}

	saved := svc.Settings().Settings
	if saved.ResultsRoot == nil || *saved.ResultsRoot != newResults {
		t.Fatalf("saved ResultsRoot = %v, want %q", saved.ResultsRoot, newResults)
	}
	if saved.InferyardPath == nil || *saved.InferyardPath != newCLI {
		t.Fatalf("saving results draft reverted InferyardPath to %v, want %q", saved.InferyardPath, newCLI)
	}
}
