//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPreviewManualBindTargetsUnboundBundle(t *testing.T) {
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
	target := s.bindTarget()
	if target == nil || target.Bundle != "zh-svg-pelican" {
		t.Fatalf("manual bind targets %+v", target)
	}
	core := s.progress.Bundles[0]
	if core.Bundle != "zh-core" || core.BoundPath == nil {
		t.Fatalf("earlier binding lost: %+v", core)
	}
	s.engineState.State = "running"
	s.engineState.Pid = 9001
	s.localPID, s.localEndpoint = "4002", s.localExpectedEndpoint()
	id := previewAwaiting(p)
	p.dispatch(previewLocalBinding{taskID: id, pid: 4002, endpoint: s.localEndpoint})
	for i := 0; i < 50 && previewAwaiting(p) != ""; i++ {
		p.advance(100 * time.Millisecond)
	}
	if previewAwaiting(p) != "" {
		t.Fatal("preview did not accept the handoff")
	}
	if s.progress.Bundles[0].BoundPath == nil || *s.progress.Bundles[0].BoundPath == "" {
		t.Fatalf("handoff overwrote the verified binding: %+v", s.progress.Bundles[0])
	}
	tt := ui.NewTester(s.view, 1120, 2000)
	if s.failure != nil {
		t.Fatalf("preview reported a failure: %+v", s.failure)
	}
	if !tt.HasText("当前测评题集运行进度") && !tt.HasText("开始运行") {
		t.Fatalf("preview did not reach a runnable step: %v", tt.Texts())
	}
}

func TestPreviewHandoffRetryWithSamePID(t *testing.T) {
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
	s.localPID, s.localEndpoint = "4002", s.localExpectedEndpoint()
	p.failNext = true
	p.dispatch(previewLocalBinding{taskID: id, pid: 4002, endpoint: s.localEndpoint})
	for i := 0; i < 50 && s.task.failure == nil; i++ {
		p.advance(100 * time.Millisecond)
	}
	if s.task.failure == nil {
		t.Fatal("rejected handoff produced no failure")
	}
	tt := ui.NewTester(s.view, 1120, 2000)
	if n := countText(tt, "cli_blocked"); n != 1 {
		t.Fatalf("rejected handoff rendered %d times: %v", n, tt.Texts())
	}
	if s.localPIDUsed(4002) {
		t.Fatal("rejected handoff consumed the PID")
	}
	p.state.retryBenchmark()
	for i := 0; i < 1000 && previewAwaiting(p) == ""; i++ {
		p.advance(100 * time.Millisecond)
	}
	id = previewAwaiting(p)
	if id == "" {
		t.Fatal("retry did not reach the handoff")
	}
	s.localPID, s.localEndpoint = "4002", s.localExpectedEndpoint()
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
