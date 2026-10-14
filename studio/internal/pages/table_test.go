package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestOnevraTableSelectionTracksKeyAcrossReorder(t *testing.T) {
	s := testState()
	a := core.RunTask{TaskId: "a", Bundle: ptr("bundle-a")}
	b := core.RunTask{TaskId: "b", Bundle: ptr("bundle-b")}
	s.tasks = []core.RunTask{a, b}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.tasksView(c) }) }, 800, 600)
	click(t, tt, "bundle-b")
	s.tasks = []core.RunTask{b, a}
	tt.Frame()
	if selectedRow(&s.tasksTable, 2) != 0 || !tt.HasText("b") {
		t.Fatal("selection followed index instead of task key")
	}
	tt.Key(0, ui.KeyEnd)
	if selectedRow(&s.tasksTable, 2) != 1 || !tt.HasText("a") {
		t.Fatal("End did not select last row")
	}
	tt.Key(0, ui.KeyHome)
	if selectedRow(&s.tasksTable, 2) != 0 {
		t.Fatal("Home did not select first row")
	}
	s.tasks = []core.RunTask{a}
	tt.Frame()
	if selectedRow(&s.tasksTable, 1) != -1 {
		t.Fatal("removed selected key selected a different task")
	}
}

func TestOnevraTableClipsWideContent(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "fixture", Path: "/fixture"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.historyView(c) }) }, 800, 600)
	nodes := modelUINodes(tt, "label", "运行历史表")
	if len(nodes) != 2 {
		t.Fatal("expected scroll boundary and table", len(nodes))
	}
	outer, inner := nodes[0], nodes[1]
	if outer.FieldByName("w").Float() > 800 || inner.FieldByName("w").Float() <= 800 {
		t.Fatal("wide content must remain inside bounded scroller")
	}
}
