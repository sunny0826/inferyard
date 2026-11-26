package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
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
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { s.detailView(c) })
	}, 1120, 900)
	tt.Frame()

	click(t, tt, "← 返回任务")
	tt.Frame()
	if s.detailID != "" {
		t.Fatalf("back button left detailID = %q, want empty", s.detailID)
	}
}
