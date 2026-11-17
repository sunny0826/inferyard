package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// The task page's cancel runs the whole round through its own service
// boundary; dismissing the prompt must not reach that boundary, and confirming
// must reach it once per non-terminal run.
func TestWholeRoundCancelBoundary(t *testing.T) {
	s := testState()
	s.selected, s.task.stage = "dashboard", "run"
	s.ctx = context.Background()
	s.tasks = []core.RunTask{
		{TaskId: "a", Kind: "run", State: "running", Bundle: ptr("zh-core")},
		{TaskId: "b", Kind: "run", State: "starting", Queued: true, Bundle: ptr("zh-svg-pelican")},
	}
	cancelled := 0
	s.updateWindow = func(fn func()) { fn() }
	s.cancelRound = func(id string) (core.RunTask, *core.ApiError) {
		cancelled++
		for i := range s.tasks {
			if s.tasks[i].TaskId == id {
				s.tasks[i].State = "cancelled"
			}
		}
		return s.tasks[0], nil
	}
	tt := ui.NewTester(s.runView, 1120, 900)
	click(t, tt, "取消运行")
	tt.Frame()
	tt.Click("返回")
	tt.Frame()
	if cancelled != 0 {
		t.Fatalf("dismiss reached the whole-round boundary %d times", cancelled)
	}

	click(t, tt, "取消运行")
	tt.Frame()
	tt.Click("确认取消运行")
	tt.Frame()
	if cancelled != 2 {
		t.Fatalf("confirm reached the boundary %d times, want 2", cancelled)
	}
}

// When the service boundary is shared, returning must not cancel even when the
// page later re-renders with a fresh queue.
func TestWholeRoundDismissLeavesQueueUntouched(t *testing.T) {
	s := testState()
	s.selected, s.task.stage = "dashboard", "run"
	s.ctx = context.Background()
	s.tasks = []core.RunTask{{TaskId: "only", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.updateWindow = func(fn func()) { fn() }
	s.cancelRound = func(string) (core.RunTask, *core.ApiError) {
		s.tasks[0].State = "cancelled"
		return s.tasks[0], nil
	}
	tt := ui.NewTester(s.leftNav, 240, 900)
	click(t, tt, "新任务")
	tt.Frame()
	tt.Click("返回")
	tt.Frame()
	if s.tasks[0].State != "running" || s.newTaskPending {
		t.Fatalf("dismiss changed the queue or the pending state: %+v", s.tasks)
	}
}
