package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestWorkspaceCleanupButtonShownInSettingsAndConfirms(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()
	called := make(chan struct{}, 1)
	updates := make(chan func(), 4)
	s.updateWindow = func(fn func()) { updates <- fn }
	s.cleanWorkspaceArtifactsFn = func() (core.WorkspaceCleanupResult, *core.ApiError) {
		called <- struct{}{}
		return core.WorkspaceCleanupResult{Removed: []string{"/workspace/results"}}, nil
	}

	tt := ui.NewTester(func(c *ui.Context) {
		s.settingsView(c)
		s.cleanWorkspaceArtifactsDialog(c)
	}, 1120, 1800)

	actionLabel := l10n.TR("settings.workspace.cleanup.action")
	confirmLabel := l10n.TR("settings.workspace.cleanup.confirm")

	if _, ok := tt.Find(actionLabel); !ok {
		t.Fatalf("expected settings page to show %q", actionLabel)
	}
	if err := tt.Click(actionLabel); err != nil {
		t.Fatalf("failed to click %q: %v", actionLabel, err)
	}
	if !s.cleanArtifactsConfirm {
		t.Fatal("expected confirmation dialog to open after clicking cleanup button")
	}
	if _, ok := tt.Find(confirmLabel); !ok {
		t.Fatalf("expected confirmation dialog to show %q", confirmLabel)
	}
	if err := tt.Click(confirmLabel); err != nil {
		t.Fatalf("failed to click %q: %v", confirmLabel, err)
	}

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-called:
	case <-timer.C:
		t.Fatal("expected cleanWorkspaceArtifactsFn to be called after confirmation")
	}
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-timer.C:
			t.Fatal("timed out draining workspace cleanup UI updates")
		}
	}
}

func TestWorkspaceCleanupButtonShownOnUnreadableBannerOnly(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()
	s.updateWindow = func(fn func()) { fn() }
	s.cleanWorkspaceArtifactsFn = func() (core.WorkspaceCleanupResult, *core.ApiError) {
		return core.WorkspaceCleanupResult{}, nil
	}

	actionLabel := l10n.TR("settings.workspace.cleanup.action")

	s.applyDashboard(dashboardSnapshot{}, core.Err("workspace_artifact_unreadable"))
	tt := ui.NewTester(func(c *ui.Context) {
		s.feedback(c)
		s.cleanWorkspaceArtifactsDialog(c)
	}, 1120, 900)
	if _, ok := tt.Find(actionLabel); !ok {
		t.Fatalf("expected refreshFailure banner for workspace_artifact_unreadable to show %q", actionLabel)
	}

	s.applyDashboard(dashboardSnapshot{}, core.Err("cli_unavailable"))
	tt = ui.NewTester(func(c *ui.Context) {
		s.feedback(c)
		s.cleanWorkspaceArtifactsDialog(c)
	}, 1120, 900)
	if _, ok := tt.Find(actionLabel); ok {
		t.Fatalf("did not expect cleanup button on unrelated refreshFailure banner")
	}

	s.refreshFailure, s.refreshFailureCode = "", ""
	s.failure = core.Err("workspace_artifact_unreadable")
	tt = ui.NewTester(func(c *ui.Context) {
		s.feedback(c)
		s.cleanWorkspaceArtifactsDialog(c)
	}, 1120, 900)
	if _, ok := tt.Find(actionLabel); !ok {
		t.Fatalf("expected failure banner for workspace_artifact_unreadable to show %q", actionLabel)
	}
}

func TestWorkspaceCleanupDisabledWhileRunActive(t *testing.T) {
	s := settingsState()
	s.cleanWorkspaceArtifactsFn = func() (core.WorkspaceCleanupResult, *core.ApiError) {
		return core.WorkspaceCleanupResult{}, nil
	}
	if !s.canCleanWorkspaceArtifacts() {
		t.Fatal("expected cleanup enabled when idle")
	}
	s.tasks = []core.RunTask{{TaskId: "r1", State: "running"}}
	if s.canCleanWorkspaceArtifacts() {
		t.Fatal("expected cleanup disabled while a run is active")
	}
}

