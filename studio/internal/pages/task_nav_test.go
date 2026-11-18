package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// taskNavState starts on a page other than the task page with a running task,
// so switching back through the sidebar must preserve the run, the queue and
// the chosen pair.
func taskNavState() *viewState {
	s := testState()
	s.selected = "reports"
	s.task.stage = "run"
	s.tasks = []core.RunTask{{TaskId: "run-1", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	s.model, s.engine = "model-1.gguf", "engine-1"
	s.ctx = context.Background()
	return &s
}

// The new sidebar entry switches to the task page without resetting, cancelling
// or confirming: the run, the queue and the chosen pair all stay untouched.
func TestTaskNavSwitchesWithoutReset(t *testing.T) {
	s := taskNavState()
	tt := ui.NewTester(s.leftNav, 240, 900)
	click(t, tt, "任务")
	tt.Frame()
	if s.selected != "dashboard" {
		t.Fatalf("task nav did not select the task page: %q", s.selected)
	}
	if s.task.stage != "run" || s.running() == nil || s.newTaskPending {
		t.Fatalf("task nav reset the run: stage=%q newTask=%v", s.task.stage, s.newTaskPending)
	}
	if s.model != "model-1.gguf" || s.engine != "engine-1" {
		t.Fatalf("task nav changed the pair: %q %q", s.model, s.engine)
	}
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("task nav left a pending confirmation: %+v", s.pendingDestructive)
	}
}

// From the task page already selected, clicking the entry is a no-op.
func TestTaskNavIsStableWhenAlreadySelected(t *testing.T) {
	s := taskNavState()
	s.selected = "dashboard"
	before := len(s.tasks)
	tt := ui.NewTester(s.leftNav, 240, 900)
	click(t, tt, "任务")
	tt.Frame()
	if s.selected != "dashboard" || len(s.tasks) != before {
		t.Fatalf("task nav changed the selected page: %q tasks %d", s.selected, len(s.tasks))
	}
}
