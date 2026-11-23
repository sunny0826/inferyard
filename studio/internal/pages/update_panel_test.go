package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func sampleUpdateRelease() *core.UpdateRelease {
	return &core.UpdateRelease{
		Version:      "0.10.0",
		TagName:      "v0.10.0",
		Name:         "v0.10.0",
		Notes:        "<!-- comment -->\n## Highlights\n- feat(studio): one-click update",
		ReleaseURL:   "https://github.com/sunny0826/inferyard/releases/tag/v0.10.0",
		AssetName:    "inferyard-studio-v0.10.0-darwin-arm64.dmg",
		AssetURL:     "https://github.com/sunny0826/inferyard/releases/download/v0.10.0/inferyard-studio-v0.10.0-darwin-arm64.dmg",
		AssetSize:    15 << 20,
		ChecksumsURL: "https://github.com/sunny0826/inferyard/releases/download/v0.10.0/SHA256SUMS",
	}
}

func TestUpdateHeaderBadgeVisibilityAndDialogOpen(t *testing.T) {
	s := readyState("reports")
	tt := sidebarHeaderState(s)
	tt.Frame()

	for _, st := range []core.UpdateState{
		{State: core.UpdateIdle, CurrentVersion: "0.0.8"},
		{State: core.UpdateUpToDate, CurrentVersion: "0.0.8"},
		{State: core.UpdateChecking, CurrentVersion: "0.0.8", ManualCheck: false},
		{State: core.UpdateError, CurrentVersion: "0.0.8", ErrorMessage: "network error"},
	} {
		s.updateState = st
		tt.Frame()
		if tt.HasText("新版本") || tt.HasText("更新失败") || tt.HasText("下载更新") {
			t.Fatalf("unexpected header badge for state %+v: %v", st, tt.Texts())
		}
	}

	s.updateState = core.UpdateState{
		State:          core.UpdateAvailable,
		CurrentVersion: "0.0.8",
		Release:        sampleUpdateRelease(),
		CanSelfUpdate:  true,
	}
	tt.Frame()
	if !tt.HasText("新版本 v0.10.0") {
		t.Fatalf("expected available header badge, got %v", tt.Texts())
	}
	click(t, tt, "新版本 v0.10.0")
	if !s.updateDialogOpen {
		t.Fatal("clicking update badge did not open update dialog")
	}

	s.updateState.State = core.UpdateDownloading
	s.updateState.DownloadedBytes = 6 << 20
	s.updateState.TotalBytes = ptr(int64(15 << 20))
	tt.Frame()
	if !tt.HasText("下载更新 40%") {
		t.Fatalf("expected downloading percentage badge, got %v", tt.Texts())
	}

	s.updateState.State = core.UpdateVerifying
	tt.Frame()
	if !tt.HasText("校验更新") {
		t.Fatalf("expected verifying badge, got %v", tt.Texts())
	}

	s.updateState.State = core.UpdateReadyToApply
	tt.Frame()
	if !tt.HasText("待安装 v0.10.0") {
		t.Fatalf("expected ready badge, got %v", tt.Texts())
	}

	s.updateState.State = core.UpdateError
	s.updateState.ErrorMessage = "校验失败"
	tt.Frame()
	if !tt.HasText("更新失败") {
		t.Fatalf("expected error badge when release exists, got %v", tt.Texts())
	}
}

