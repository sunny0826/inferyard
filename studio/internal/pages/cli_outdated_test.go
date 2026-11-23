package pages

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
	if !tt.HasText("CLI 需要升级") || !slices.Contains(tt.Texts(), "升级 CLI") {
		t.Fatalf("expected CLI upgrade card and button, got: %v", tt.Texts())
	}
	if !tt.HasText("CLI 版本过旧（当前 0.0.3，需要 0.0.9），请升级 CLI。") {
		t.Fatalf("expected outdated summary copy, got: %v", tt.Texts())
	}
	if tt.HasText("无法读取工作区进度") {
		t.Fatalf("unexpected generic progress failure message: %v", tt.Texts())
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
	if !slices.Contains(tt.Texts(), "升级 CLI") || !tt.HasText("CLI 版本过旧（当前 0.0.3，需要 0.0.9），请升级 CLI。") {
		t.Fatalf("expected global outdated CLI banner on reports page, got: %v", tt.Texts())
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
	if !tt.HasText("CLI 版本过旧（当前 0.0.3，需要 0.0.9），请升级 CLI。") {
		t.Fatalf("expected outdated CLI summary in dev mode, got: %v", tt.Texts())
	}
	if slices.Contains(tt.Texts(), "升级 CLI") {
		t.Fatalf("did not expect 升级 CLI button in bound dev mode: %v", tt.Texts())
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
	for _, txt := range ttOutdated.Texts() {
		if strings.HasPrefix(txt, "CLI：复用") {
			t.Fatalf("outdated CLI rendered reuse copy: %v", ttOutdated.Texts())
		}
	}
	if !ttOutdated.HasText("CLI：从 0.0.3 升级至 0.0.9（Python 3.15.2）") {
		t.Fatalf("expected CLI upgrade action copy, got: %v", ttOutdated.Texts())
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
	if !ttOK.HasText("CLI：复用 " + displayName("/Users/test/.local/bin/inferyard")) {
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
	if !tt.HasText("CLI 版本过旧（当前 0.0.3，需要 0.0.9），请升级 CLI。") {
		t.Fatalf("settings CLI field did not show outdated version info: %v", tt.Texts())
	}
}
