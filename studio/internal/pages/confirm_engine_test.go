package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// engineStopState mounts a running engine whose confirm opens on demand.
func engineStopState() *viewState {
	s := testState()
	s.task.stage = "bind"
	s.engineState = core.EngineState{State: "running", Pid: 42, StartTicks: 111, Endpoint: "http://127.0.0.1:48857"}
	return &s
}

// A stop prompt opened for one engine instance must not act on a replacement:
// PID and start ticks are snapshotted on the first click, and a changed
// instance clears the request and stops nothing.
func TestStopConfirmationDropsReplacedEngine(t *testing.T) {
	s := engineStopState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	opened := s.pendingDestructive
	if opened == (destructiveRequest{}) {
		t.Fatalf("prompt did not open")
	}
	// The engine stopped and a different process took over while the prompt
	// was open.
	s.engineState = core.EngineState{State: "running", Pid: 43, StartTicks: 222, Endpoint: "http://127.0.0.1:48857"}
	tt.Frame()
	if s.destructiveTargetValid(opened) {
		t.Fatal("replaced engine still validated")
	}
	// The confirm control is gone because the view re-rendered for the new
	// engine, so validity is the gate; a stale click would dispatch nothing.
}

// The specialty page's stop entry drives the same destructiveAction with the
// same identity snapshot; the engine is locked there only when the engine is
// active and no run is running, which the guard re-checks.
func TestStopConfirmationGuardsSpecialtyWhenEngineChanges(t *testing.T) {
	s := testState()
	s.speed = &speedWorkspace{selected: true}
	s.engineState = core.EngineState{State: "running", Pid: 9, StartTicks: 1}
	var selected bool
	tt := ui.NewTester(func(c *ui.Context) { selected = s.speedSelection(c) }, 1120, 1200)
	_ = selected
	click(t, tt, "停止当前服务")
	tt.Frame()
	opened := s.pendingDestructive
	if opened == (destructiveRequest{}) {
		t.Fatalf("specialty prompt did not open")
	}
	s.engineState = core.EngineState{State: "running", Pid: 10, StartTicks: 2}
	tt.Frame()
	if s.destructiveTargetValid(opened) {
		t.Fatal("specialty replaced engine still validated")
	}
}

// Previews carry the same identity fields, so the guard applies there too.
func TestPreviewStopConfirmationTracksEngineIdentity(t *testing.T) {
	s := engineStopState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	opened := s.pendingDestructive
	if opened == (destructiveRequest{}) {
		t.Fatal("prompt did not open")
	}
	s.engineState = core.EngineState{State: "running", Pid: 42, StartTicks: 999}
	tt.Frame()
	if s.destructiveTargetValid(opened) {
		t.Fatal("preview replaced engine still validated")
	}
}

// The confirm click itself must not reach the engine-stop boundary when the
// process changed; the counter is the proof.
func TestStopConfirmationDoesNotStopReplacement(t *testing.T) {
	s := engineStopState()
	stops := 0
	s.stopEngineAPI = func() (core.EngineState, *core.ApiError) {
		s.engineState.State = "stopping"
		stops++
		return s.engineState, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	s.engineState = core.EngineState{State: "running", Pid: 43, StartTicks: 222}
	tt.Frame()
	tt.Click("确认停止服务")
	tt.Frame()
	if stops != 0 {
		t.Fatalf("replacement engine was stopped %d times", stops)
	}
}

// Dismissing also never reaches the boundary.
func TestStopConfirmationDismissDoesNotStop(t *testing.T) {
	s := engineStopState()
	stops := 0
	s.stopEngineAPI = func() (core.EngineState, *core.ApiError) {
		stops++
		return s.engineState, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	tt.Click("返回")
	tt.Frame()
	if stops != 0 {
		t.Fatalf("dismiss stopped the engine %d times", stops)
	}
}