func TestWorkspaceCleanupDisabledWhenWorkspaceUninitialized(t *testing.T) {
	s := settingsState()
	s.service = settingsTestService(t, t.TempDir())
	s.ready.Workspace.Initialized = false
	if s.canCleanWorkspaceArtifacts() {
		t.Fatal("expected cleanup disabled for uninitialized workspace")
	}
	s.ready.Workspace.Initialized = true
	if !s.canCleanWorkspaceArtifacts() {
		t.Fatal("expected cleanup enabled for initialized workspace")
	}
}

func TestWorkspaceCleanupClearsCachedRefreshAndErrors(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()
	s.service = settingsTestService(t, t.TempDir())
	updates := make(chan func(), 4)
	s.updateWindow = func(fn func()) { updates <- fn }
	s.cleanWorkspaceArtifactsFn = func() (core.WorkspaceCleanupResult, *core.ApiError) {
		return core.WorkspaceCleanupResult{Removed: []string{"/workspace/results"}}, nil
	}

	staleEpoch := s.settingsSequence()
	s.failure = core.Err("workspace_artifact_unreadable")
	s.refreshFailure = human("workspace_artifact_unreadable")
	s.refreshFailureCode = "workspace_artifact_unreadable"
	s.historyError = core.Err("io_error")
	s.reportsError = core.Err("report_unreadable")
	s.detailError = core.Err("not_found")
	s.bgRefresh = bgRefreshCache{
		rootsKey:    refreshRootsKey(s.service.Settings().Effective),
		cleanSeq:    s.cleanArtifactsSeq.Load(),
		initialized: true,
		reports:     []core.ReportSummary{{ReportId: "summary-1"}},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		s.settingsView(c)
		s.cleanWorkspaceArtifactsDialog(c)
	}, 1120, 1800)
	if err := tt.Click(l10n.TR("settings.workspace.cleanup.action")); err != nil {
		t.Fatalf("click cleanup action: %v", err)
	}
	if err := tt.Click(l10n.TR("settings.workspace.cleanup.confirm")); err != nil {
		t.Fatalf("click cleanup confirm: %v", err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-timer.C:
			t.Fatal("timed out waiting for workspace cleanup completion")
		}
	}
	if s.failure != nil || s.refreshFailure != "" || s.refreshFailureCode != "" {
		t.Fatalf("expected failure banners cleared, got failure=%+v refreshFailure=%q code=%q", s.failure, s.refreshFailure, s.refreshFailureCode)
	}
	if s.historyError != nil || s.reportsError != nil || s.detailError != nil {
		t.Fatalf("expected history/reports/detail errors cleared, got hist=%+v rep=%+v det=%+v", s.historyError, s.reportsError, s.detailError)
	}
	if s.applyRefreshBatch(refreshBatch{
		epoch:         staleEpoch,
		mask:          refreshCore | refreshProgress,
		progressError: core.Err("workspace_artifact_unreadable"),
	}) {
		t.Fatal("expected pre-cleanup refresh batch to be rejected")
	}
	if s.refreshFailureCode != "" {
		t.Fatalf("expected stale refresh batch not to restore banner, got %q", s.refreshFailureCode)
	}
	s.updateWindow = func(fn func()) { fn() }
	s.refreshMask(refreshCore)
	if len(s.bgRefresh.reports) != 0 || s.bgRefresh.progress != nil {
		t.Fatalf("expected refreshMask to invalidate pre-cleanup bgRefresh, got %+v", s.bgRefresh)
	}
}