func TestUpdateDialogActionsActiveWorkloadWarningAndFallback(t *testing.T) {
	origOpen := openUpdatePathFunc
	t.Cleanup(func() { openUpdatePathFunc = origOpen })
	var openedPath string
	openUpdatePathFunc = func(path string) error {
		openedPath = path
		return nil
	}

	s := readyState("reports")
	uiCh := make(chan func(), 4)
	s.updateWindow = func(fn func()) { uiCh <- fn }
	s.updateDialogOpen = true
	s.updateState = core.UpdateState{
		State:          core.UpdateAvailable,
		CurrentVersion: "0.0.8",
		Release:        sampleUpdateRelease(),
		CanSelfUpdate:  true,
	}

	var started, cancelled int
	var applyCalls []bool
	s.updateStartFn = func() { started++ }
	s.updateCancelFn = func() core.UpdateState {
		cancelled++
		return core.UpdateState{State: core.UpdateAvailable, CurrentVersion: "0.0.8", Release: sampleUpdateRelease(), CanSelfUpdate: true}
	}
	s.updateApplyFn = func(_ context.Context, confirmStop bool) (core.UpdateState, error) {
		applyCalls = append(applyCalls, confirmStop)
		return core.UpdateState{
			State:          core.UpdateApplying,
			CurrentVersion: "0.0.8",
			Release:        sampleUpdateRelease(),
		}, nil
	}

	tt := ui.NewTester(s.contentView, 1120, 800)
	tt.Frame()
	if !tt.HasText("v0.0.8 → v0.10.0") || !tt.HasText("one-click update") || tt.HasText("comment") {
		t.Fatalf("unexpected dialog content: %v", tt.Texts())
	}

	click(t, tt, "立即更新")
	if started != 1 {
		t.Fatalf("expected updateStartFn call, got %d", started)
	}

	s.updateState.State = core.UpdateDownloading
	s.updateState.DownloadedBytes = 3 << 20
	s.updateState.TotalBytes = ptr(int64(15 << 20))
	tt.Frame()
	click(t, tt, "取消下载")
	if cancelled != 1 || s.updateState.State != core.UpdateAvailable {
		t.Fatalf("cancel failed: cancelled=%d state=%+v", cancelled, s.updateState)
	}

	// Active workload warning blocks quiet apply and switches CTA to "停止任务并安装".
	s.tasks = []core.RunTask{{TaskId: "r1", State: "running"}}
	s.updateState.State = core.UpdateReadyToApply
	tt.Frame()
	if !tt.HasText("停止任务并安装") || !tt.HasText("1 个测评任务") {
		t.Fatalf("expected active workload warning & confirm CTA: %v", tt.Texts())
	}
	click(t, tt, "停止任务并安装")
	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stop-and-apply UI callback")
	}
	s.tasks = nil
	s.updateState.State = core.UpdateReadyToApply
	tt.Frame()
	if !tt.HasText("更新并重启") {
		t.Fatalf("expected idle apply CTA: %v", tt.Texts())
	}
	click(t, tt, "更新并重启")
	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for idle-apply UI callback")
	}
	if len(applyCalls) != 2 || !applyCalls[0] || applyCalls[1] {
		t.Fatalf("unexpected applyCalls: %v", applyCalls)
	}

	// Error fallback offers Retry, Open downloaded file, and Open Release Page.
	s.updateState.State = core.UpdateError
	s.updateState.ErrorCode = "update_path_unwritable"
	s.updateState.ErrorMessage = "目录不可写"
	s.updateState.FallbackPath = "/tmp/inferyard-studio-v0.10.0-darwin-arm64.dmg"
	tt.Frame()
	click(t, tt, "打开安装包")
	if openedPath != s.updateState.FallbackPath {
		t.Fatalf("openUpdatePathFunc got %q, want %q", openedPath, s.updateState.FallbackPath)
	}
	click(t, tt, "打开发布页")
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != s.updateState.Release.ReleaseURL {
		t.Fatalf("expected release URL opened, got %v", urls)
	}
}

func TestSettingsUpdateSectionManualCheckAndNonDirtyInvariant(t *testing.T) {
	s := settingsState()
	uiCh := make(chan func(), 4)
	s.updateWindow = func(fn func()) { uiCh <- fn }
	s.updateState = core.UpdateState{State: core.UpdateIdle, CurrentVersion: "0.0.8"}
	checks := 0
	s.updateCheckFn = func(context.Context) (core.UpdateState, error) {
		checks++
		return core.UpdateState{State: core.UpdateUpToDate, CurrentVersion: "0.0.8"}, nil
	}

	tt := settingsTester(s)
	tt.Frame()
	if !tt.HasText("软件更新") || !tt.HasText("检查更新") {
		t.Fatalf("settings missing update section: %v", tt.Texts())
	}
	if s.settingsUnsaved() || tt.HasText("有未保存修改") {
		t.Fatalf("update section unexpectedly marked settings dirty: %v", tt.Texts())
	}
	click(t, tt, "检查更新")
	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update check UI callback")
	}
	tt.Frame()
	if checks != 1 || !tt.HasText("已是最新版本") || !s.updateDialogOpen {
		t.Fatalf("manual check did not update feedback or open dialog: checks=%d dialog=%v texts=%v", checks, s.updateDialogOpen, tt.Texts())
	}
}

