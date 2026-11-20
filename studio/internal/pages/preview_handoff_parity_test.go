//go:build ui_debug && !release

package pages

import (
	"testing"
	"time"
)

// The page and the simulator must accept and reject the same handoff inputs.
// The production side is the page's own disable rule plus the service check the
// clicked button passes through; the preview side is the simulated bind.
func TestHandoffAcceptanceMatchesPreview(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		pid              int64
		endpoint         string
		used             map[int64]bool
		wantCode         string // empty means accepted
		wantPageDisabled bool
	}{
		{name: "accepted", pid: 4002, endpoint: "http://127.0.0.1:48857"},
		{name: "endpoint_mismatch", pid: 4002, endpoint: "http://127.0.0.1:49999", wantCode: "candidate_endpoint_mismatch", wantPageDisabled: true},
		{name: "reused_pid", pid: 4001, endpoint: "http://127.0.0.1:48857", used: map[int64]bool{4001: true}, wantCode: "local_engine_service_reused", wantPageDisabled: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
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
			if scenario.used != nil {
				s.localUsedPIDs = scenario.used
			}
			// The page disables the same combination the simulator then rejects.
			disabled := s.localSelectionInvalid(scenario.pid, nil) ||
				scenario.endpoint != s.localExpectedEndpoint()
			if disabled != scenario.wantPageDisabled {
				t.Fatalf("page disabled=%v, want %v", disabled, scenario.wantPageDisabled)
			}
			p.dispatch(previewLocalBinding{taskID: id, pid: scenario.pid, endpoint: scenario.endpoint})
			p.advance(time.Second)
			got := ""
			if s.failure != nil {
				got = s.failure.Code
			}
			if got != scenario.wantCode {
				t.Fatalf("preview failure %q, want %q", got, scenario.wantCode)
			}
			// The handoff card owns the rejection, so the preview must attribute it
			// the same way the production card does.
			handoff := ""
			if s.handoffFailure != nil {
				handoff = s.handoffFailure.Code
			}
			if handoff != scenario.wantCode {
				t.Fatalf("preview handoff failure %q, want %q", handoff, scenario.wantCode)
			}
			// The production page consumes only an accepted service; the preview must
			// account for it the same way, so a rejected PID stays retryable.
			wantUsed := scenario.wantCode == "" || scenario.used[scenario.pid]
			if got := s.localPIDUsed(scenario.pid); got != wantUsed {
				t.Fatalf("preview used PID=%v after %q, want %v", got, scenario.wantCode, wantUsed)
			}
		})
	}
}
