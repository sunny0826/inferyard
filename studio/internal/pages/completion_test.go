package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The completion step summarises the recorded score and offers the next run.
func TestCompletionSummarisesRunsAndOffersNextTask(t *testing.T) {
	s := testState()
	s.task.stage, s.task.report = "done", "/reports/round"
	s.task.runs = []string{"/results/round-1"}
	s.history.StandaloneRuns = []core.RunSummary{{
		RunId: "round-1", Path: "/results/round-1", Bundle: ptr("zh-core"), ModelName: ptr("Fixture Model"),
		Status: "complete", Score: &core.RunScore{Passed: 3, Valid: 4, Rate: ptr(0.75)},
		StartedAt: ptr("2026-11-17T01:00:00Z"), FinishedAt: ptr("2026-11-17T01:02:05Z"),
	}}
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, want := range []string{"Fixture Model · zh-core", "3 / 4 · 75.0%", "2 分 5 秒", "查看报告", "再次测评"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	click(t, tt, "再次测评")
	if s.task.stage != "prepare_candidate" {
		t.Fatal("completion entry did not start the next task", s.task.stage)
	}
}

// Missing score or times are labelled, never rendered as zero.
func TestCompletionKeepsMissingScoresMissing(t *testing.T) {
	s := testState()
	s.task.stage = "done"
	s.task.runs = []string{"/results/round-1"}
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "round-1", Path: "/results/round-1", Bundle: ptr("zh-core"), Status: "partial"}}
	tt := ui.NewTester(s.completionView, 1120, 1600)
	if !tt.HasText("缺测 — 未记录分数") || !tt.HasText("缺测 — 未记录开始或结束时间") {
		t.Fatal(tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "0.0%") || strings.Contains(text, "0 秒") {
			t.Fatalf("missing measurement rendered as zero: %q", text)
		}
	}
}

func TestCompletionLinksWorkspaceReportWithoutTaskRuns(t *testing.T) {
	s := testState()
	s.task = benchmarkTask{stage: "done"}
	s.progress.DefaultScopeComplete = true
	s.progress.Report = inferyard.ProgressReport{Status: "present", Path: ptr("workspace-report"), Runs: []string{"round-1"}}
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "round-1", Path: "/results/round-1", Bundle: ptr("zh-core"), Status: "complete"}}
	tt := ui.NewTester(s.completionView, 1120, 1600)
	if !tt.HasText("查看报告") || !tt.HasText("zh-core") {
		t.Fatal("workspace report or its run summary is missing", tt.Texts())
	}
}
