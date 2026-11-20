package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The first bundle's PID is resolved through this round's own task id, never by
// bundle name: RunTasks is newest-first, so an older round's completed task for
// the same bundle must not answer for this one.
func TestInitialBundlePIDTracksCurrentRoundTask(t *testing.T) {
	newState := func() *viewState {
		s := testState()
		s.task = benchmarkTask{stage: "run", bundles: []string{"zh-core", "zh-svg-pelican"},
			bindings: &benchmarkBindings{localTasks: map[string]string{"round2-core": "zh-core", "round2-pelican": "zh-svg-pelican"}},
			start:    startChainResult{localRequest: &core.LocalCandidateRequest{ServicePID: 4000, Bundles: []string{"zh-core", "zh-svg-pelican"}}}}
		// An older round finished this bundle with the same (now rejected) PID.
		s.tasks = []core.RunTask{
			{TaskId: "round2-core", Kind: "run", State: "starting", Bundle: ptr("zh-core")},
			{TaskId: "round1-core", Kind: "run", State: "done", Bundle: ptr("zh-core"), ConfigPath: ptr("/w/bound/round1/config.toml"), RunPath: ptr("/w/results/round1")},
		}
		return &s
	}

	s := newState()
	if !s.localPIDUsed(4000) {
		t.Fatal("current round's in-flight service is not held")
	}
	// The CLI rejected it during verification: the service returns to the user,
	// even though the older round's task succeeded.
	s.tasks[0] = core.RunTask{TaskId: "round2-core", Kind: "run", State: "failed", Bundle: ptr("zh-core"), Error: core.Err("candidate_binding_invalid")}
	if s.localPIDUsed(4000) {
		t.Fatal("rejected current-round service still counts as used")
	}
	if !s.localSelectionInvalid(4000, nil) {
		t.Fatal("rejected current-round PID is not retryable")
	}

	// A verified binding stays consumed even if the run that follows it fails.
	verified := newState()
	verified.tasks[0] = core.RunTask{TaskId: "round2-core", Kind: "run", State: "failed", Bundle: ptr("zh-core"),
		ConfigPath: ptr("/w/bound/round2/config.toml"), Error: core.Err("run_incomplete")}
	if !verified.localPIDUsed(4000) {
		t.Fatal("verified binding was released because the run failed")
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
