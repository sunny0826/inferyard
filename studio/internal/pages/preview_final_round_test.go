//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPreviewSecondBundleRetryAcceptsRejectedPID(t *testing.T) {
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
	s.localPID, s.localEndpoint = "4002", s.localExpectedEndpoint()
	p.failNext = true
	p.dispatch(previewLocalBinding{taskID: previewAwaiting(p), pid: 4002, endpoint: s.localEndpoint})
	for i := 0; i < 100 && s.task.failure == nil; i++ {
		p.advance(100 * time.Millisecond)
	}
	if s.task.failure == nil {
		t.Fatal("rejected handoff produced no failure")
	}
	if s.localPIDUsed(4002) {
		t.Fatal("rejected attempt consumed the form PID")
	}
	tt := ui.NewTester(s.view, 1120, 2200)
	if err := tt.Click("重试未完成的测评题集"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000 && previewAwaiting(p) == "" && s.task.failure == nil; i++ {
		p.advance(100 * time.Millisecond)
	}
	id := previewAwaiting(p)
	if id == "" {
		t.Fatalf("retry did not reach the handoff: %+v", s.task.failure)
	}
	if s.task.start.localRequest != nil && s.task.start.localRequest.ServicePID == 4002 {
		t.Fatal("retry reused the rejected form PID as the initial service")
	}
	s.localPID, s.localEndpoint = "4002", s.localExpectedEndpoint()
	if s.localSelectionInvalid(4002, nil) {
		t.Fatal("retry rejects the same PID")
	}
	p.dispatch(previewLocalBinding{taskID: id, pid: 4002, endpoint: s.localEndpoint})
	for i := 0; i < 100; i++ {
		p.advance(100 * time.Millisecond)
	}
	if s.task.failure != nil {
		t.Fatalf("retry with the same PID failed: %+v", s.task.failure)
	}
	if !s.localPIDUsed(4002) {
		t.Fatal("verified retry did not consume the PID")
	}
}
