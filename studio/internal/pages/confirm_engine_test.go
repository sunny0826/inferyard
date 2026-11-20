package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func engineStopState() *viewState {
	s := testState()
	s.task.stage = "bind"
	s.engineState = core.EngineState{State: "running", Pid: 42, StartTicks: 111, Endpoint: "http://127.0.0.1:48857"}
	return &s
}

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
