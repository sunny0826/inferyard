//go:build ui_debug && !release

package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// A pending preview stop prompt is dropped the same way, since preview runs the
// same renderer over its own engine state.
func TestPreviewStopPromptDropsReplacedEngine(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.state.engineState = core.EngineState{State: "running", Pid: 42, StartTicks: 111, Endpoint: "http://127.0.0.1:48857"}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { p.state.serviceView(c) }) }, 1120, 1200)
	click(t, tt, "停止服务")
	tt.Frame()
	if p.state.pendingDestructive == (destructiveRequest{}) {
		t.Fatal("preview prompt did not open")
	}
	p.state.engineState = core.EngineState{State: "running", Pid: 43, StartTicks: 222, Endpoint: "http://127.0.0.1:48857"}
	tt.Frame()
	if p.state.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("preview kept a stale prompt: %+v", p.state.pendingDestructive)
	}
}
