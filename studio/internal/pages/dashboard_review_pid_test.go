package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestHandoffFailureRenderedOnce(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	e := core.Err("cli_blocked")
	s.tasks = []core.RunTask{{TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: e}}
	s.localBindingTask = "second"
	s.handoffFailure, s.failure = e, e
	tt := ui.NewTester(s.view, 1120, 2000)
	if tt.HasText("cli_blocked") || !tt.HasText("inferyard 拒绝执行") {
		t.Fatalf("unexpected handoff summary: %v", tt.Texts())
	}
	if n := countText(tt, "错误详情"); n != 1 {
		t.Fatalf("handoff banner rendered %d times: %v", n, tt.Texts())
	}
	click(t, tt, "错误详情")
	if n := countText(tt, "错误码：cli_blocked"); n != 1 {
		t.Fatalf("handoff cause rendered %d times: %v", n, tt.Texts())
	}
}

func TestTaskErrorOutsideHandoffStaysListed(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	e := core.Err("cli_blocked")
	s.tasks = []core.RunTask{{TaskId: "first", State: "failed", Bundle: ptr("zh-core"), Error: e}}
	tt := ui.NewTester(s.view, 1120, 2000)
	if tt.HasText("cli_blocked") || !tt.HasText("inferyard 拒绝执行") {
		t.Fatalf("unexpected task error summary: %v", tt.Texts())
	}
	if n := countText(tt, "错误详情"); n != 1 {
		t.Fatalf("task banner rendered %d times: %v", n, tt.Texts())
	}
	click(t, tt, "错误详情")
	if n := countText(tt, "错误码：cli_blocked"); n != 1 {
		t.Fatalf("task cause rendered %d times: %v", n, tt.Texts())
	}
}

func TestHandoffPIDConsumedOnlyAfterVerification(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	s.tasks = []core.RunTask{{TaskId: "second", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	s.reserveLocalPID("second", 4002)
	if !s.localPIDUsed(4002) {
		t.Fatal("in-flight service offered again")
	}
	if s.localUsedPIDs[4002] {
		t.Fatal("in-flight service already recorded as used")
	}
	failed := []core.RunTask{{TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: core.Err("candidate_binding_invalid")}}
	s.reconcileLocalPIDs(failed)
	if s.localPIDUsed(4002) {
		t.Fatal("failed verification consumed the PID")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("PID is not retryable after a failed verification")
	}
	s.reserveLocalPID("second", 4002)
	verified := []core.RunTask{{TaskId: "second", State: "running", Bundle: ptr("zh-svg-pelican"), ConfigPath: ptr("/w/bound/second/config.toml")}}
	s.reconcileLocalPIDs(verified)
	if !s.localUsedPIDs[4002] {
		t.Fatal("verified service not recorded")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("verified service offered again")
	}
}
