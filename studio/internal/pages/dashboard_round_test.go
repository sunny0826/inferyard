package pages

import (
	"context"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func runStepState(bound string) *viewState {
	s := testState()
	s.task.stage = "run"
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/w", NextAction: "run", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_missing", NextAction: "run", BoundPath: ptr(bound), NextCommand: ptr("inferyard run --config " + bound)},
	}}
	return &s
}

func TestRunStepRequiresServiceOrVerifiedBinding(t *testing.T) {
	s := runStepState("/w/bound/zh-svg-pelican-1/config.toml")
	s.progress.Bundles[0].BoundPath = nil
	s.progress.Bundles[0].Phase = "bound_missing"
	s.progress.Bundles[0].NextAction = "bind"
	b := s.active()
	if b == nil || s.runServiceReady(*b) {
		t.Fatalf("unbound bundle is runnable: %+v", b)
	}
	if e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError) {
		t.Fatal("unbound run executed")
		return core.WizardResult{}, nil
	}); e == nil {
		t.Fatal("unbound run accepted")
	}
}
