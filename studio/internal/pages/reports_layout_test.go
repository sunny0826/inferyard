package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestReportsDefaultsToReportListTab(t *testing.T) {
	s := testState()
	s.reports = []core.ReportSummary{{
		ReportId: "one", Path: "/reports/one", RunIds: []string{"a"},
		RunSummaries: []core.ReportListRun{{RunID: "a", Model: "Model-A", BundleLabel: "zh-core", Title: ptr("Model-A")}},
	}}
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "run-1", Path: "/results/run-1"}}
	tt := ui.NewTester(s.reportsView, 1120, 1400)
	for _, want := range []string{"测评报告", "长上下文专项", "运行历史", "打开报告 · one", "从历史运行生成报告"} {
		if !tt.HasText(want) {
			t.Fatalf("default tab missed %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("运行历史表") {
		t.Fatal("history table rendered under the default tab")
	}
	click(t, tt, "运行历史")
	if !tt.HasText("运行历史表") || tt.HasText("打开报告 · one") {
		t.Fatalf("history tab mixed in the report list: %v", tt.Texts())
	}
	click(t, tt, "长上下文专项")
	if !tt.HasText("暂无专项历史任务") || tt.HasText("运行历史表") {
		t.Fatalf("specialty tab rendered another list: %v", tt.Texts())
	}
}

func TestReportBackLinkReturnsToReportListTab(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	_, s.reports, s.reportsError = s.service.Reports()
	if s.reportsError != nil || len(s.reports) == 0 {
		t.Fatal("fixture report is missing", s.reportsError)
	}
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "run-1", Path: "/results/run-1"}}
	s.selected = "reports"
	tt := ui.NewTester(s.view, 1120, 1600)
	click(t, tt, "运行历史")
	if !tt.HasText("运行历史表") {
		t.Fatalf("history tab did not open: %v", tt.Texts())
	}
	s.selected, s.task.stage, s.task.report = "dashboard", "done", "summary-1"
	s.task.runs = []string{"/results/run-1"}
	tt.Frame()
	click(t, tt, "查看报告")
	tt.Frame()
	click(t, tt, "返回报告列表")
	if !tt.HasText("打开报告 · summary-1") || tt.HasText("运行历史表") {
		t.Fatalf("back link kept another tab: %v", tt.Texts())
	}
}

func TestSpeedResultBackLinkReturnsToSpecialtyTab(t *testing.T) {
	s := testState()
	task := &speedTask{id: "speed-1", model: "Preview-Model-Q4.gguf", engine: "preview-engine", stage: speedDone}
	s.speed = &speedWorkspace{history: []*speedTask{task}, current: task}
	s.selected = "dashboard"
	s.busy = true
	tt := ui.NewTester(s.view, 1120, 1600)
	click(t, tt, "查看结果")
	tt.Frame()
	if !tt.HasText("长上下文速度 · 结果") {
		t.Fatalf("result view did not open: %v", tt.Texts())
	}
	click(t, tt, "返回历史任务")
	tt.Frame()
	if !tt.HasText("专项历史任务") || tt.HasText("打开报告 · ") {
		t.Fatalf("back link left the specialty tab: %v", tt.Texts())
	}
}

func TestResetWorkspaceRestoresReportListTab(t *testing.T) {
	s := testState()
	s.reportsTab = reportsTabHistory
	s.openedReport = &reportViewState{reference: "gone", failure: core.Err("not_found")}
	s.resetWorkspace()
	if s.reportsTab != reportsTabList || s.openedReport != nil {
		t.Fatalf("reset kept tab=%d opened=%v", s.reportsTab, s.openedReport != nil)
	}
}

func TestReportsTabSurvivesReframe(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "run-1", Path: "/results/run-1"}}
	tt := ui.NewTester(s.reportsView, 1120, 1200)
	click(t, tt, "运行历史")
	tt.Frame()
	if !tt.HasText("运行历史表") || s.reportsTab != reportsTabHistory {
		t.Fatalf("tab did not survive a frame: %d", s.reportsTab)
	}
}

func TestReportSourcesUseCheckboxesForMultiSelect(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{
		{RunId: "run-1", Path: "/results/run-1"},
		{RunId: "run-2", Path: "/results/run-2"},
	}
	tt := ui.NewTester(s.reportsView, 1120, 1600)
	click(t, tt, "从历史运行生成报告")
	tt.Advance(time.Second)
	click(t, tt, "run-1")
	click(t, tt, "run-2")
	if s.reportChoices.Len() != 2 {
		t.Fatalf("expected 2 checked runs, got %d", s.reportChoices.Len())
	}
	if !tt.HasText("已选 2 / 100") {
		t.Fatalf("count not updated: %v", tt.Texts())
	}
	click(t, tt, "run-1")
	if s.reportChoices.Len() != 1 || !s.reportChoices.Has("/results/run-2") {
		t.Fatalf("unchecking run-1 failed: len=%d", s.reportChoices.Len())
	}
}

