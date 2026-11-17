package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func runningState() *viewState {
	s := testState()
	s.selected = "dashboard"
	s.tasks = []core.RunTask{{TaskId: "active", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.runProgress = &core.RunProgress{Done: 1, Total: ptr(int64(4))}
	return &s
}

// A running benchmark must not be discarded by one click in the sidebar.
func TestNewTaskConfirmsBeforeCancellingRunningWork(t *testing.T) {
	s := runningState()
	tt := ui.NewTester(s.view, 1120, 1600)
	click(t, tt, "新任务")
	if s.task.stage == "prepare_candidate" || s.newTaskPending {
		t.Fatal("sidebar started a new task without confirmation")
	}
	if !tt.HasText("确认开始新任务") {
		t.Fatal("confirmation prompt missing", tt.Texts())
	}
	click(t, tt, "返回")
	if s.task.stage == "prepare_candidate" || s.pendingDestructive != (destructiveRequest{}) {
		t.Fatal("dismissing the prompt still started a task")
	}
	click(t, tt, "新任务")
	click(t, tt, "确认开始新任务")
	if s.task.stage != "prepare_candidate" || s.running() == nil {
		t.Fatal("confirmed new task did not switch to preparation", s.task.stage)
	}
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatal("confirmation state survived the action")
	}
}

// An idle preparation screen keeps the single-click entry.
func TestNewTaskStaysDirectWhenIdle(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.view, 1120, 1200)
	click(t, tt, "新任务")
	if s.task.stage != "prepare_candidate" {
		t.Fatal("idle new task required an extra click", s.task.stage)
	}
	if tt.HasText("确认开始新任务") {
		t.Fatal("idle new task asked for confirmation")
	}
}

func TestCancelRunRequiresConfirmation(t *testing.T) {
	s := runningState()
	s.task.stage = "run"
	tt := ui.NewTester(s.runView, 1120, 900)
	click(t, tt, "取消运行")
	if s.task.stage != "run" || !tt.HasText("确认取消运行") {
		t.Fatal("cancel ran on the first click or the prompt is missing", tt.Texts())
	}
	if !tt.HasText("已写入结果目录的进度保留") {
		t.Fatal("prompt does not state what is kept", tt.Texts())
	}
	click(t, tt, "确认取消运行")
	if s.task.stage == "run" || s.pendingDestructive != (destructiveRequest{}) {
		t.Fatal("confirmed cancel did not leave the running step", s.task.stage)
	}
}

func TestCancelTaskAndStopServiceRequireConfirmation(t *testing.T) {
	s := runningState()
	s.task.stage = "run"
	s.detail = &core.RunTaskDetail{Task: s.tasks[0]}
	dt := ui.NewTester(s.detailView, 1120, 1200)
	click(t, dt, "取消任务")
	if !dt.HasText("确认取消任务") || s.task.stage != "run" {
		t.Fatal("detail cancel ran without confirmation", dt.Texts())
	}
	click(t, dt, "确认取消任务")
	if s.task.stage == "run" {
		t.Fatal("confirmed detail cancel did not leave the running step")
	}

	stop := testState()
	stop.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
	stop.task.stage = "bind"
	st := ui.NewTester(stop.serviceView, 1120, 1200)
	click(t, st, "停止服务")
	if !st.HasText("确认停止服务") {
		t.Fatal("stop prompt missing", st.Texts())
	}
}

func TestCancelDownloadRequiresConfirmation(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{TaskId: "dl", Kind: "model", OutName: "model", State: "downloading"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1000)
	click(t, tt, "取消下载")
	if s.downloads[0].State != "downloading" {
		t.Fatal("download cancel ran on the first click")
	}
	if !tt.HasText("确认取消下载") || !tt.HasText("已写入的输出目录保留") {
		t.Fatal("download prompt missing or vague", tt.Texts())
	}
	click(t, tt, "返回")
	if s.pendingDestructive != (destructiveRequest{}) || s.downloads[0].State != "downloading" {
		t.Fatal("dismissing the download prompt cancelled it")
	}
}
