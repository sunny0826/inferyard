package pages

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func renderDetailCasesBounds(t *testing.T, rows int) (viewport, table ui.Rect) {
	t.Helper()
	cases := make([]core.CaseProgress, rows)
	for i := range cases {
		cases[i] = core.CaseProgress{CaseId: fmt.Sprintf("case-%03d", i+1), State: "done", QualityState: ptr("pass")}
	}
	s := readyState("benchmarks")
	s.detailID = "task-1"
	s.detail = &core.RunTaskDetail{
		Task:     core.RunTask{TaskId: "task-1", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{Done: int64(rows), Total: ptr(int64(max(1, rows))), Cases: cases},
	}
	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()
	label := l10n.TR("detail.cases_table_label")
	var hasViewport, hasTable bool
	for _, item := range tt.AccessAll() {
		if item.Name != label {
			continue
		}
		if item.Role == ui.RoleTable {
			table, hasTable = item.Bounds, true
		} else {
			viewport, hasViewport = item.Bounds, true
		}
	}
	if !hasViewport || !hasTable {
		t.Fatalf("cases viewport/table missing (viewport=%v table=%v): %v", hasViewport, hasTable, tt.Texts())
	}
	return viewport, table
}

func TestDetailCasesTableHeightCompactForFewRows(t *testing.T) {
	viewport, table := renderDetailCasesBounds(t, 2)
	if viewport.H < 80 || viewport.H < table.H {
		t.Fatalf("2-row viewport height %.1f clips table height %.1f", viewport.H, table.H)
	}
	if viewport.H >= detailCasesTableMaxHeight || viewport.H > 160 {
		t.Fatalf("2-row viewport height %.1f should shrink-wrap well below %.0f", viewport.H, detailCasesTableMaxHeight)
	}
}

func TestDetailCasesTableHeightKeepsOneRowWhenEmpty(t *testing.T) {
	theme := ui.LightTheme()
	want := theme.Space(8) + 1 + theme.Space(8)
	if got := detailCasesTableHeight(theme, 0); got != want {
		t.Fatalf("empty table height %.1f, want one row plus header %.1f", got, want)
	}
	viewport, _ := renderDetailCasesBounds(t, 0)
	if viewport.H < want-0.5 {
		t.Fatalf("empty rendered viewport height %.1f collapsed below single-row placeholder %.1f", viewport.H, want)
	}
}

func TestDetailCasesTableHeightCapsLongRuns(t *testing.T) {
	viewport, table := renderDetailCasesBounds(t, 20)
	if viewport.H > detailCasesTableMaxHeight {
		t.Fatalf("20-row viewport height %.1f exceeded %.0f cap", viewport.H, detailCasesTableMaxHeight)
	}
	if viewport.H < detailCasesTableMaxHeight-4 || table.H <= viewport.H {
		t.Fatalf("20-row viewport %.1f / table %.1f did not cap and scroll at %.0f", viewport.H, table.H, detailCasesTableMaxHeight)
	}
}

func TestDetailViewBackButtonClearsDetail(t *testing.T) {
	s := readyState("benchmarks")
	s.detailID = "task-1"
	s.detail = &core.RunTaskDetail{
		Task: core.RunTask{TaskId: "task-1", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{
			Done:  1,
			Total: ptr(int64(1)),
			Cases: []core.CaseProgress{{CaseId: "case-001", State: "done", QualityState: ptr("pass")}},
		},
	}
	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	back, ok := tt.AccessByRole(ui.RoleButton, "← 返回任务")
	if !ok || back.Bounds.X+back.Bounds.W > 205 {
		t.Fatalf("expected back button in left sidebar header (<=205px), got ok=%v bounds=%+v", ok, back.Bounds)
	}
	click(t, tt, "← 返回任务")
	tt.Frame()
	if s.detailID != "" {
		t.Fatalf("back button left detailID = %q, want empty", s.detailID)
	}
}

func TestRunningDashboardMergesCasesAndResourcesInline(t *testing.T) {
	s := readyState("benchmarks")
	s.benchFlowOpen = true
	s.task.stage = "run"
	task := core.RunTask{
		TaskId:  "task-live",
		Kind:    "run",
		State:   "running",
		Bundle:  ptr("zh-core"),
		RunPath: ptr("/tmp/results/task-live"),
	}
	s.tasks = []core.RunTask{task}
	s.runProgress = &core.RunProgress{
		Phase:         ptr("formal"),
		Done:          1,
		Total:         ptr(int64(4)),
		CurrentCaseId: ptr("case-002"),
		Cases: []core.CaseProgress{
			{
				CaseId:       "case-001",
				State:        "completed",
				Category:     ptr("math"),
				QualityState: ptr("pass"),
				DurationMs:   ptr(1240.0),
				Prompt:       ptr("Compute 1+1"),
				Response:     ptr("2"),
			},
		},
	}
	s.activeRunDetail = &core.RunTaskDetail{
		Task:     task,
		Progress: s.runProgress,
	}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.dashboardView(c) })
	}, 1120, 1200)
	tt.Frame()

	for _, want := range []string{"case-001", "math", l10n.TR("chart.resource.title")} {
		if !tt.HasText(want) {
			t.Fatalf("expected merged task view to display %q inline, got %v", want, tt.Texts())
		}
	}
	if got := countProgressBars(tt); got != 1 {
		t.Fatalf("expected exactly 1 progress bar on merged running task page, got %d", got)
	}

	// Selecting the currently running task routes directly to the merged flow view instead of a separate detail page.
	s.benchFlowOpen = false
	s.selectTask("task-live")
	if !s.benchFlowOpen || s.detailID != "" {
		t.Fatalf("expected selectTask on active run to open merged flow view, got benchFlowOpen=%v detailID=%q", s.benchFlowOpen, s.detailID)
	}
}