func TestUpdateStateRejectsStaleSeqCallbacks(t *testing.T) {
	s := readyState("reports")
	s.applyUpdateState(core.UpdateState{Seq: 3, State: core.UpdateDownloading, CurrentVersion: "0.0.8", Release: sampleUpdateRelease()})
	s.applyUpdateState(core.UpdateState{Seq: 2, State: core.UpdateAvailable, CurrentVersion: "0.0.8", Release: sampleUpdateRelease()})
	if s.updateState.State != core.UpdateDownloading || s.updateState.Seq != 3 {
		t.Fatalf("stale seq 2 overwrote seq 3: %+v", s.updateState)
	}
}

func TestUpdateReadyPromptCardActionsAndWorkloadGuard(t *testing.T) {
	s := readyState("reports")
	uiCh := make(chan func(), 4)
	s.updateWindow = func(fn func()) { uiCh <- fn }
	s.updateState = core.UpdateState{
		Seq:            4,
		State:          core.UpdateReadyToApply,
		CurrentVersion: "0.0.8",
		Release:        sampleUpdateRelease(),
		CanSelfUpdate:  true,
	}
	var applyCalls []bool
	s.updateApplyFn = func(_ context.Context, confirmStop bool) (core.UpdateState, error) {
		applyCalls = append(applyCalls, confirmStop)
		return core.UpdateState{Seq: 5, State: core.UpdateApplying, CurrentVersion: "0.0.8", Release: sampleUpdateRelease()}, nil
	}

	tt := ui.NewTester(s.contentView, 1120, 800)
	tt.Frame()
	for _, want := range []string{"发现新版本", "版本 0.10.0 已可更新，重启后生效。", "查看更新日志", "稍后", "更新并重启"} {
		if !tt.HasText(want) {
			t.Fatalf("ready prompt missing %q: %v", want, tt.Texts())
		}
	}

	// Clicking "查看更新日志" opens the full update modal and hides the floating card.
	click(t, tt, "查看更新日志")
	if !s.updateDialogOpen {
		t.Fatal("clicking 查看更新日志 did not open update dialog")
	}
	s.updateDialogOpen = false
	tt.Frame()

	// Clicking "稍后" dismisses the floating card for this version while preserving ready state.
	click(t, tt, "稍后")
	if s.updateReadyPromptDismissedVersion != "0.10.0" || tt.HasText("版本 0.10.0 已可更新，重启后生效。") {
		t.Fatalf("稍后 did not dismiss ready card: dismissed=%q texts=%v", s.updateReadyPromptDismissedVersion, tt.Texts())
	}

	// Active workload prevents silent apply from floating card and opens dialog for confirmation instead.
	s.updateReadyPromptDismissedVersion = ""
	s.tasks = []core.RunTask{{TaskId: "r1", State: "running"}}
	tt.Frame()
	click(t, tt, "更新并重启")
	if !s.updateDialogOpen || len(applyCalls) != 0 {
		t.Fatalf("active workload should open dialog instead of silent apply: dialog=%v calls=%v", s.updateDialogOpen, applyCalls)
	}

	// Active workload returned lazily by ApplyUpdate opens modal when triggered from closed floating card.
	s.tasks = nil
	s.updateDialogOpen = false
	s.updateApplyFn = func(context.Context, bool) (core.UpdateState, error) {
		return core.UpdateState{
			Seq:            5,
			State:          core.UpdateReadyToApply,
			CurrentVersion: "0.0.8",
			Release:        sampleUpdateRelease(),
			Workloads:      core.UpdateActiveWorkloads{ActiveDownloads: 1},
		}, core.ErrUpdateWorkloadActive
	}
	tt.Frame()
	click(t, tt, "更新并重启")
	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for workload conflict callback")
	}
	if !s.updateDialogOpen {
		t.Fatal("workload conflict from ready prompt did not open modal")
	}

	// When idle, clicking "更新并重启" invokes apply directly.
	s.updateApplyFn = func(_ context.Context, confirmStop bool) (core.UpdateState, error) {
		applyCalls = append(applyCalls, confirmStop)
		return core.UpdateState{Seq: 6, State: core.UpdateApplying, CurrentVersion: "0.0.8", Release: sampleUpdateRelease()}, nil
	}
	s.tasks = nil
	s.updateState.Workloads = core.UpdateActiveWorkloads{}
	s.updateDialogOpen = false
	tt.Frame()
	click(t, tt, "更新并重启")
	select {
	case fn := <-uiCh:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ready prompt apply callback")
	}
	if len(applyCalls) != 1 || applyCalls[0] {
		t.Fatalf("expected single idle apply(false) call, got %v", applyCalls)
	}
}
