//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// observeDestructive clicks the gate, re-reads the queue, then dismisses it and
// reports whether the prompt appeared, the run is still active and no new task
// started. Both sides expose the cancel through an injected boundary, so
// "return" is also proven by the absence of a cancel call.
func observeDestructive(s *viewState, tt *ui.Tester, click, confirm, dismiss string) (prompt, running, newTask bool) {
	if err := tt.Click(click); err != nil {
		return false, s.running() != nil, true
	}
	tt.Frame()
	prompt = tt.HasText(confirm)
	if prompt {
		_ = tt.Click(dismiss)
		tt.Frame()
	}
	return prompt, s.running() != nil, s.newTaskPending
}

func TestPreviewDestructiveGateParity(t *testing.T) {
	production, productionView, calls := destructiveProduction(t)
	p, previewView := destructivePreview(t)

	// Returning must not cancel on either side.
	assertRunGateDismissed(t, "production", production, productionView)
	assertRunGateDismissed(t, "preview", p.state, previewView)
	assertNoCancel(t, calls, "dismiss")

	detailState, detailView := destructiveDetail(t, calls)
	assertRunGateDismissed(t, "detail", detailState, detailView)
	assertNoCancel(t, calls, "detail dismiss")
	confirmDetailCancel(t, detailView, calls)
	assertNoCancel(t, calls, "preview")

	// The preview new-task gate dismisses without cancelling anything.
	if p.state.task.stage != "run" {
		t.Fatalf("preview left the running step: %q", p.state.task.stage)
	}
	nav := ui.NewTester(p.state.leftNav, 240, 900)
	assertRunGateDismissed(t, "preview new task", p.state, nav)
}

// assertRunGateDismissed clicks cancel, dismisses the prompt and checks that the
// run and the new-task state are unchanged.
func assertRunGateDismissed(t *testing.T, name string, s *viewState, tt *ui.Tester) {
	t.Helper()
	click, confirm := "取消运行", "确认取消运行"
	switch name {
	case "detail":
		click, confirm = "取消任务", "确认取消任务"
	case "preview new task":
		click, confirm = "新任务", "确认开始新任务"
	}
	prompt, running, newTask := observeDestructive(s, tt, click, confirm, "返回")
	if !prompt || !running || newTask {
		t.Fatalf("%s dismiss changed the run: prompt=%v running=%v newTask=%v", name, prompt, running, newTask)
	}
}

// destructiveProduction drives the real run view with a counter at the service
// boundary, so a dismiss that cancels is observable.
func destructiveProduction(t *testing.T) (*viewState, *ui.Tester, chan string) {
	t.Helper()
	s := testState()
	s.selected, s.task.stage = "dashboard", "run"
	s.tasks = []core.RunTask{{TaskId: "run", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.ctx = context.Background()
	calls := make(chan string, 4)
	s.cancelRun = func(string) (core.RunTask, *core.ApiError) { return s.tasks[0], nil }
	return &s, ui.NewTester(s.runView, 1120, 900), calls
}

// destructivePreview starts a preview run and returns the same page builder.
func destructivePreview(t *testing.T) (*previewSession, *ui.Tester) {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	p.state.startBenchmark()
	for range 100 {
		p.advance(100 * time.Millisecond)
		if p.state.running() != nil {
			break
		}
	}
	if p.state.running() == nil {
		t.Fatal("preview run never started")
	}
	return p, ui.NewTester(p.state.runView, 1120, 900)
}

// destructiveDetail drives the run detail page, whose cancel dispatches through
// the injected boundary rather than starting a new round.
func destructiveDetail(t *testing.T, calls chan string) (*viewState, *ui.Tester) {
	t.Helper()
	s := testState()
	// work() refreshes after reporting; a cancelled context stops it before the
	// refresh, which has no service here.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ctx = ctx
	s.tasks = []core.RunTask{{TaskId: "detail-run", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.detail = &core.RunTaskDetail{Task: s.tasks[0]}
	s.cancelRun = func(id string) (core.RunTask, *core.ApiError) {
		calls <- id
		return s.tasks[0], nil
	}
	return &s, ui.NewTester(s.detailView, 1120, 1200)
}

func confirmDetailCancel(t *testing.T, detailView *ui.Tester, calls chan string) {
	t.Helper()
	detailView.Frame()
	if err := detailView.Click("取消任务"); err != nil {
		t.Fatal(err)
	}
	detailView.Frame()
	if err := detailView.Click("确认取消任务"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-calls:
		if id != "detail-run" {
			t.Fatalf("confirmed the wrong run: %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirm never reached the cancel boundary")
	}
	assertNoCancel(t, calls, "second confirm")
}

func assertNoCancel(t *testing.T, calls <-chan string, step string) {
	t.Helper()
	select {
	case id := <-calls:
		t.Fatalf("%s cancelled run %q", step, id)
	default:
	}
}
