package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestDetailCasesTableHeightCompactForFewRows(t *testing.T) {
	theme := ui.LightTheme()
	const rows = 2
	got := detailCasesTableHeight(theme, rows)
	need := theme.Space(8) + 1 + float32(rows)*theme.Space(8)
	if got < need {
		t.Fatalf("2-row table height %.1f clips its rows, want at least %.1f", got, need)
	}
	if got >= detailCasesTableMaxHeight {
		t.Fatalf("2-row table height %.1f should stay well below the %.0f cap", got, detailCasesTableMaxHeight)
	}
	if got > 160 {
		t.Fatalf("2-row table height %.1f leaves too much blank space", got)
	}
}

func TestDetailCasesTableHeightKeepsOneRowWhenEmpty(t *testing.T) {
	theme := ui.LightTheme()
	if got, want := detailCasesTableHeight(theme, 0), theme.Space(8)+1+theme.Space(8); got != want {
		t.Fatalf("empty table height %.1f, want one row plus header %.1f", got, want)
	}
}

func TestDetailCasesTableHeightCapsLongRuns(t *testing.T) {
	theme := ui.LightTheme()
	for _, rows := range []int{20, 50} {
		if got := detailCasesTableHeight(theme, rows); got != detailCasesTableMaxHeight {
			t.Fatalf("%d-row table height %.1f, want the %.0f cap", rows, got, detailCasesTableMaxHeight)
		}
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

	// Open active task A, then open historical task B and flip to page 2.
	s.selectTask("task-a")
	s.selectTask("task-b")
	s.casesTablePage = 2
	s.resourceTablePage = 1
	s.detailCaseID = "case-001"

	// Returning to active task A must reset pagination and case modal state.
	s.selectTask("task-a")
	if s.casesTablePage != 0 || s.resourceTablePage != 0 || s.detailCaseID != "" {
		t.Fatalf("expected reset when switching from task-b back to task-a, got casesPage=%d resPage=%d caseID=%q",
			s.casesTablePage, s.resourceTablePage, s.detailCaseID)
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
