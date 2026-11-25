package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestFormatElapsedDurationFloorSemantics(t *testing.T) {
	cases := []struct {
		dur    time.Duration
		wantEn string
		wantZh string
	}{
		{0, "< 1s", "不足 1 秒"},
		{950 * time.Millisecond, "< 1s", "不足 1 秒"},
		{1900 * time.Millisecond, "1s", "1 秒"},
		{65 * time.Second, "1m 5s", "1 分 5 秒"},
		{2 * time.Minute, "2m", "2 分"},
		{3660 * time.Second, "1h 1m", "1 小时 1 分"},
	}
	for _, tc := range cases {
		if got := formatElapsedDurationFor(l10n.LanguageEn, tc.dur); got != tc.wantEn {
			t.Fatalf("en duration(%v) = %q, want %q", tc.dur, got, tc.wantEn)
		}
		if got := formatElapsedDurationFor(l10n.LanguageZhHans, tc.dur); got != tc.wantZh {
			t.Fatalf("zh duration(%v) = %q, want %q", tc.dur, got, tc.wantZh)
		}
	}
}

func TestBenchmarkElapsedUsesBackendStartedAtAcrossNavigation(t *testing.T) {
	s := readyState("benchmarks")
	var baseNow time.Time
	probe := ui.NewTester(func(c *ui.Context) { baseNow = c.Now() }, 400, 300)
	probe.Frame()

	startedAt := baseNow.Add(-125 * time.Second).UTC().Format(time.RFC3339Nano)
	finishedStartedAt := baseNow.Add(-10 * time.Minute).UTC().Format(time.RFC3339)

	runTask := core.RunTask{
		TaskId:    "task-live-1",
		Kind:      "run",
		State:     "running",
		Bundle:    ptr(inferyard.DefaultBundlePelican),
		RunPath:   ptr("/w/results/run-live-1"),
		StartedAt: ptr(startedAt),
	}
	doneTask := core.RunTask{
		TaskId:     "task-done-1",
		Kind:       "run",
		State:      "done",
		Bundle:     ptr(inferyard.DefaultBundleCore),
		RunPath:    ptr("/w/results/run-done-1"),
		StartedAt:  ptr(finishedStartedAt),
		FinishedAt: ptr(baseNow.Add(-5 * time.Minute).UTC().Format(time.RFC3339)),
	}
	s.tasks = []core.RunTask{runTask, doneTask}
	// Single-case bundle (0 / 1) must immediately show deterministic elapsed time instead of sticking on an estimate.
	s.runProgress = &core.RunProgress{
		Phase:         ptr("formal"),
		Done:          0,
		Total:         ptr(int64(1)),
		CurrentCaseId: ptr("svg-pelican-01"),
	}
	s.detail = &core.RunTaskDetail{
		Task:     runTask,
		Progress: s.runProgress,
	}

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	wantInitialElapsed := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 125*time.Second))
	wantZeroProgress := l10n.TRf("dashboard.run.cases_progress", "done", 0, "total", 1)

	// 1. Default Benchmarks list view shows both live case progress and elapsed time for the active task,
	// while keeping the static start timestamp on terminal tasks.
	joined := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(joined, wantZeroProgress) || !strings.Contains(joined, wantInitialElapsed) {
		t.Fatalf("expected benchmarks list row to include %q and %q, got:\n%s", wantZeroProgress, wantInitialElapsed, joined)
	}
	if !strings.Contains(joined, formatTimestamp(finishedStartedAt)) {
		t.Fatalf("expected terminal task row to preserve formatted start timestamp, got:\n%s", joined)
	}

	// 2. Navigate away to Services, advance 6 seconds, then open Benchmark flow view and Detail view:
	// elapsed time must advance to 2m 11s (131s) rather than resetting to 0 on remount.
	s.navigateTo("services", "")
	tt.Advance(6 * time.Second)

	s.openBenchmarksFlow()
	tt.Frame()
	wantAfterNav := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 131*time.Second))
	flowTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(flowTexts, wantAfterNav) {
		t.Fatalf("expected benchmark flow card to preserve backend elapsed %q after page switch, got:\n%s", wantAfterNav, flowTexts)
	}

	s.detailID = runTask.TaskId
	tt.Advance(2 * time.Second)
	wantInDetail := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 133*time.Second))
	detailTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(detailTexts, wantInDetail) {
		t.Fatalf("expected run detail view to show %q without resetting timer, got:\n%s", wantInDetail, detailTexts)
	}
}

func TestContextSpeedMeasuringShowsBackendElapsedTime(t *testing.T) {
	s := readyState("benchmarks")
	var baseNow time.Time
	probe := ui.NewTester(func(c *ui.Context) { baseNow = c.Now() }, 400, 300)
	probe.Frame()

	startedAt := baseNow.Add(-48 * time.Second).UTC().Format(time.RFC3339Nano)
	speedRun := core.RunTask{
		TaskId:    "speed-1",
		Kind:      "context_speed",
		State:     "running",
		StartedAt: ptr(startedAt),
		ContextSpeed: &core.ContextSpeedTask{
			Model:     "Qwen3-8B.gguf",
			Engine:    "llama-server",
			Context:   inferyard.ContextSpeed128K,
			Stage:     "measuring",
			Completed: 0,
			Plan:      &core.SpeedPlanRef{MaxWallSeconds: 600},
			Batch:     &core.SpeedBatchRef{Path: "/w/results/speed-1"},
		},
	}
	s.speed = &speedWorkspace{current: speedFromTask(speedRun)}

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()

	want48s := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 48*time.Second))
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), want48s) {
		t.Fatalf("expected context speed measuring stage at 0/5 to show %q, got:\n%s", want48s, strings.Join(tt.Texts(), "\n"))
	}

	tt.Advance(3 * time.Second)
	want51s := l10n.TRf("common.elapsed.running", "duration", formatElapsedDurationFor(l10n.LanguageZhHans, 51*time.Second))
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), want51s) {
		t.Fatalf("expected context speed elapsed time to advance to %q, got:\n%s", want51s, strings.Join(tt.Texts(), "\n"))
	}
}
