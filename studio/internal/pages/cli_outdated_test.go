package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestSettingsDraftSavePreservesUpgradedCLIPath(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
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

func TestAutoCLIUpgradeBannerOnlyShowsOnFailure(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.12"
	t.Cleanup(func() { core.Version = origVer })

	s := readyState("reports")
	oldVer := "0.0.11"
	s.ready.CLI = core.InferyardProbe{Status: "outdated", Version: &oldVer}
	s.cliUpgrade = core.CLIUpgradeState{
		Seq:           1,
		State:         core.CLIUpgradeUpgrading,
		TargetVersion: "0.0.12",
		Message:       "正在升级 InferYard CLI 至 0.0.12…",
	}

	tt := ui.NewTester(s.contentView, 1120, 800)
	tt.Frame()
	if tt.HasText("CLI 版本过旧") || tt.HasText("升级 CLI") || tt.HasText("CLI 自动升级失败") {
		t.Fatalf("unexpected CLI banner during auto-upgrade on reports: %v", tt.Texts())
	}

	// Dashboard shows automatic upgrading progress card instead of manual upgrade prompt.
	s.selected = "dashboard"
	tt.Frame()
	if !tt.HasText("正在自动升级 CLI") || !tt.HasText("正在升级 InferYard CLI 至 0.0.12…") {
		t.Fatalf("dashboard missing auto-upgrade progress card: %v", tt.Texts())
	}
	if tt.HasText("CLI 需要升级") {
		t.Fatalf("dashboard should not show manual CLI upgrade card while auto-upgrading: %v", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if txt == "升级 CLI" {
			t.Fatalf("dashboard should not show manual Upgrade CLI button while auto-upgrading: %v", tt.Texts())
		}
	}

	// Device and Models pages suppress transient cli_outdated errors while auto-upgrading.
	s.selected = "device"
	s.device.LastError = core.Err("cli_outdated")
	tt.Frame()
	if tt.HasText("CLI 版本过旧") {
		t.Fatalf("device page should suppress cli_outdated error during auto-upgrade: %v", tt.Texts())
	}
	s.selected = "models"
	s.registry.ModelError = core.Err("cli_outdated")
	tt.Frame()
	if tt.HasText("CLI 版本过旧") {
		t.Fatalf("models page should suppress cli_outdated ModelError during auto-upgrade: %v", tt.Texts())
	}

	// Even when idle (before failure), non-bound reports page does not show an outdated warning banner.
	s.selected = "reports"
	s.applyCLIUpgradeState(core.CLIUpgradeState{Seq: 2, State: core.CLIUpgradeIdle})
	tt.Frame()
	if tt.HasText("CLI 版本过旧") || tt.HasText("CLI 自动升级失败") {
		t.Fatalf("unexpected outdated banner when CLI upgrade has not failed: %v", tt.Texts())
	}

	// Only after CLI auto-upgrade fails does the error banner appear with error details and actions.
	s.applyCLIUpgradeState(core.CLIUpgradeState{
		Seq:           3,
		State:         core.CLIUpgradeFailed,
		TargetVersion: "0.0.12",
		Error: &core.ApiError{
			Code:    "cli_upgrade_failed",
			Message: "CLI 自动升级失败，可重试或在设置中手动指定路径。",
			Details: map[string]any{"output_tail": "error: Failed to fetch from PyPI"},
		},
	})
	tt.Frame()
	if !tt.HasText("CLI 自动升级失败，可重试或在设置中手动指定路径。") || !tt.HasText("重试") || !tt.HasText("前往设置") {
		t.Fatalf("expected CLI upgrade failure banner with actions, got %v", tt.Texts())
	}

	// Device and Models pages do not duplicate cli_outdated errors alongside CLI upgrade failure banner,
	// while non-cli_outdated ModelErrors remain visible.
	s.selected = "device"
	tt.Frame()
	if tt.HasText("CLI 版本过旧") {
		t.Fatalf("device page should not duplicate cli_outdated error alongside CLI upgrade failure banner: %v", tt.Texts())
	}
	s.selected = "models"
	tt.Frame()
	if tt.HasText("CLI 版本过旧") {
		t.Fatalf("models page should not duplicate cli_outdated ModelError alongside CLI upgrade failure banner: %v", tt.Texts())
	}
	s.registry.ModelError = core.Err("io_error")
	tt.Frame()
	if !tt.HasText("文件读写失败") {
		t.Fatalf("models page should keep non-cli_outdated ModelError visible: %v", tt.Texts())
	}

	// Switching UI language after a cli_upgrade_failed error renders cleanly without double-prefixing.
	s.selected = "reports"
	tt.SetLocale("en")
	tt.Frame()
	if !tt.HasText("Automatic CLI upgrade failed. Retry or specify the path manually in Settings.") {
		t.Fatalf("expected clean English message after locale switch, got %v", tt.Texts())
	}
	if tt.HasText("Automatic CLI upgrade failed: CLI") {
		t.Fatalf("locale switch double-prefixed CLI upgrade error: %v", tt.Texts())
	}
	tt.SetLocale("zh-Hans")
	tt.Frame()

	// Dismissing closes the banner for the current failure seq, and a subsequent failure seq re-shows it.
	click(t, tt, "关闭")
	tt.Frame()
	if tt.HasText("CLI 自动升级失败，可重试或在设置中手动指定路径。") {
		t.Fatalf("dismissed CLI upgrade error banner remained visible: %v", tt.Texts())
	}
	s.applyCLIUpgradeState(core.CLIUpgradeState{
		Seq:           4,
		State:         core.CLIUpgradeFailed,
		TargetVersion: "0.0.12",
		Error: &core.ApiError{
			Code:    "download_failed",
			Message: "下载失败，请检查网络后重试。",
		},
	})
	tt.Frame()
	if !tt.HasText("CLI 自动升级失败：下载失败，请检查网络后重试。") {
		t.Fatalf("new CLI upgrade failure seq should re-open banner with wrapped message: %v", tt.Texts())
	}
}

func TestCLIUpgradeRetryDoesNotDuplicateFailureBanner(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.12"
	t.Cleanup(func() { core.Version = origVer })

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	oldCLI := filepath.Join(root, "old-inferyard")
	if err := os.WriteFile(oldCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bound := &core.BoundPaths{
		Source:    "dev",
		CLI:       oldCLI,
		Workspace: filepath.Join(root, "ws"),
		Results:   filepath.Join(root, "results"),
		Models:    filepath.Join(root, "models"),
		Engines:   filepath.Join(root, "engines"),
		Settings:  filepath.Join(root, "settings.json"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc, err := core.New(ctx, core.Options{
		Paths:         bound,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
			if path == oldCLI && len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.11 source=release\n")}, nil
			}
			return inferyard.Execution{ExitCode: 4}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	s := readyState("reports")
	s.ctx = ctx
	s.service = svc
	updates := make(chan func(), 16)
	s.updateWindow = func(fn func()) { updates <- fn }
	s.applyCLIUpgradeState(core.CLIUpgradeState{
		Seq:           1,
		State:         core.CLIUpgradeFailed,
		TargetVersion: "0.0.12",
		Error: &core.ApiError{
			Code:    "install_failed",
			Message: "CLI 自动升级失败，可重试或在设置中手动指定路径。",
		},
	})

	tt := ui.NewTester(s.contentView, 1120, 800)
	tt.Frame()
	click(t, tt, "重试")

	deadline := time.After(3 * time.Second)
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("timed out waiting for CLI upgrade retry")
		}
	}
	if s.failure != nil {
		t.Fatalf("expected s.failure to remain nil so error banner is not duplicated, got %+v", s.failure)
	}
	if s.cliUpgrade.State != core.CLIUpgradeFailed || s.cliUpgrade.Error == nil {
		t.Fatalf("expected cliUpgrade to capture retry failure, got %+v", s.cliUpgrade)
	}

	// Prepare Environment with outdated CLI also routes failure into cliUpgrade without duplicating s.failure.
	if err := os.MkdirAll(bound.Workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bound.Workspace, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.selected = "dashboard"
	s.ready.CLI = core.InferyardProbe{Status: "outdated"}
	s.ready.Workspace.Initialized = true
	s.ready.Results.Exists = false
	s.prepareEnvironment()
	deadline = time.After(3 * time.Second)
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("timed out waiting for prepareEnvironment with outdated CLI")
		}
	}
	if s.failure != nil {
		t.Fatalf("prepareEnvironment with outdated CLI duplicated failure into s.failure: %+v", s.failure)
	}
	if s.cliUpgrade.State != core.CLIUpgradeFailed || s.cliUpgrade.Error == nil {
		t.Fatalf("expected cliUpgrade to own outdated CLI upgrade failure, got %+v", s.cliUpgrade)
	}
}
