package pages

import (
	"context"
	"testing"

	"inferyard/studio/internal/core"
)

type sequenceLocalBackend struct {
	tasks  []core.RunTask
	starts int
}

func (b *sequenceLocalBackend) PrepareLocalCandidates(context.Context, core.LocalCandidateRequest) ([]core.CandidateRun, *core.ApiError) {
	return nil, nil
}

func (b *sequenceLocalBackend) Progress(context.Context) (core.ProgressResponse, *core.ApiError) {
	return core.ProgressResponse{}, nil
}

func (b *sequenceLocalBackend) WizardExecute(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError) {
	return core.WizardResult{}, nil
}

func (b *sequenceLocalBackend) StartRuns(core.StartRunsRequest) ([]core.RunTask, *core.ApiError) {
	b.starts++
	return b.tasks, nil
}

func (b *sequenceLocalBackend) StartEngine(string) (core.EngineState, *core.ApiError) {
	return core.EngineState{State: "running", Pid: 71, Endpoint: "http://127.0.0.1:8080"}, nil
}

func (b *sequenceLocalBackend) Engine() core.EngineState {
	return core.EngineState{State: "running", Pid: 71, Endpoint: "http://127.0.0.1:8080"}
}

func (b *sequenceLocalBackend) StartCandidateRuns(core.CandidateRunsRequest) ([]core.RunTask, *core.ApiError) {
	b.starts++
	return b.tasks, nil
}

func (b *sequenceLocalBackend) RunTasks() []core.RunTask {
	out := make([]core.RunTask, 0, len(b.tasks))
	for i := len(b.tasks) - 1; i >= 0; i-- {
		out = append(out, b.tasks[i])
	}
	return out
}

func TestFirstBundleErrorOutranksPreviousScopeIncomplete(t *testing.T) {
	root := core.Err("candidate_binding_invalid")
	blocked := core.Err("previous_scope_incomplete")
	b := &sequenceLocalBackend{tasks: []core.RunTask{
		{TaskId: "core", Kind: "run", State: "failed", Bundle: ptr("zh-core"), Error: root},
		{TaskId: "pelican", Kind: "run", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: blocked},
	}}
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
}

func TestBlockedLaterBundleDoesNotMaskRootCause(t *testing.T) {
	root := core.Err("service_identity_unavailable")
	blocked := core.Err("previous_scope_incomplete")
	b := &sequenceLocalBackend{tasks: []core.RunTask{
		{TaskId: "pelican", Kind: "run", State: "failed", Bundle: ptr("zh-svg-pelican"), Error: blocked},
		{TaskId: "core", Kind: "run", State: "failed", Bundle: ptr("zh-core"), Error: root},
	}}
	bindings := &benchmarkBindings{}
	e := runLocalBenchmarkFlow(context.Background(), b, startChainResult{
		workspace: "/w",
		localCandidates: []core.CandidateRun{
			{CandidatePath: "/w/c/zh-core.toml", Bundle: "zh-core", Family: "lmstudio"},
			{CandidatePath: "/w/c/zh-svg-pelican.toml", Bundle: "zh-svg-pelican", Family: "lmstudio"},
		},
		localRequest: &core.LocalCandidateRequest{ModelName: "m", EngineName: "lmstudio", Bundles: []string{"zh-core", "zh-svg-pelican"}},
	}, []string{"zh-svg-pelican", "zh-core"}, bindings, func(string) {}, func([]string) {}, func([]string, string) {
		t.Fatal("failed run produced a report")
	})
	if e == nil || e.Code == blocked.Code {
		t.Fatalf("blocked bundle blamed: %+v", e)
	}
	if bindings.failedBundle != "zh-core" {
		t.Fatalf("failed bundle: %q", bindings.failedBundle)
	}
}
