package pages

import (
	"context"
	"slices"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func runnablePlan() *viewState {
	s := testState()
	s.task = benchmarkTask{stage: "run", bundles: []string{"zh-core"}}
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/w", NextAction: "run", Bundles: []inferyard.BundleProgress{{
		Bundle: "zh-core", Phase: "run_missing", NextAction: "run",
		BoundPath:     ptr("/w/bound/zh-core-1/config.toml"),
		CandidatePath: ptr("/w/c/zh-core.toml"),
		NextCommand:   ptr("inferyard run --config /w/bound/zh-core-1/config.toml"),
	}}}
	return &s
}

func TestForeignManagedEngineDoesNotRebindBundle(t *testing.T) {
	s := runnablePlan()
	s.engineState = core.EngineState{State: "running", Local: true, Pid: 9002, Endpoint: "http://127.0.0.1:48857", Candidate: "/w/c/zh-svg-pelican.toml"}
	b := s.active()
	if b == nil || *b.CandidatePath == s.engineState.Candidate {
		t.Fatal("fixture: engine candidate matches the bundle")
	}
	var ran []string
	if e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		ran = append(ran, q.Command)
		return core.WizardResult{}, nil
	}); e != nil {
		t.Fatal(e)
	}
	if len(ran) != 1 {
		t.Fatalf("expected the frozen run only, got %v", ran)
	}
	args, err := inferyard.SplitCommand(ran[0])
	if err != nil || !slices.Equal(args, []string{"run", "--config", "/w/bound/zh-core-1/config.toml"}) {
		t.Fatalf("unexpected run command: %v / %v", args, err)
	}
}
