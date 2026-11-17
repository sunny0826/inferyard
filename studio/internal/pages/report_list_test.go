package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func reportCardState() viewState {
	s := testState()
	s.reports = []core.ReportSummary{
		{
			ReportId: "report-1", Path: "/reports/report-1", ModifiedAt: ptr("2026-03-04T05:06:07Z"),
			RunIds: []string{"run-1"}, RunPaths: []*string{ptr("/results/run-1")},
			RunSummaries: []core.ReportListRun{{RunID: "run-1", Model: "SmolLM2-Q4.gguf", BundleLabel: "zh-core",
				Quality: core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(7)), Denominator: ptr(int64(13))}, Complete: true}}},
		},
		{
			ReportId: "report-2", Path: "/reports/report-2", ModifiedAt: ptr("2026-01-02T03:04:05Z"),
			Unreadable: true, RunIds: []string{}, RunPaths: []*string{},
		},
	}
	return s
}

// A card leads with the recorded model and quality, and states that the time is
// the report file's modification time.
func TestReportCardsLeadWithModelAndQuality(t *testing.T) {
	s := reportCardState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 1400)
	for _, want := range []string{
		"SmolLM2-Q4.gguf",
		"zh-core · 1 次运行 · 报告文件修改时间 2026-03-04T05:06:07Z",
		"SmolLM2-Q4.gguf · zh-core · 答案通过 7 / 13",
		"无法读取；打开后可重试或返回列表。",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("ReportId") || tt.HasText("索引") {
		t.Fatal("card list still leads with table columns", tt.Texts())
	}
}

// Identifiers and paths move into the per-report detail.
func TestReportCardDetailHoldsIdentifiers(t *testing.T) {
	s := reportCardState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 1400)
	click(t, tt, "报告详情")
	if !tt.HasText("报告 ID：report-1") || !tt.HasText("来源运行：run-1") || !tt.HasText("来源路径：run-1") {
		t.Fatalf("detail missing identifiers: %v", tt.Texts())
	}
	if !tt.HasText("报告文件修改时间：2026-03-04T05:06:07Z") {
		t.Fatalf("detail missing file time: %v", tt.Texts())
	}
}

// Multiple runs stay separate; a missing quality count keeps a reason and never
// becomes zero or a percentage.
func TestReportCardsKeepRunsSeparateAndNeverZeroFill(t *testing.T) {
	s := testState()
	s.reports = []core.ReportSummary{{
		ReportId: "multi", Path: "/reports/multi", RunIds: []string{"a", "b"}, RunPaths: []*string{nil, nil},
		RunSummaries: []core.ReportListRun{
			{RunID: "a", Model: "Model-A", BundleLabel: "zh-core", Quality: core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(3)), Denominator: ptr(int64(5))}, Complete: true}},
			{RunID: "b", Model: "Model-B", BundleLabel: "zh-svg-pelican"},
		},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 1200)
	for _, want := range []string{"2 次运行 · 各次分别展示，不合并成绩", "Model-A · zh-core · 答案通过 3 / 5", "Model-B · zh-svg-pelican · 答案通过未记录"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"3 / 5 · 0 / 0", "100%", "合计"} {
		if tt.HasText(unwanted) {
			t.Fatalf("runs merged or zero-filled as %q", unwanted)
		}
	}
}

// The history table keeps five primary columns; identifiers and batch move to
// the selected detail, and missing values say so.
func TestHistoryTableIsCompactAndSaysWhenMissing(t *testing.T) {
	s := testState()
	s.selected = "reports"
	started := "2026-02-03T04:05:06Z"
	s.history.StandaloneRuns = []core.RunSummary{
		{RunId: "run-complete", Path: "/results/run-complete", ModelName: ptr("SmolLM2-Q4.gguf"), Bundle: ptr("zh-core"), Status: "complete", Sealed: true, StartedAt: &started, Score: &core.RunScore{Passed: 7, Valid: 13, Rate: ptr(0.538)}},
		{RunId: "run-empty", Path: "/results/run-empty", Status: "partial"},
	}
	tt := ui.NewTester(s.view, 1120, 1600)
	for _, want := range []string{"模型", "测评题集", "状态", "评分", "开始时间", "SmolLM2-Q4.gguf", "7/13 · 53.8%", "未记录评分", "未记录", "未记录模型"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"RunId", "批次", "证据不足"} {
		if tt.HasText(unwanted) {
			t.Fatalf("history table kept %q", unwanted)
		}
	}
	click(t, tt, "run-empty")
	if !tt.HasText("运行 ID：run-empty · 独立运行") {
		t.Fatalf("selected detail lacks identity: %v", tt.Texts())
	}
}

// An empty list shows an empty state instead of a tall empty table.
func TestReportListWithoutReportsHasNoTable(t *testing.T) {
	s := testState()
	s.loaded, s.reports = true, nil
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 800)
	if !tt.HasText("暂无报告。请新建任务，或从历史运行另做报告。") {
		t.Fatalf("missing empty state: %v", tt.Texts())
	}
	if tt.HasText("报告详情") || tt.HasText("打开报告") {
		t.Fatal("empty list rendered cards")
	}
}
