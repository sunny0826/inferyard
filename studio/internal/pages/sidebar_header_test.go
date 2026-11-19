package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// sidebarHeaderState mounts the sidebar entry and the header status bar the
// production window and the preview share.
func sidebarHeaderState(s *viewState) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
			ui.Column(c).Grow(1).Children(func() { s.cornerStatus(c) })
		})
	}, 1120, 700)
}

// taskLabelBox is the "新任务" text box; its x position shows whether the
// entry is centered in the full-width sidebar column or shrunk to the start.
func taskLabelBox(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	r, ok := tt.Find("新任务")
	if !ok {
		t.Fatalf("sidebar entry missing: %v", tt.Texts())
	}
	return r
}

func TestSidebarNewTaskKeepsOneGeometry(t *testing.T) {
	s := &viewState{selected: "reports", loaded: true, denied: map[string]bool{}}
	tt := sidebarHeaderState(s)
	idle := taskLabelBox(t, tt)

	s.tasks = []core.RunTask{{TaskId: "run", State: "running"}}
	tt.Frame()
	running := taskLabelBox(t, tt)
	if running.X != idle.X || running.W != idle.W {
		t.Fatalf("running entry moved: idle %+v running %+v", idle, running)
	}

	if err := tt.Click("新任务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("确认开始新任务") {
		t.Fatalf("in-flight entry did not ask to confirm: %v", tt.Texts())
	}
	prompt := taskLabelBox(t, tt)
	if prompt.X != idle.X || prompt.W != idle.W {
		t.Fatalf("entry shrank when the prompt opened: idle %+v prompt %+v", idle, prompt)
	}

	s.tasks = []core.RunTask{{TaskId: "run", State: "failed"}}
	s.pendingDestructive = destructiveRequest{}
	tt.Frame()
	failed := taskLabelBox(t, tt)
	if failed.X != idle.X || failed.W != idle.W {
		t.Fatalf("failed entry moved: idle %+v failed %+v", idle, failed)
	}
}

func TestSidebarNewTaskIdleSetupStaysDisabled(t *testing.T) {
	s := &viewState{selected: "dashboard", loaded: true, denied: map[string]bool{},
		progress: &inferyard.WorkspaceProgress{NextAction: "prepare_candidate"}}
	tt := sidebarHeaderState(s)
	if err := tt.Click("新任务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.newTaskPending || tt.HasText("确认开始新任务") {
		t.Fatalf("idle setup duplicated its own start action: %v", tt.Texts())
	}
}

func TestHeaderEngineStatusIsClickOnly(t *testing.T) {
	s := &viewState{selected: "reports", loaded: true, denied: map[string]bool{},
		engineState: core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}}
	tt := sidebarHeaderState(s)
	if !tt.HasText("引擎 · 运行中") {
		t.Fatalf("engine status lacks its subject: %v", tt.Texts())
	}
	r, ok := tt.Find("引擎 · 运行中")
	if !ok {
		t.Fatal("engine status control missing")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if tt.HasText("停止引擎") {
		t.Fatalf("hover opened the status panel: %v", tt.Texts())
	}
	if err := tt.Click("引擎 · 运行中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("停止引擎") {
		t.Fatalf("click did not open the status panel: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("停止引擎") {
		t.Fatalf("Escape left the panel open: %v", tt.Texts())
	}
}

func TestHeaderQueueAndDownloadPanelsNavigate(t *testing.T) {
	s := &viewState{selected: "reports", loaded: true, denied: map[string]bool{},
		tasks:     []core.RunTask{{TaskId: "run", State: "running"}},
		downloads: []core.DownloadTask{{TaskId: "download", State: "downloading"}}}
	tt := sidebarHeaderState(s)

	if err := tt.Click("任务 · 执行 1 · 等待 0"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("任务队列") {
		t.Fatalf("queue panel did not open: %v", tt.Texts())
	}
	if err := tt.Click("查看当前任务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.selected != "dashboard" || s.detailID != "" {
		t.Fatalf("queue action did not open the task page: %q %q", s.selected, s.detailID)
	}
	if tt.HasText("任务队列") {
		t.Fatal("queue action left its panel open")
	}

	if err := tt.Click("下载中 1 项"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("前往下载页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.selected != "downloads" {
		t.Fatalf("download action did not open the downloads page: %q", s.selected)
	}
}