func TestActiveRunSkipsTransientProgressAndRefreshesAfterCompletion(t *testing.T) {
	workspace := t.TempDir()
	for _, sub := range []string{"bundles", "preflight", "candidates", "bound", "results", "reports", "models", "engines"} {
		if err := os.MkdirAll(filepath.Join(workspace, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "preparation.json"), []byte(`{"kind":"community_init.v1","schema_version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(workspace, "bundles", "zh-core.json")
	if err := os.WriteFile(bundlePath, []byte(`{"bundle_id":"zh-core"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	boundDir := filepath.Join(workspace, "bound", "zh-core-1")
	if err := os.MkdirAll(boundDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resultsDir := filepath.Join(workspace, "results")
	cfgPath := filepath.Join(boundDir, "config.toml")
	cfgContent := fmt.Sprintf("[output]\nroot = %q\n[bundle]\npath = %q\n[endpoint]\nserver_pid = 12345\nprocess_start_ticks = 99999\n", resultsDir, bundlePath)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}
	cliPath := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	cfgRaw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: &workspace, InferyardPath: &cliPath})
	if err := os.WriteFile(settingsPath, cfgRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	var progressCalls atomic.Int32
	runStarted := make(chan struct{})
	releaseRun := make(chan struct{})
	svc, err := core.New(context.Background(), core.Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) == 1 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 99.0.0 source=release\n")}, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				n := progressCalls.Add(1)
				if n == 2 {
					return inferyard.Execution{
						ExitCode: inferyard.ExitBlocked,
						Stdout:   []byte(`{"command":"progress","status":"blocked","limitations":["workspace_artifact_unreadable"]}`),
					}, nil
				}
				body := `{"kind":"progress","progress_version":2,"workspace":` + fmt.Sprintf("%q", workspace) + `,"device_preflight":{"status":"ok","path":null},"bundles":[{"bundle":"zh-core","phase":"run_missing","candidate_path":null,"bound_path":null,"runs":[],"next_action":"run","next_command":null,"blocked_reason":null}],"report":{"status":"missing","path":null,"runs":[]},"next_action":"run","next_command":null,"default_scope_complete":false}`
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(body)}, nil
			}
			if len(args) > 0 && args[0] == "run" {
				close(runStarted)
				<-releaseRun
				return inferyard.Execution{
					ExitCode: 0,
					Stdout:   []byte(`{"command":"run","status":"finished","completeness":"complete"}`),
				}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := testState()
	s.ctx = ctx
	s.service = svc
	s.selected = "benchmarks"
	s.updateWindow = func(fn func()) { fn() }

	s.refreshMask(refreshAll)
	if progressCalls.Load() != 1 || s.refreshFailureCode != "" || s.progress == nil {
		t.Fatalf("expected clean initial refresh, got calls=%d err=%q progress=%v", progressCalls.Load(), s.refreshFailureCode, s.progress)
	}

	allowSingle := true
	if _, apiErr := svc.StartRuns(core.StartRunsRequest{
		ConfigPaths:       []string{cfgPath},
		AllowSingleBundle: &allowSingle,
	}); apiErr != nil {
		t.Fatalf("StartRuns failed: %+v", apiErr)
	}
	select {
	case <-runStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mock run command to start")
	}

	liveRunDir := filepath.Join(resultsDir, "20261127T010000Z-live")
	if err := os.MkdirAll(liveRunDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveRunDir, "inputs.json"), []byte(`{"schema_version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.refreshMask(refreshCore | refreshHistory | refreshProgress)
	if progressCalls.Load() != 1 {
		t.Fatalf("expected active run refresh to reuse cached progress without spawning CLI, got %d calls", progressCalls.Load())
	}
	if s.refreshFailure != "" || s.refreshFailureCode != "" {
		t.Fatalf("expected no incompatible banner during active run, got failure=%q code=%q", s.refreshFailure, s.refreshFailureCode)
	}
	if s.progress == nil {
		t.Fatal("expected s.progress preserved during active run")
	}

	_ = os.RemoveAll(liveRunDir)
	progressCalls.Store(2)
	close(releaseRun)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks := svc.RunTasks()
		if len(tasks) == 1 && tasks[0].State == "done" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	s.refreshMask(refreshCore | refreshHistory | refreshProgress)
	if progressCalls.Load() != 3 {
		t.Fatalf("expected terminal run refresh to re-run progress probe once, got calls=%d", progressCalls.Load())
	}
	if s.refreshFailure != "" || s.refreshFailureCode != "" {
		t.Fatalf("expected clean state after run completion, got failure=%q code=%q", s.refreshFailure, s.refreshFailureCode)
	}
}