func TestResumeAfterCompletedRoundShowsLiveRunNotOldCompletion(t *testing.T) {
	s := readyState("benchmarks")
	s.task.stage = "done"
	s.task.report = "/tmp/old-report"
	resumed := core.RunTask{
		TaskId: "task-resumed",
		Kind:   "resume",
		State:  "running",
		Bundle: ptr("zh-core"),
	}
	s.tasks = []core.RunTask{resumed}
	s.runProgress = &core.RunProgress{
		Done:          2,
		Total:         ptr(int64(10)),
		CurrentCaseId: ptr("case-003"),
		Cases:         []core.CaseProgress{{CaseId: "case-001", State: "completed", QualityState: ptr("pass")}},
	}
	s.activeRunDetail = &core.RunTaskDetail{Task: resumed, Progress: s.runProgress}

	s.selectTask("task-resumed")
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.dashboardView(c) })
	}, 1120, 1000)
	tt.Frame()

	if _, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("common.confirm.cancel_run.label")); !ok {
		t.Fatalf("expected resumed active run to show cancel button instead of previous round completion, texts=%v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("dashboard.completion.run_again")); ok {
		t.Fatalf("resumed active run must not render stale completion actions")
	}
	if !tt.HasText("case-001") {
		t.Fatalf("expected resumed active run to show inline case progress, texts=%v", tt.Texts())
	}
}

func TestDetailPaginationAndCaseModalResetAcrossTasks(t *testing.T) {
	s := readyState("benchmarks")
	active := core.RunTask{TaskId: "task-a", Kind: "run", State: "running", Bundle: ptr("zh-core")}
	s.tasks = []core.RunTask{active}
	s.activeRunDetail = &core.RunTaskDetail{Task: active}

	// Open active task A, then open historical task B, flip to page 2, and select row 1.
	s.selectTask("task-a")
	s.selectTask("task-b")
	s.casesTablePage = 2
	s.resourceTablePage = 1
	sel := 1
	s.casesTable.Selected = &sel
	s.detailCaseID = "case-001"

	// Back to Benchmarks list then open active flow via "open_flow" (bypassing selectTask):
	// rendering inlineRunDetailView for task-a must reset pagination, selection, and case modal.
	s.detailID = ""
	s.detailCaseID = ""
	s.benchFlowOpen = true
	s.activeRunDetail = &core.RunTaskDetail{
		Task:     active,
		Progress: &core.RunProgress{Done: 1, Total: ptr(int64(2)), Cases: []core.CaseProgress{{CaseId: "case-001", State: "completed"}}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.dashboardView(c) })
	}, 1120, 1000)
	tt.Frame()
	if s.casesTablePage != 0 || s.resourceTablePage != 0 || *s.casesTable.Selected != -1 || s.detailCaseID != "" {
		t.Fatalf("expected reset when returning via open_flow from task-b to task-a, got casesPage=%d resPage=%d sel=%d caseID=%q",
			s.casesTablePage, s.resourceTablePage, *s.casesTable.Selected, s.detailCaseID)
	}

	// Simulate active task A finishing while a case modal is open: detailCaseID must clear.
	s.detailCaseID = "case-001"
	s.applyRefreshBatch(refreshBatch{
		epoch:           s.settingsSequence(),
		mask:            refreshCore,
		tasks:           []core.RunTask{{TaskId: "task-a", Kind: "run", State: "done", Bundle: ptr("zh-core")}},
		activeRunDetail: nil,
	})
	if s.detailCaseID != "" {
		t.Fatalf("expected detailCaseID cleared after active task ends, got %q", s.detailCaseID)
	}
}
