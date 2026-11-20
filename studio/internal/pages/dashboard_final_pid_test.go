package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The first bundle's form PID belongs to the round until the CLI rejects that
// attempt; a rejection releases it so the same service can be retried.
func TestInitialBundleFailureReleasesPID(t *testing.T) {
	s := testState()
	s.task = benchmarkTask{stage: "run", bundles: []string{"zh-core", "zh-svg-pelican"},
		start: startChainResult{localRequest: &core.LocalCandidateRequest{ServicePID: 4000, Bundles: []string{"zh-core", "zh-svg-pelican"}}}}
	s.tasks = []core.RunTask{{TaskId: "first", Kind: "run", State: "starting", Bundle: ptr("zh-core")}}
	if !s.localPIDUsed(4000) {
		t.Fatal("first bundle's service is not held while it is verifying")
	}
	// The CLI rejected it mid-verification: the service returns to the user.
	failed := []core.RunTask{{TaskId: "first", Kind: "run", State: "failed", Bundle: ptr("zh-core"), Error: core.Err("candidate_binding_invalid")}}
	s.tasks = failed
	if s.localPIDUsed(4000) {
		t.Fatal("rejected first-bundle service still counts as used")
	}
	if !s.localSelectionInvalid(4000, nil) {
		t.Fatal("rejected first-bundle PID is not retryable")
	}
	// A retried attempt verifies it; then it belongs to the round.
	s.tasks = []core.RunTask{{TaskId: "first-retry", Kind: "run", State: "running", Bundle: ptr("zh-core")}}
	if !s.localPIDUsed(4000) {
		t.Fatal("verified first-bundle service was not held")
	}
}

// An asynchronous CLI rejection on the handoff path is rendered by the step, and
// the task list does not repeat it.
func TestAsyncHandoffRejectionRenderedOnce(t *testing.T) {
	s := testState()
	s.task = benchmarkTask{stage: "bind", bundles: []string{"zh-core", "zh-svg-pelican"}}
	s.progress.NextAction = "bind"
	s.progress.Bundles = []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_missing", NextAction: "run", BoundPath: ptr("/w/bound/zh-core-1/config.toml")},
		bindable("zh-svg-pelican", "/w/c/zh-svg-pelican.toml"),
	}
	e := core.Err("cli_error")
	s.tasks = []core.RunTask{{TaskId: "second", Kind: "run", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: e}}
	s.task.failure, s.failure = e, e
	tt := ui.NewTester(s.view, 1120, 2200)
	if n := countText(tt, "cli_error"); n != 1 {
		t.Fatalf("rejection rendered %d times: %v", n, tt.Texts())
	}
	if !tt.HasText("连接并核验服务失败") {
		t.Fatalf("step does not own the rejection: %v", tt.Texts())
	}
}

// The same task error stays in the list when the visible step does not own it,
// because the list is then the only place it appears.
func TestTaskErrorStaysListedWithoutStepOwner(t *testing.T) {
	s := runnablePlan()
	// No recorded step failure: the task list is the only place the error appears.
	e := core.Err("cli_error")
	s.tasks = []core.RunTask{{TaskId: "second", Kind: "run", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: e}}
	tt := ui.NewTester(s.view, 1120, 2200)
	if n := countText(tt, "cli_error"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}
