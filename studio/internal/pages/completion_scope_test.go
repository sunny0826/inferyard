package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// A scoring failure stays in the denominator: three passes and one unscorable
// case read 3 / 4 without a rate, never 100%.
func TestCompletionKeepsUnscorableCasesInDenominator(t *testing.T) {
	s := testState()
	s.task.stage = "done"
	s.task.runs = []string{"/results/round-1"}
	s.history.StandaloneRuns = []core.RunSummary{{
		RunId: "round-1", Path: "/results/round-1", Bundle: ptr("zh-core"), Status: "complete",
		Score: &core.RunScore{Passed: 3, Valid: 4, Unscorable: 1},
	}}
	tt := ui.NewTester(s.completionView, 1120, 1600)
	if !tt.HasText("3 / 4 · 缺测 — 1 题不可评分，分母不缩") {
		t.Fatalf("unscorable case shrank the denominator: %v", tt.Texts())
	}
	for _, label := range tt.Texts() {
		if strings.Contains(label, "100.0%") || strings.Contains(label, "3 / 3") {
			t.Fatalf("scoring failure produced %q", label)
		}
	}
	s.selected = "reports"
	history := ui.NewTester(s.view, 1120, 1600)
	if !history.HasText("3/4 · 含 1 题不可评分") {
		t.Fatalf("history score hid the unscorable case: %v", history.Texts())
	}
}

// Only runs from this task are summarised: a workspace report that also lists
// earlier runs must not add them to this round.
func TestCompletionDoesNotMixInEarlierWorkspaceRuns(t *testing.T) {
	s := testState()
	s.task.stage, s.task.report = "done", "/reports/round-2"
	s.task.runs = []string{"/results/round-2"}
	s.progress.Report = inferyard.ProgressReport{Status: "present", Path: ptr("/reports/round-1"), Runs: []string{"round-1", "round-2"}}
	s.history.StandaloneRuns = []core.RunSummary{
		{RunId: "round-1", Path: "/results/round-1", Bundle: ptr("zh-core"), Status: "complete"},
		{RunId: "round-2", Path: "/results/round-2", Bundle: ptr("zh-svg-pelican"), Status: "complete"},
	}
	tt := ui.NewTester(s.completionView, 1120, 1600)
	if !tt.HasText("本轮共 1 次运行。") || !tt.HasText("zh-svg-pelican") {
		t.Fatalf("round scope is wrong: %v", tt.Texts())
	}
	if tt.HasText("本轮共 2 次运行。") || tt.HasText("zh-core") {
		t.Fatal("an earlier workspace run was attributed to this round")
	}
	if !tt.HasText("查看报告") {
		t.Fatal("this round's report link is missing")
	}
}
