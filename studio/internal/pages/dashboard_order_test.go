package pages

import (
	"context"
	"testing"

	"inferyard/studio/internal/core"
)

// waitingBackend is a local benchmark backend whose queue changes on each read,
// so a double read in one iteration observes different snapshots.
type waitingBackend struct {
	tasks    []core.RunTask
	reads    int
	onRead   func(*waitingBackend)
	started  bool
	requests []core.StartRunsRequest
}

func (b *waitingBackend) PrepareLocalCandidates(context.Context, core.LocalCandidateRequest) ([]core.CandidateRun, *core.ApiError) {
	return nil, nil
}

func (b *waitingBackend) Progress(context.Context) (core.ProgressResponse, *core.ApiError) {
	return core.ProgressResponse{}, nil
}

func (b *waitingBackend) WizardExecute(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError) {
	return core.WizardResult{}, nil
}

func (b *waitingBackend) StartRuns(q core.StartRunsRequest) ([]core.RunTask, *core.ApiError) {
	b.requests = append(b.requests, q)
	return b.tasks, nil
}

func (b *waitingBackend) StartCandidateRuns(q core.CandidateRunsRequest) ([]core.RunTask, *core.ApiError) {
	return b.tasks, nil
}

func (b *waitingBackend) StartEngine(string) (core.EngineState, *core.ApiError) {
	return core.EngineState{State: "running", Pid: 71, Endpoint: "http://127.0.0.1:8080"}, nil
}

func (b *waitingBackend) Engine() core.EngineState {
	return core.EngineState{State: "running", Pid: 71, Endpoint: "http://127.0.0.1:8080"}
}

// RunTasks returns newest-first and lets the fixture advance state after the
// snapshot is taken, so a second read in the same iteration would observe it.
func (b *waitingBackend) RunTasks() []core.RunTask {
	b.reads++
	out := make([]core.RunTask, 0, len(b.tasks))
	for i := len(b.tasks) - 1; i >= 0; i-- {
		out = append(out, b.tasks[i])
	}
	if b.onRead != nil {
		b.onRead(b)
	}
	return out
}

// A second-bundle binding that succeeded and then failed during the run must keep
// its PID: the verified ConfigPath is checked before the error.
func TestReconcileKeepsPIDForVerifiedBindingThatLaterFails(t *testing.T) {
	s := testState()
	s.reserveLocalPID("second", 4002)
	tasks := []core.RunTask{{
		TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"),
		ConfigPath: ptr("/w/bound/second/config.toml"), Error: core.Err("run_incomplete"),
	}}
	s.reconcileLocalPIDs(tasks)
	if !s.localUsedPIDs[4002] {
		t.Fatal("verified binding released because the run failed")
	}
	if _, pending := s.localPendingPIDs["second"]; pending {
		t.Fatal("verified service still pending")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("verified service offered again")
	}
}

// An attempt that failed before writing a binding releases the PID.
func TestReconcileReleasesPIDWithoutVerifiedBinding(t *testing.T) {
	s := testState()
	s.reserveLocalPID("second", 4002)
	s.reconcileLocalPIDs([]core.RunTask{{
		TaskId: "second", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: core.Err("candidate_binding_invalid"),
	}})
	if s.localUsedPIDs[4002] {
		t.Fatal("unverified binding consumed the PID")
	}
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("unverified PID is not retryable")
	}
}

// A task that finishes between two reads of the queue must not be missed: the
// iteration uses one snapshot, so the real error survives.
func TestRunQueueReadsOneSnapshotPerIteration(t *testing.T) {
	root := core.Err("service_identity_unavailable")
	b := &waitingBackend{}
	// First read: both tasks still running. Second read (the same iteration) would
	// see them terminal if it happened.
	b.tasks = []core.RunTask{
		{TaskId: "core", Kind: "run", State: "running", Bundle: ptr("zh-core")},
		{TaskId: "pelican", Kind: "run", State: "running", Bundle: ptr("zh-svg-pelican")},
	}
	b.onRead = func(b *waitingBackend) {
		if b.reads == 1 {
			b.tasks = []core.RunTask{
				{TaskId: "core", Kind: "run", State: "failed", Bundle: ptr("zh-core"), Error: root},
				{TaskId: "pelican", Kind: "run", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: core.Err("previous_scope_incomplete")},
			}
		}
	}
	// Ensure the loop terminates if it ignores the first snapshot.
	bindings := &benchmarkBindings{}
	e := runLocalBenchmarkFlow(context.Background(), b, startChainResult{
		workspace: "/w",
		localCandidates: []core.CandidateRun{
			{CandidatePath: "/w/c/zh-core.toml", Bundle: "zh-core", Family: "lmstudio"},
			{CandidatePath: "/w/c/zh-svg-pelican.toml", Bundle: "zh-svg-pelican", Family: "lmstudio"},
		},
		localRequest: &core.LocalCandidateRequest{ModelName: "m", EngineName: "lmstudio", Bundles: []string{"zh-core", "zh-svg-pelican"}},
	}, []string{"zh-core", "zh-svg-pelican"}, bindings, func(string) {}, func([]string) {}, func([]string, string) {
		t.Fatal("failed run produced a report")
	})
	if e == nil || e.Code != root.Code {
		t.Fatalf("root cause lost: %+v", e)
	}
	if bindings.failedBundle != "zh-core" {
		t.Fatalf("failed bundle: %q", bindings.failedBundle)
	}
	if b.reads == 0 {
		t.Fatal("queue never read")
	}
}
