package pages

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// roundHarness stands in for the service boundary of a whole-round reset. The
// page's own state is written only from the UI callback, as production does, so
// the test observes the reset instead of racing its goroutine.
type roundHarness struct {
	mu        sync.Mutex
	queue     []core.RunTask
	cancelled []string
	ui        chan func()
}

func newRoundHarness(tasks ...core.RunTask) *roundHarness {
	return &roundHarness{queue: tasks, ui: make(chan func())}
}

// tasks returns a copy of the queue, safe for the reset goroutine.
func (h *roundHarness) tasks() []core.RunTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]core.RunTask(nil), h.queue...)
}

// cancel marks the run terminal behind the page and records the call.
func (h *roundHarness) cancel(id string) (core.RunTask, *core.ApiError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelled = append(h.cancelled, id)
	for i := range h.queue {
		if h.queue[i].TaskId == id {
			h.queue[i].State = "cancelled"
		}
	}
	return h.queue[0], nil
}

func (h *roundHarness) cancelledIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.cancelled...)
}

// install wires the page to the harness; update runs the callback on the test
// goroutine, and the first completion closes done.
func (h *roundHarness) install(s *viewState) {
	s.runTasks = h.tasks
	s.cancelRound = h.cancel
	s.updateWindow = func(fn func()) {
		select {
		case h.ui <- fn:
		case <-s.ctx.Done():
		}
	}
}

// runUI executes queued callbacks until settled reports the reset finished.
// The brief quiet period after each frame replaces the window loop's next
// frame: the reset goroutine may still be enqueueing the following callback.
func (h *roundHarness) runUI(t *testing.T, settled func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case fn := <-h.ui:
			fn()
		case <-time.After(20 * time.Millisecond):
			if settled() {
				return
			}
		case <-deadline:
			t.Fatal("reset never completed on the UI thread")
		}
	}
}

// The task page's cancel runs the whole round through its own service
// boundary; dismissing the prompt must not reach that boundary, and confirming
// must reach it once per non-terminal run.
func TestWholeRoundCancelBoundary(t *testing.T) {
	h := newRoundHarness(
		core.RunTask{TaskId: "a", Kind: "run", State: "running", Bundle: ptr("zh-core")},
		core.RunTask{TaskId: "b", Kind: "run", State: "starting", Queued: true, Bundle: ptr("zh-svg-pelican")},
	)
	s := harnessState(h)
	tt := ui.NewTester(s.runView, 1120, 900)
	click(t, tt, "取消运行")
	tt.Frame()
	tt.Click("返回")
	tt.Frame()
	// newBenchmark marks the page pending before it starts any goroutine, so an
	// untouched flag and recorder prove the dismiss spawned no reset.
	if s.newTaskPending || s.task.stage != "run" || s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("dismiss changed the page: pending=%v stage=%q", s.newTaskPending, s.task.stage)
	}
	if got := h.cancelledIDs(); len(got) != 0 {
		t.Fatalf("dismiss reached the whole-round boundary %d times", len(got))
	}
	select {
	case fn := <-h.ui:
		fn()
		t.Fatal("dismiss started an asynchronous reset")
	default:
	}

	confirmRunCancel(t, tt)
	h.runUI(t, func() bool { return resetFinished(&s) })
	assertRoundCancelled(t, h, &s)
}

// confirmRunCancel presses the run-cancel entry and its confirmation.
func confirmRunCancel(t *testing.T, tt *ui.Tester) {
	t.Helper()
	click(t, tt, "取消运行")
	tt.Frame()
	tt.Click("确认取消运行")
	tt.Frame()
}

// assertRoundCancelled checks that the reset cancelled every queued run and
// finished on the preparation step.
func assertRoundCancelled(t *testing.T, h *roundHarness, s *viewState) {
	t.Helper()
	// The page reads the queue through the same boundary, so refresh it the way
	// a window frame would after the reset completed.
	s.tasks = h.tasks()
	if got := h.cancelledIDs(); len(got) != 2 {
		t.Fatalf("confirm reached the boundary %d times, want 2", len(got))
	}
	if s.newTaskPending || s.task.stage != "prepare_candidate" {
		t.Fatalf("reset did not finish: pending=%v stage=%q", s.newTaskPending, s.task.stage)
	}
	for _, task := range s.tasks {
		if task.State != "cancelled" {
			t.Fatalf("run %q ended as %q", task.TaskId, task.State)
		}
	}
}

// harnessState mounts a running task page wired to the harness queue.
func harnessState(h *roundHarness) viewState {
	s := testState()
	s.selected, s.task.stage = "dashboard", "run"
	s.ctx = context.Background()
	s.tasks = h.tasks()
	h.install(&s)
	return s
}

// resetFinished reports whether the page reached a terminal reset state.
func resetFinished(s *viewState) bool {
	return !s.newTaskPending && s.task.stage == "prepare_candidate"
}

// When the service boundary is shared, returning must not cancel even when the
// page later re-renders with a fresh queue.
func TestWholeRoundDismissLeavesQueueUntouched(t *testing.T) {
	h := newRoundHarness(core.RunTask{TaskId: "only", Kind: "run", State: "running", Bundle: ptr("zh-core")})
	s := harnessState(h)
	tt := ui.NewTester(s.leftNav, 240, 900)
	click(t, tt, "新任务")
	tt.Frame()
	tt.Click("返回")
	tt.Frame()
	if s.newTaskPending || h.tasks()[0].State != "running" || len(h.cancelledIDs()) != 0 {
		t.Fatalf("dismiss changed the queue or the pending state: %+v", h.tasks())
	}
}
