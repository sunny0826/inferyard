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
