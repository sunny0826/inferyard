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

func TestAutoAndManualCLIUpgradeImmediatelyClearOutdatedPromptBeforeFullRefresh(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.24"
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
	for _, dir := range []string{ws, filepath.Join(ws, "results"), filepath.Join(ws, "models")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	serveSchema, err := os.ReadFile(filepath.Join("..", "..", "..", "cli", "schemas", "serve_plan.schema.json"))
	if err != nil {
		t.Fatalf("read serve_plan schema: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc, err := core.New(ctx, core.Options{
		SettingsPath:  filepath.Join(root, "settings.json"),
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				switch path {
				case oldCLI:
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.23 source=release\n")}, nil
				case newCLI:
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.24 source=release\n")}, nil
				}
			}
			if path == newCLI && len(args) == 2 && args[0] == "--schema" && args[1] == "serve_plan" {
				return inferyard.Execution{ExitCode: 0, Stdout: serveSchema}, nil
			}
			return inferyard.Execution{ExitCode: 4}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	initPatch := map[string]json.RawMessage{}
	for k, v := range map[string]string{"inferyard_path": oldCLI, "workspace_root": ws} {
		initPatch[k], _ = json.Marshal(v)
	}
	if _, apiErr := svc.UpdateSettings(initPatch); apiErr != nil {
		t.Fatal(apiErr)
	}

	preUpgradeGen := svc.CLIGeneration()
	preUpgradeReady := svc.Readiness()
	if preUpgradeReady.CLI.Status != "outdated" {
		t.Fatalf("expected initial CLI outdated, got %+v", preUpgradeReady.CLI)
	}

	// Case 1: Auto-upgrade finishes and emits sync.reset + cli.upgrade(idle) BEFORE refreshMask finishes.
	s := readyState("dashboard")
	s.ctx = ctx
	s.service = svc
	s.ready = preUpgradeReady
	s.serveSupported = false
	s.checks = core.EngineCheckOverview{
		Supported: false,
		Engines:   map[string]core.EngineEligibility{"mlx-lm": {State: core.EngineCheckUnknown, Reason: "cli_outdated"}},
	}
	s.device.LastError = core.Err("cli_outdated")
	s.registry.ModelError = core.Err("cli_outdated")
	s.cliUpgrade = core.CLIUpgradeState{Seq: 1, State: core.CLIUpgradeUpgrading, TargetVersion: "0.0.24"}

	tt := ui.NewTester(s.contentView, 1120, 800)
	tt.Frame()
	if !tt.HasText("正在自动升级 CLI") {
		t.Fatalf("expected auto-upgrading card before completion, got %v", tt.Texts())
	}

	_, _, cursor := svc.Bus.Subscribe()
	newPatch, _ := json.Marshal(newCLI)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"inferyard_path": newPatch}); apiErr != nil {
		t.Fatal(apiErr)
	}
	events, reset, _ := svc.Bus.After(cursor)
	if !eventsMayRecoverCLI(events, reset) {
		t.Fatalf("expected eventsMayRecoverCLI=true for upgrade completion events: %+v", events)
	}

	// Simulate window.go wake callback before any full refreshMask runs.
	snap := s.captureCLIRecoverySnapshot()
	if !snap.valid {
		t.Fatal("expected valid cliRecoverySnapshot once CLI is upgraded")
	}
	s.applyCLIRecoverySnapshot(snap)
	s.ingestCoreEvents(events)

	tt.Frame()
	if tt.HasText("CLI 需要升级") || tt.HasText("CLI 版本过旧") || tt.HasText("当前 CLI 过旧") {
		t.Fatalf("UI flashed outdated CLI prompt after upgrade completed: %v", tt.Texts())
	}
	if s.ready.CLI.Status != "ok" || !s.serveSupported || !s.checks.Supported {
		t.Fatalf("expected ready=ok serveSupported=true checks.Supported=true, got ready=%+v serve=%v checks=%+v", s.ready.CLI, s.serveSupported, s.checks)
	}

	// A stale pre-upgrade refreshBatch finishing later must not revert s.ready back to outdated.
	staleBatch := refreshBatch{
		epoch:      s.settingsSequence(),
		cliGen:     preUpgradeGen,
		mask:       refreshAll,
		ready:      preUpgradeReady,
		cliUpgrade: core.CLIUpgradeState{Seq: 1, State: core.CLIUpgradeUpgrading, TargetVersion: "0.0.24"},
	}
	if s.applyRefreshBatch(staleBatch) {
		t.Fatal("expected stale pre-upgrade refreshBatch to be rejected")
	}
	if s.ready.CLI.Status != "ok" {
		t.Fatalf("stale refreshBatch reverted ready.CLI to %+v", s.ready.CLI)
	}

	// Case 2: Manual UpgradeCLI button transitions s.ready before finishWork clears busy state.
	oldVer := "0.0.23"
	sManual := readyState("services")
	sManual.ctx = ctx
	sManual.service = svc
	sManual.ready = preUpgradeReady
	sManual.ready.CLI.Version = &oldVer
	sManual.serveSupported = false
	updates := make(chan func(), 16)
	sManual.updateWindow = func(fn func()) { updates <- fn }

	ttManual := ui.NewTester(sManual.contentView, 1120, 800)
	ttManual.Frame()
	if !ttManual.HasText("CLI 需要升级") {
		t.Fatalf("expected manual upgrade card before clicking upgrade, got %v", ttManual.Texts())
	}
	click(t, ttManual, "升级 CLI")

	deadline := time.After(3 * time.Second)
	for sManual.busy {
		select {
		case fn := <-updates:
			fn()
			ttManual.Frame()
		case <-deadline:
			t.Fatal("timed out waiting for manual upgradeCLI")
		}
	}
	ttManual.Frame()
	if ttManual.HasText("CLI 需要升级") || ttManual.HasText("CLI 版本过旧") || ttManual.HasText("当前 CLI 版本过旧") {
		t.Fatalf("manual upgrade still showed outdated CLI prompt after completion: %v", ttManual.Texts())
	}
	if sManual.ready.CLI.Status != "ok" || !sManual.serveSupported {
		t.Fatalf("expected manual upgrade to immediately sync ready=ok and serveSupported=true, got ready=%+v serve=%v", sManual.ready.CLI, sManual.serveSupported)
	}

	// Case 3: A recovery snapshot captured before a concurrent workspace save must not roll back s.settings or s.ready.Workspace.
	sRace := readyState("settings")
	sRace.ctx = ctx
	sRace.service = svc
	sRace.ready = preUpgradeReady
	sRace.applySettings(svc.Settings())
	sRace.openedReport = &reportViewState{reference: "r1"}

	staleEpochSnap := sRace.captureCLIRecoverySnapshot()
	newWs := filepath.Join(root, "ws-concurrent")
	wsPatchRaw, _ := json.Marshal(newWs)
	if _, saveErr := sRace.persistSettings(map[string]json.RawMessage{"workspace_root": wsPatchRaw}, settingDraft{workspace: newWs}, ""); saveErr != nil {
		t.Fatalf("persistSettings concurrent workspace: %+v", saveErr)
	}
	sRace.openedReport = &reportViewState{reference: "r2"}
	sRace.applyCLIRecoverySnapshot(staleEpochSnap)

	if got := emptyText(sRace.settings.Effective.WorkspaceRoot); got != newWs {
		t.Fatalf("applyCLIRecoverySnapshot rolled back workspace_root: got %q want %q", got, newWs)
	}
	if sRace.openedReport == nil || sRace.openedReport.reference != "r2" {
		t.Fatalf("applyCLIRecoverySnapshot unexpectedly reset workspace state: openedReport=%+v", sRace.openedReport)
	}
	if sRace.ready.CLI.Status != "ok" {
		t.Fatalf("expected CLI readiness to still advance to ok despite concurrent workspace save, got %+v", sRace.ready.CLI)
	}

	// Case 4: Prepare Environment with outdated CLI + uninitialized workspace never renders outdated warning after CLI step finishes.
	uninitWs := filepath.Join(root, "ws-uninit")
	sPrep := readyState("dashboard")
	sPrep.ctx = ctx
	sPrep.service = svc
	sPrep.ready = core.Readiness{
		CLI:       core.InferyardProbe{Status: "outdated", Version: &oldVer},
		Workspace: core.DirectoryReadiness{Path: &uninitWs, Configured: true},
	}
	sPrep.cliUpgrade = core.CLIUpgradeState{Seq: 1, State: core.CLIUpgradeUpgrading, TargetVersion: "0.0.24"}
	prepUpdates := make(chan func(), 16)
	sPrep.updateWindow = func(fn func()) { prepUpdates <- fn }

	initWsPatch, _ := json.Marshal(uninitWs)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": initWsPatch}); apiErr != nil {
		t.Fatal(apiErr)
	}
	ttPrep := ui.NewTester(sPrep.contentView, 1120, 800)
	sPrep.prepareEnvironment()
	prepDeadline := time.After(3 * time.Second)
	for sPrep.busy {
		select {
		case fn := <-prepUpdates:
			fn()
			ttPrep.Frame()
			if sPrep.cliUpgrade.State == core.CLIUpgradeIdle && ttPrep.HasText("CLI 版本过旧") {
				t.Fatalf("prepareEnvironment rendered idle + outdated CLI warning mid-flow: %v", ttPrep.Texts())
			}
		case <-prepDeadline:
			t.Fatal("timed out waiting for prepareEnvironment")
		}
	}
	ttPrep.Frame()
	if sPrep.ready.CLI.Status != "ok" || ttPrep.HasText("CLI 版本过旧") || ttPrep.HasText("CLI 需要升级") {
		t.Fatalf("prepareEnvironment left outdated CLI state: ready=%+v texts=%v", sPrep.ready, ttPrep.Texts())
	}
}
