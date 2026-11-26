package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestWorkspaceCleanupButtonShownInSettingsAndConfirms(t *testing.T) {
	s := settingsState()
	s.ctx = context.Background()
	called := make(chan struct{}, 1)
	s.updateWindow = func(fn func()) { fn() }
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

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanWorkspaceArtifactsFn to be called after confirmation")
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
