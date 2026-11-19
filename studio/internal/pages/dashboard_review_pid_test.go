package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// A failed handoff renders its cause once: the handoff card shows it next to the
// form and the task list below does not repeat the same diagnostic.
func TestHandoffFailureRenderedOnce(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	e := core.Err("cli_blocked")
	s.tasks = []core.RunTask{{TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: e}}
	s.localBindingTask = "second"
	s.handoffFailure, s.failure = e, e
	tt := ui.NewTester(s.view, 1120, 2000)
	if n := countText(tt, "cli_blocked"); n != 1 {
		t.Fatalf("handoff cause rendered %d times: %v", n, tt.Texts())
	}
}

// A task error the handoff card does not show stays in the task list: the list
// is then the only place the cause appears.
func TestTaskErrorOutsideHandoffStaysListed(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	e := core.Err("cli_blocked")
	s.tasks = []core.RunTask{{TaskId: "first", State: "failed", Bundle: ptr("zh-core"), Error: e}}
	tt := ui.NewTester(s.view, 1120, 2000)
	if n := countText(tt, "cli_blocked"); n != 1 {
		t.Fatalf("task cause rendered %d times: %v", n, tt.Texts())
	}
}

// A handoff PID is only reserved once the service accepts the request; a failed
// CLI verification releases it so the same service can be retried.
func TestHandoffPIDConsumedOnlyAfterVerification(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	s.tasks = []core.RunTask{{TaskId: "second", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	s.reserveLocalPID("second", 4002)
	// While the CLI is still verifying, the service is reserved, not consumed.
	if !s.localPIDUsed(4002) {
		t.Fatal("in-flight service offered again")
	}
	if s.localUsedPIDs[4002] {
		t.Fatal("in-flight service already recorded as used")
	}
	// Verification failed: the task is failed with an error, and the PID is free.
	failed := []core.RunTask{{TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: core.Err("candidate_binding_invalid")}}
	s.reconcileLocalPIDs(failed)
	if s.localPIDUsed(4002) {
		t.Fatal("failed verification consumed the PID")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("PID is not retryable after a failed verification")
	}
	// The retry is accepted; once it progresses past binding, the PID is used.
	s.reserveLocalPID("second", 4002)
	verified := []core.RunTask{{TaskId: "second", State: "running", Bundle: ptr("zh-svg-pelican")}}
	s.reconcileLocalPIDs(verified)
	if !s.localUsedPIDs[4002] {
		t.Fatal("verified service not recorded")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("verified service offered again")
	}
}
