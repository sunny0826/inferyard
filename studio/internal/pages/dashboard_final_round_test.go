package pages

import (
	"context"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// runnablePlan is a task whose only bundle the CLI already bound, with a frozen
// run command.
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

// Restarting Studio's own engine for this bundle must re-verify the binding: the
// frozen config may name the previous process.
func TestRestartedManagedEngineRebindsBundle(t *testing.T) {
	s := runnablePlan()
	s.engineState = core.EngineState{State: "running", Local: true, Pid: 9001, Endpoint: "http://127.0.0.1:48857", Candidate: "/w/c/zh-core.toml"}
	b := s.active()
	if b == nil || *b.CandidatePath != s.engineState.Candidate {
		t.Fatal("fixture: engine candidate does not match the bundle")
	}
	var ran []string
	e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		ran = append(ran, q.Command)
		return core.WizardResult{}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(ran) != 2 {
		t.Fatalf("expected bind then run, got %v", ran)
	}
	bind, err := inferyard.SplitCommand(ran[0])
	if err != nil || bind[0] != "config" || bind[1] != "bind" {
		t.Fatalf("first command is not a bind: %v / %v", bind, err)
	}
	if !slices.Contains(bind, "9001") || !slices.Contains(bind, "http://127.0.0.1:48857") {
		t.Fatalf("bind does not target the restarted engine: %v", bind)
	}
	run, err := inferyard.SplitCommand(ran[1])
	if err != nil || run[0] != "run" {
		t.Fatalf("second command is not a run: %v / %v", run, err)
	}
}

// An engine started for another bundle must not hijack this one: the verified
// binding is used as-is.
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

// An engine that is not running leaves a verified binding untouched.
func TestStoppedEngineUsesVerifiedBinding(t *testing.T) {
	s := runnablePlan()
	s.engineState = core.EngineState{State: "stopped"}
	b := s.active()
	if b == nil || s.engineState.State == "running" {
		t.Fatal("fixture: bundle or engine state unexpected")
	}
	var ran []string
	if e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		ran = append(ran, q.Command)
		return core.WizardResult{}, nil
	}); e != nil {
		t.Fatal(e)
	}
	if len(ran) != 1 {
		t.Fatalf("unexpected commands: %v", ran)
	}
	args, err := inferyard.SplitCommand(ran[0])
	if err != nil || !slices.Equal(args, []string{"run", "--config", "/w/bound/zh-core-1/config.toml"}) {
		t.Fatalf("unexpected run command: %v / %v", args, err)
	}
}

// A manual bind rejection marks the bundle the form targeted, not whichever
// bundle refreshed progress lists first.
func TestManualBindFailureMarksTargetBundle(t *testing.T) {
	s := twoBundlePlan()
	if target := s.bindTargetBundle(); target != "zh-svg-pelican" {
		t.Fatalf("target: %q", target)
	}
	s.executeBindingFor("zh-svg-pelican", "inferyard config bind --pid 1", core.Err("cli_blocked"))
	if s.failedBundle != "zh-svg-pelican" {
		t.Fatalf("failed bundle: %q", s.failedBundle)
	}
	tt := ui.NewTester(s.conclusionView, 1120, 1000)
	if !tt.HasText("zh-svg-pelican · 失败") {
		t.Fatalf("target bundle not marked: %v", tt.Texts())
	}
	if tt.HasText("zh-core · 失败") {
		t.Fatalf("bound bundle marked failed: %v", tt.Texts())
	}
}
