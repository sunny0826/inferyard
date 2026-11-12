//go:build ui_debug && !release

package pages

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// Binding parity keeps the production chain and the in-memory bind on the same
// product contract: every incomplete bundle ends bound and the task can run.
// Each driver is compared with its own explicit expectation.
type bindParity struct {
	NextAction string
	Phases     []string
}

func TestPreviewBindingParity(t *testing.T) {
	production := runProductionBindingParity(t)
	preview, engine := runPreviewBindingParity(t)

	wantProduction := bindParity{NextAction: "run", Phases: []string{"run_missing", "run_missing"}}
	if production.NextAction != wantProduction.NextAction || !slices.Equal(production.Phases, wantProduction.Phases) {
		t.Fatalf("production bind = %+v, want %+v", production, wantProduction)
	}
	wantPreview := bindParity{NextAction: "run", Phases: []string{"run_missing", "run_missing"}}
	if preview.NextAction != wantPreview.NextAction || !slices.Equal(preview.Phases, wantPreview.Phases) {
		t.Fatalf("preview bind = %+v, want %+v", preview, wantPreview)
	}
	if engine.State != "running" || engine.Pid != 42 || engine.Endpoint != "http://127.0.0.1:48857" {
		t.Fatalf("preview bind did not leave the simulated service running: %+v", engine)
	}
}

func bindingParityProgress(root string) *inferyard.WorkspaceProgress {
	p := &inferyard.WorkspaceProgress{Workspace: root, NextAction: "bind"}
	for _, bundle := range []string{inferyard.DefaultBundleCore, inferyard.DefaultBundlePelican} {
		candidate := filepath.Join(root, "candidates", bundle)
		command := commandOf([]string{"config", "bind", "--candidate", filepath.Join(candidate, "candidate.toml"), "--out", filepath.Join(root, "bound", bundle)})
		p.Bundles = append(p.Bundles, inferyard.BundleProgress{Bundle: bundle, Phase: "bound_missing", NextAction: "bind", CandidatePath: &candidate, NextCommand: &command})
	}
	return p
}

func runProductionBindingParity(t *testing.T) bindParity {
	t.Helper()
	root := t.TempDir()
	progress := bindingParityProgress(root)
	get := func(context.Context) (core.ProgressResponse, *core.ApiError) {
		return core.ProgressResponse{Progress: progress}, nil
	}
	execute := func(_ context.Context, request core.WizardRequest) (core.WizardResult, *core.ApiError) {
		args, err := inferyard.SplitCommand(request.Command)
		if err != nil {
			t.Fatal(err)
		}
		if len(args) < 2 || args[0] != "config" || args[1] != "bind" {
			t.Fatalf("unexpected binding command %v", args)
		}
		bundle := filepath.Base(filepath.Dir(valueOf(t, args, "--candidate")))
		if valueOf(t, args, "--pid") != "6124" || valueOf(t, args, "--endpoint") != "http://127.0.0.1:8081" {
			t.Fatalf("binding lost the service identity: %v", args)
		}
		for i := range progress.Bundles {
			if progress.Bundles[i].Bundle == bundle {
				progress.Bundles[i].Phase, progress.Bundles[i].NextAction = "run_missing", "run"
			}
		}
		progress.NextAction = "run"
		return core.WizardResult{}, nil
	}
	if e := runBindingChain(context.Background(), root, bindingJobs(progress), 6124, "http://127.0.0.1:8081", get, execute, func(string) {}); e != nil {
		t.Fatal(e)
	}
	return bindParity{NextAction: progress.NextAction, Phases: bindingParityPhases(progress)}
}

func runPreviewBindingParity(t *testing.T) (bindParity, core.EngineState) {
	t.Helper()
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	s := p.state
	s.progress = bindingParityProgress("/preview/workspace")
	s.candidate = *s.progress.Bundles[0].CandidatePath
	s.tasks = nil
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
	s.bindDiscovered(42, "http://127.0.0.1:48857")
	p.advance(time.Minute)
	return bindParity{NextAction: s.progress.NextAction, Phases: bindingParityPhases(s.progress)}, s.engineState
}

func bindingParityPhases(p *inferyard.WorkspaceProgress) []string {
	phases := make([]string, 0, len(p.Bundles))
	for _, bundle := range p.Bundles {
		phases = append(phases, bundle.Phase)
	}
	return phases
}

func valueOf(t *testing.T, args []string, flag string) string {
	t.Helper()
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("missing %s in %v", flag, args)
	}
	return args[i+1]
}
