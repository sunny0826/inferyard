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

// A changed start time alone (same PID) still counts as a replacement, so a
// restarted process cannot inherit an armed prompt.
func TestStopConfirmationTracksStartTime(t *testing.T) {
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
		t.Fatal("restarted engine still validated")
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

// The header's engine panel is a third entry to the same stop prompt. A
// replacement instance must clear the armed request outright: the prompt is
// gone, nothing is pending, and the stop boundary is never reached.
func TestStopConfirmationHeaderDropsReplacedEngine(t *testing.T) {
	s := engineStopState()
	stopped := 0
	s.stopEngineAPI = func() (core.EngineState, *core.ApiError) {
		stopped++
		return core.EngineState{State: "stopped"}, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.cornerStatus(c) }) }, 1120, 400)
	if err := tt.Click("引擎 · 运行中"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Frame()
	click(t, tt, "停止引擎")
	tt.Frame()
	if s.pendingDestructive == (destructiveRequest{}) {
		t.Fatal("header prompt did not open")
	}
	// A successor process appeared while the prompt stayed open.
	s.engineState = core.EngineState{State: "running", Pid: 44, StartTicks: 333, Endpoint: "http://127.0.0.1:48857"}
	tt.Frame()
	if s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("replaced engine left a stale prompt armed: %+v", s.pendingDestructive)
	}
	click(t, tt, "引擎 · 运行中")
	tt.Frame()
	if _, ok := tt.Find("确认停止引擎"); ok {
		t.Fatal("stale confirmation is still offered")
	}
	click(t, tt, "停止引擎")
	tt.Frame()
	if s.pendingDestructive.pid != 44 || s.pendingDestructive.startTicks != 333 {
		t.Fatalf("a fresh prompt must snapshot the current engine: %+v", s.pendingDestructive)
	}
	if stopped != 0 {
		t.Fatalf("a replaced engine was stopped %d times", stopped)
	}
}

// A stop prompt left open when the engine stops or fails is forgotten even
// though no stop entry renders while the engine is down. Otherwise an engine
// that comes back with the same snapshot, as the preview's simulated engine
// does on every start, would reopen the header already armed to stop it.
func TestStopPromptClearsWhenEngineGoesDown(t *testing.T) {
	for _, down := range []string{"stopped", "failed"} {
		t.Run(down, func(t *testing.T) {
			s := engineStopState()
			s.task.stage = ""
			tt := ui.NewTester(s.view, 1120, 800)
			click(t, tt, "引擎 · 运行中")
			click(t, tt, "停止引擎")
			if _, ok := tt.Find("确认停止引擎"); !ok {
				t.Fatal("header prompt did not open", tt.Texts())
			}
			running := s.engineState
			s.engineState = core.EngineState{State: down, Pid: running.Pid, StartTicks: running.StartTicks}
			tt.Frame()
			s.engineState = running
			tt.Frame()
			if _, ok := tt.Find("确认停止引擎"); ok {
				t.Fatal("a prompt from before the engine went down came back armed")
			}
			if _, ok := tt.Find("停止引擎"); !ok {
				t.Fatal("the header must offer a fresh stop entry", tt.Texts())
			}
		})
	}
}
