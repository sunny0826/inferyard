//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"
)

// After an auto-start failure, a successful connection must advance the preview
// out of Step 4 and drop the old error, exactly like the production outcome.
func TestPreviewBindingSuccessAdvancesPastStepFour(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "任务 · 失败"))
	s := p.state
	if !s.stepFailed() {
		t.Fatalf("fixture is not failed: stage=%q engine=%q", s.task.stage, s.engineState.State)
	}
	before := previewTester(p, 1120, 1800)
	if !before.HasText("engine_start_failed") {
		t.Fatalf("failed scene lost its cause: %v", before.Texts())
	}
	s.startEngine()
	p.advance(2 * time.Second)
	if s.failure != nil || s.task.failure != nil {
		t.Fatalf("preview kept the old error: %+v / %+v", s.failure, s.task.failure)
	}
	if s.task.stage == "bind" {
		t.Fatal("preview stayed on the bind stage")
	}
	tt := previewTester(p, 1120, 1800)
	if tt.HasText("engine_start_failed") {
		t.Fatalf("old error still visible: %v", tt.Texts())
	}
	if !tt.HasText("运行当前测评题集") {
		t.Fatalf("preview did not advance to the run step: %v", tt.Texts())
	}
	// The preview continues into the run itself, so the step advanced instead of
	// staying disabled on the bind stage.
	if !tt.HasText("当前测评题集运行进度") {
		t.Fatalf("preview did not start the run: %v", tt.Texts())
	}
}

// While the queue is busy awaiting the next service, the in-place refresh still
// discovers services.
func TestPreviewHandoffRefreshWorksWhileBusy(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "引擎 · LM Studio 手动绑定"))
	s := p.state
	s.startBenchmark()
	for i := 0; i < 1000 && previewAwaiting(p) == ""; i++ {
		p.advance(100 * time.Millisecond)
	}
	if previewAwaiting(p) == "" {
		t.Fatal("preview did not reach the handoff")
	}
	if !s.busy {
		t.Fatal("handoff is not busy in the fixture")
	}
	tt := previewTester(p, 1120, 1600)
	if err := tt.Click("刷新服务列表"); err != nil {
		t.Fatal(err)
	}
	if !s.discovering {
		t.Fatalf("refresh was blocked while awaiting the service: %v", tt.Texts())
	}
	p.advance(time.Second)
	if s.discovering || !s.discoveryLoaded {
		t.Fatal("refresh did not complete")
	}
}

// A handoff failure in the preview is rendered by the handoff card, and a retry
// with the same PID is allowed once the failure is cleared.
func TestPreviewHandoffFailureVisibleAndRetryable(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "引擎 · LM Studio 手动绑定"))
	s := p.state
	s.startBenchmark()
	for i := 0; i < 1000 && previewAwaiting(p) == ""; i++ {
		p.advance(100 * time.Millisecond)
	}
	id := previewAwaiting(p)
	if id == "" {
		t.Fatal("preview did not reach the handoff")
	}
	s.localPID = "4002"
	s.localEndpoint = "http://127.0.0.1:49999"
	p.dispatch(previewLocalBinding{taskID: id, pid: 4002, endpoint: "http://127.0.0.1:49999"})
	if s.handoffFailure == nil || s.handoffFailure.Code != "candidate_endpoint_mismatch" {
		t.Fatalf("handoff failure not recorded: %+v", s.handoffFailure)
	}
	if s.localPIDUsed(4002) {
		t.Fatal("rejected handoff consumed the PID")
	}
	tt := previewTester(p, 1120, 1600)
	if n := countText(tt, "candidate_endpoint_mismatch"); n != 1 {
		t.Fatalf("handoff failure rendered %d times: %v", n, tt.Texts())
	}
}