func TestHistoryTabNamesRowInteraction(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "run-1", Path: "/results/run-1"}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.historyView(c) })
	}, 1120, 900)
	if tt.HasText("运行 ID：run-1") {
		t.Fatalf("row details appeared before clicking row: %v", tt.Texts())
	}
	click(t, tt, "run-1")
	if !tt.HasText("运行 ID：run-1") {
		t.Fatalf("row details did not appear after clicking row: %v", tt.Texts())
	}
}

func TestHistoryHintOnlyWhenRowsExist(t *testing.T) {
	s := testState()
	s.loaded, s.historyError = true, core.Err("history_unreadable")
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.historyView(c) })
	}, 1120, 900)
	if tt.HasText("运行 ID：") {
		t.Fatalf("row details appeared without rows: %v", tt.Texts())
	}
}

func TestReportPassRateMatchesCompletionView(t *testing.T) {
	complete := core.ReportQuality{
		StudioCounts: core.ReportCounts{Numerator: ptr(int64(7)), Denominator: ptr(int64(13))},
		Complete:     true,
	}
	if got := reportPassRate(complete); got != "7 / 13 · 53.8%" {
		t.Fatalf("complete rate = %q", got)
	}
	incomplete := complete
	incomplete.Complete = false
	if got := reportPassRate(incomplete); got != "7 / 13" {
		t.Fatalf("incomplete rate = %q", got)
	}
	zero := core.ReportQuality{
		StudioCounts: core.ReportCounts{Numerator: ptr(int64(0)), Denominator: ptr(int64(0))},
		Complete:     true,
	}
	if got := reportPassRate(zero); got != "0 / 0" {
		t.Fatalf("zero denominator rate = %q", got)
	}
}

func TestReportSummaryDoesNotRepeatKpiNumbers(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportView(c) }) }, 1120, 3000)
	frame := strings.Join(tt.Texts(), "\n")
	if strings.Contains(frame, "13 道评分题中通过 7 道") || strings.Contains(frame, "11 / 13 个请求完成") {
		t.Fatalf("summary repeated the KPI numbers: %v", tt.Texts())
	}
	if !tt.HasText("7 / 13 · 53.8%") {
		t.Fatalf("pass card lacks its rate: %v", tt.Texts())
	}
}

func TestReportSummaryGridFollowsViewport(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	wide := ui.NewTester(func(c *ui.Context) { s.reportView(c) }, 1200, 3000)
	throughput, ok := wide.Find("吞吐")
	if !ok {
		t.Fatal("throughput card is missing")
	}
	intervals, ok := wide.Find("输出内容块到达间隔")
	if !ok {
		t.Fatal("interval card is missing")
	}
	if throughput.Y != intervals.Y || throughput.X == intervals.X {
		t.Fatalf("wide layout did not pair the cards: %v %v", throughput, intervals)
	}
	narrow := ui.NewTester(func(c *ui.Context) { s.reportView(c) }, 900, 3000)
	narrowThroughput, _ := narrow.Find("吞吐")
	narrowIntervals, _ := narrow.Find("输出内容块到达间隔")
	if narrowThroughput.Y == narrowIntervals.Y {
		t.Fatal("compact layout put the cards side by side")
	}
}

func TestReportDetailBackLinkAndCardDedup(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	tt := ui.NewTester(func(c *ui.Context) { s.reportView(c) }, 1120, 3000)
	back, ok := tt.Find("返回报告列表")
	if !ok {
		t.Fatal("back link is missing")
	}
	title, ok := tt.Find("Fixture Model Q4")
	if !ok || back.Y >= title.Y {
		t.Fatalf("back link is not above the title: %v %v", back, title)
	}

	card := core.ReportSummary{
		ReportId: "single", Path: "/reports/single", RunIds: []string{"a"},
		RunSummaries: []core.ReportListRun{{
			RunID: "a", Model: "Model-A", BundleLabel: "zh-core", Title: ptr("Model-A"),
			Quality: core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(3)), Denominator: ptr(int64(5))}, Complete: true},
		}},
	}
	list := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { reportCard(c, card, func() {}) }) }, 1120, 600)
	for _, text := range list.Texts() {
		if text == "Model-A · zh-core · 答案通过 3 / 5" {
			t.Fatalf("single-run line repeated the heading and subheading: %v", list.Texts())
		}
	}
	if !list.HasText("答案通过 3 / 5") {
		t.Fatalf("single-run line lost its quality: %v", list.Texts())
	}
}
