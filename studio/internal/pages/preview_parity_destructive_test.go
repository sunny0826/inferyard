//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// destructiveFrame records what one confirmation gate does to the page: the
// first click must leave the work intact and show the prompt, and dismissing
// must not dispatch anything. Production and preview pages are driven through
// the same controls; neither side reads the other's result.
type destructiveFrame struct {
	Prompt     bool
	Running    bool
	Canceled   bool
	NewTask    bool
	Dispatched bool
}

func observeDestructive(s *viewState, tt *ui.Tester, click, confirm string) destructiveFrame {
	frame := destructiveFrame{}
	if err := tt.Click(click); err != nil {
		return destructiveFrame{Dispatched: true}
	}
	tt.Frame()
	frame.Prompt = tt.HasText(confirm)
	frame.Running = s.running() != nil
	frame.Canceled = s.newTaskPending
	frame.NewTask = s.task.stage == "prepare_candidate"
	if frame.Prompt {
		_ = tt.Click("返回")
		tt.Frame()
		if s.pendingDestructive != (destructiveRequest{}) && s.newTaskPending {
			frame.Dispatched = true
		}
	}
	return frame
}

func TestPreviewDestructiveGateParity(t *testing.T) {
	production := testState()
	production.selected = "dashboard"
	production.task.stage = "run"
	production.tasks = []core.RunTask{{TaskId: "run", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	productionView := ui.NewTester(production.runView, 1120, 900)

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
	previewView := ui.NewTester(p.state.runView, 1120, 900)

	want := destructiveFrame{Prompt: true, Running: true}
	for name, got := range map[string]destructiveFrame{
		"production": observeDestructive(&production, productionView, "取消运行", "确认取消运行"),
		"preview":    observeDestructive(p.state, previewView, "取消运行", "确认取消运行"),
	} {
		if got != want {
			t.Errorf("%s gate mismatch: %+v want %+v", name, got, want)
		}
	}

	// The sidebar entry uses the same gate while a run is active.
	if p.state.task.stage != "run" {
		t.Fatalf("preview left the running step: %q", p.state.task.stage)
	}
	nav := ui.NewTester(p.state.leftNav, 240, 900)
	frame := observeDestructive(p.state, nav, "新任务", "确认开始新任务")
	if frame != (destructiveFrame{Prompt: true, Running: true}) {
		t.Errorf("preview new-task gate mismatch: %+v", frame)
	}
}
