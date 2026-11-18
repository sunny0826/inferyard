package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// A prompt that opened before a state change must not act afterwards: the
// target is re-read when the confirm click arrives, the stale request is
// dropped and nothing is dispatched.
func TestConfirmationDropsStaleRunTarget(t *testing.T) {
	s := runningState()
	s.task.stage = "run"
	s.detail = &core.RunTaskDetail{Task: s.tasks[0]}
	tt := ui.NewTester(s.detailView, 1120, 1200)
	click(t, tt, "取消任务")
	if !tt.HasText("确认取消任务") {
		t.Fatalf("prompt missing: %v", tt.Texts())
	}
	// The run finishes while the prompt is open. The detail copy still shows the
	// old state, so the confirm control stays clickable in this frame.
	s.tasks[0].State = "done"
	tt.Frame()
	click(t, tt, "确认取消任务")
	tt.Frame()
	if s.task.stage != "run" || s.newTaskPending {
		t.Fatalf("stale confirmation dispatched a cancel: stage=%q pending=%v", s.task.stage, s.newTaskPending)
	}
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("stale request stayed pending: %+v", s.pendingDestructive)
	}
}

// The same re-read covers downloads: a task that already finished is not
// cancelled again.
func TestConfirmationDropsFinishedDownload(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	task := core.DownloadTask{TaskId: "model-1", Kind: "model", State: "downloading", OutName: "model-1", OutDir: "/models/model-1"}
	s.downloads = []core.DownloadTask{task}
	tt := ui.NewTester(s.downloadsView, 1120, 1200)
	click(t, tt, "取消下载")
	if !tt.HasText("确认取消下载") {
		t.Fatalf("prompt missing: %v", tt.Texts())
	}
	s.downloads[0].State = "done"
	tt.Frame()
	click(t, tt, "确认取消下载")
	tt.Frame()
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("stale download request stayed pending: %+v", s.pendingDestructive)
	}
	if s.failure == nil && tt.HasText("已请求取消下载。") {
		t.Fatal("finished download was cancelled")
	}
}

// A finished orphan record is not stopped after the record disappears.
func TestConfirmationDropsResolvedOrphan(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "orphaned", Error: core.Err("engine_owner_invalid")}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.stopOrphanButton(c) }) }, 800, 400)
	click(t, tt, "停止遗留服务")
	if !tt.HasText("确认停止遗留服务") {
		t.Fatalf("prompt missing: %v", tt.Texts())
	}
	s.engineState = core.EngineState{State: "stopped"}
	tt.Frame()
	click(t, tt, "确认停止遗留服务")
	tt.Frame()
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("resolved orphan request stayed pending: %+v", s.pendingDestructive)
	}
}

// Dismissing clears the request and leaves the work untouched.
func TestConfirmationReturnLeavesWorkRunning(t *testing.T) {
	s := runningState()
	s.task.stage = "run"
	tt := ui.NewTester(s.runView, 1120, 900)
	click(t, tt, "取消运行")
	click(t, tt, "返回")
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("dismiss left a pending request: %+v", s.pendingDestructive)
	}
	if s.task.stage != "run" || s.running() == nil || s.newTaskPending {
		t.Fatal("dismissing changed the running work")
	}
}
