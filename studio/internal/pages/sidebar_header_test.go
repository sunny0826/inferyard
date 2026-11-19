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

// readyState reports a ready workspace, so only the state under test decides
// whether the sidebar entry is disabled.
func readyState(selected string) *viewState {
	return &viewState{selected: selected, loaded: true, denied: map[string]bool{},
		ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}, Workspace: core.DirectoryReadiness{Initialized: true},
			Results: core.DirectoryReadiness{Exists: true}, Models: core.DirectoryReadiness{Exists: true}}}
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

// settingsBox pins the navigation stack: no entry above it may shift.
func settingsBox(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	r, ok := tt.Find("设置")
	if !ok {
		t.Fatalf("sidebar navigation missing: %v", tt.Texts())
	}
	return r
}

func TestSidebarNewTaskKeepsOneGeometry(t *testing.T) {
	s := readyState("reports")
	tt := sidebarHeaderState(s)
	idle, idleSettings := taskLabelBox(t, tt), settingsBox(t, tt)

	for _, tasks := range [][]core.RunTask{
		{{TaskId: "run", State: "running"}},
		{{TaskId: "run", State: "failed"}},
	} {
		s.tasks = tasks
		s.task.stage = "bind"
		tt.Frame()
		entry, settings := taskLabelBox(t, tt), settingsBox(t, tt)
		if entry.X != idle.X || entry.W != idle.W {
			t.Fatalf("entry changed geometry in %s: idle %+v now %+v", tasks[0].State, idle, entry)
		}
		if settings != idleSettings {
			t.Fatalf("navigation moved in %s: idle %+v now %+v", tasks[0].State, idleSettings, settings)
		}
		if tt.HasText("确认开始新任务") {
			t.Fatalf("sidebar grew an inline confirmation in %s: %v", tasks[0].State, tt.Texts())
		}
	}
}

// A disabled entry explains itself and never resets the round behind the user's
// back: environment setup, an active run and the idle setup page all disable it.
func TestSidebarNewTaskDisabledStatesDoNotReset(t *testing.T) {
	firstRun := readyState("dashboard")
	firstRun.ready = core.Readiness{}
	setup := readyState("dashboard")
	setup.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate"}
	running := readyState("reports")
	running.tasks = []core.RunTask{{TaskId: "run", State: "running"}}

	for name, s := range map[string]*viewState{"first run": firstRun, "idle setup": setup, "running": running} {
		// The reset boundary runs in a goroutine, so it records a hit instead of
		// failing there; the assertion reads it once the frame settles.
		resets := 0
		s.cancelRound = func(string) (core.RunTask, *core.ApiError) {
			resets++
			return core.RunTask{TaskId: "cancelled", State: "cancelled"}, nil
		}
		s.runTasks = func() []core.RunTask { return nil }
		before := s.selected
		tt := sidebarHeaderState(s)
		if err := tt.Click("新任务"); err != nil {
			t.Fatal(name, err)
		}
		tt.Frame()
		if resets != 0 || s.newTaskPending || s.selected != before || tt.HasText("确认开始新任务") {
			t.Fatalf("%s: disabled entry acted: resets=%d pending=%v page=%q %v", name, resets, s.newTaskPending, s.selected, tt.Texts())
		}
	}
}

// A prompt armed on one page must not survive navigation. The header popover
// jump is the case a user hits: open the queue panel, then jump to the task page.
func TestArmedPromptClearsOnNavigation(t *testing.T) {
	s := readyState("reports")
	s.tasks = []core.RunTask{{TaskId: "run", State: "running", Bundle: ptr("zh-core")}}
	s.pendingDestructive = destructiveRequest{kind: string(destructiveCancelRun), id: "run"}
	tt := sidebarHeaderState(s)
	tt.Frame()

	if err := tt.Click("任务 · 执行 1 · 等待 0"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("查看当前任务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("queue jump kept an armed prompt: %+v", s.pendingDestructive)
	}
	if s.selected != "dashboard" {
		t.Fatalf("queue jump landed on %q", s.selected)
	}
}

// Closing the engine panel must disarm the stop confirmation it holds: the next
// opening starts from the panel's entry, never from a stale confirm button.
func TestEnginePanelCloseDisarmsStopPrompt(t *testing.T) {
	s := readyState("reports")
	s.engineState = core.EngineState{State: "running", Pid: 42, StartTicks: 9, Endpoint: "http://127.0.0.1:48857"}
	tt := sidebarHeaderState(s)

	if err := tt.Click("引擎 · 运行中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("停止引擎"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("确认停止引擎") {
		t.Fatalf("stop prompt did not open: %v", tt.Texts())
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("Escape left the stop prompt armed: %+v", s.pendingDestructive)
	}

	if err := tt.Click("引擎 · 运行中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("确认停止引擎") {
		t.Fatalf("reopened panel showed a stale confirmation: %v", tt.Texts())
	}
	if !tt.HasText("停止引擎") {
		t.Fatalf("reopened panel lost its stop entry: %v", tt.Texts())
	}
}

func TestHeaderEngineStatusIsClickOnly(t *testing.T) {
	s := readyState("reports")
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
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
}

func TestHeaderQueueAndDownloadPanelsNavigate(t *testing.T) {
	s := readyState("reports")
	s.tasks = []core.RunTask{{TaskId: "run", State: "running"}}
	s.downloads = []core.DownloadTask{{TaskId: "download", State: "downloading"}}
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
