package pages

import (
	"context"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type chainCall struct {
	command, model, engine string
}

type chainHarness struct {
	progress       *inferyard.WorkspaceProgress
	preflightMatch bool
	preflightModel string
	matched        map[string]bool
	keepMismatch   string
	calls          []chainCall
	failAt         int
	failResult     core.WizardResult
}

func newChainHarness() *chainHarness {
	createCore := "inferyard config create --workspace '/w' --bundle 'zh-core' --engine \"${INFERYARD_ENGINE:?Set INFERYARD_ENGINE}\" --out '/w/candidates/zh-core-1'"
	h := &chainHarness{
		preflightMatch: true,
		matched:        map[string]bool{},
		failAt:         -1,
		progress: &inferyard.WorkspaceProgress{
			Workspace:       "/w",
			NextAction:      "prepare_candidate",
			DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: ptr("/w/preflight/device-1/device-preflight.json")},
			Bundles: []inferyard.BundleProgress{
				{Bundle: "zh-core", Phase: "candidate_missing", NextAction: "prepare_candidate", NextCommand: &createCore},
				{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind", CandidatePath: ptr("/w/candidates/zh-svg-pelican-1")},
			},
		},
	}
	return h
}

func (h *chainHarness) run(t *testing.T, model, engine string) (startChainResult, *core.ApiError) {
	t.Helper()
	progress := func(context.Context) (core.ProgressResponse, *core.ApiError) {
		return core.ProgressResponse{Progress: h.progress}, nil
	}
	execute := func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		h.calls = append(h.calls, chainCall{q.Command, q.ModelName, q.EngineName})
		if len(h.calls)-1 == h.failAt {
			return h.failResult, nil
		}
		args, err := inferyard.SplitCommand(q.Command)
		if err != nil {
			return core.WizardResult{}, core.Err("invalid_request")
		}
		flags := map[string]string{}
		for i := 1; i+1 < len(args); i++ {
			if strings.HasPrefix(args[i], "--") && args[i] != "--json" {
				flags[args[i]] = args[i+1]
			}
		}
		if args[0] == "device-check" {
			if q.ModelName == "" || flags["--out"] == "" {
				return core.WizardResult{ExitCode: 2}, nil
			}
			h.preflightModel = q.ModelName
			h.preflightMatch = true
			path := flags["--out"] + "/device-preflight.json"
			h.progress.DevicePreflight.Status = "ok"
			h.progress.DevicePreflight.Path = &path
			if h.progress.NextAction == "device_check" {
				h.progress.NextAction = "prepare_candidate"
			}
			return core.WizardResult{}, nil
		}
		if len(args) < 2 || args[0] != "config" || args[1] != "create" ||
			q.EngineName == "" || flags["--out"] == "" || flags["--bundle"] == "" ||
			!h.preflightMatch || q.PreflightPath != *h.progress.DevicePreflight.Path {
			return core.WizardResult{ExitCode: 2}, nil
		}
		for _, b := range []string{"zh-core", "zh-svg-pelican"} {
			if strings.Contains(q.Command, "--bundle '"+b+"'") || strings.Contains(q.Command, "--bundle "+b+" ") {
				for i := range h.progress.Bundles {
					if h.progress.Bundles[i].Bundle == b {
						out := flags["--out"]
						h.progress.Bundles[i].CandidatePath = &out
						h.progress.Bundles[i].Phase = "bound_missing"
						if out != h.keepMismatch {
							h.matched[out] = true
						}
					}
				}
			}
		}
		return core.WizardResult{}, nil
	}
	preflight := func(preflightPath, modelName string) (bool, *core.ApiError) {
		return h.preflightMatch && (h.preflightModel == "" || h.preflightModel == modelName), nil
	}
	candidate := func(candidatePath, modelName, engineName string) (bool, bool) {
		match, known := h.matched[candidatePath]
		return match, known
	}
	return runStartChain(context.Background(), model, engine, progress, execute, preflight, candidate)
}

func TestStartChainPreservesCompletedBundleOnCombinationChange(t *testing.T) {
	h := newChainHarness()
	h.progress.Bundles[0].Phase = "run_complete"
	h.progress.Bundles[0].CandidatePath = ptr("/sealed/old-combination")
	result, e := h.run(t, "new-model.gguf", "new-engine")
	if e != nil || len(h.calls) != 1 || !strings.Contains(h.calls[0].command, "--bundle 'zh-svg-pelican'") {
		t.Fatal(e, h.calls)
	}
	if result.candidate != "/w/candidates/zh-svg-pelican-1" || h.progress.Bundles[0].Phase != "run_complete" || *h.progress.Bundles[0].CandidatePath != "/sealed/old-combination" {
		t.Fatalf("%+v %+v", result, h.progress.Bundles)
	}
}

func TestStartChainRunsDeviceCheckWhenPreflightMissing(t *testing.T) {
	h := newChainHarness()
	h.preflightMatch = false
	h.progress.NextAction = "device_check"
	h.progress.DevicePreflight = inferyard.ProgressPreflight{Status: "missing"}
	h.progress.Bundles = []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "preflight_missing", NextAction: "device_check"},
	}

	result, e := h.run(t, "Ternary-Bonsai-2.5-27B-PQ2_0.gguf", "llama-cpp-prism · llama-server")
	if e != nil {
		t.Fatalf("expected missing preflight to run device-check on demand, got err=%v calls=%+v", e, h.calls)
	}
	if len(h.calls) != 2 || !strings.HasPrefix(h.calls[0].command, "inferyard device-check ") ||
		h.calls[0].model != "Ternary-Bonsai-2.5-27B-PQ2_0.gguf" ||
		!strings.HasPrefix(h.calls[1].command, "inferyard config create ") {
		t.Fatalf("unexpected start chain calls: %+v", h.calls)
	}
	if result.candidate == "" {
		t.Fatalf("expected created candidate path, got %+v", result)
	}
}

func TestStartChainRejectsBlockedPreflight(t *testing.T) {
	h := newChainHarness()
	h.progress.DevicePreflight = inferyard.ProgressPreflight{Status: "blocked"}

	_, e := h.run(t, "Ternary-Bonsai-2.5-27B-PQ2_0.gguf", "llama-cpp-prism · llama-server")
	if e == nil || e.Code != "preflight_missing" {
		t.Fatalf("expected preflight_missing for blocked preflight, got %v", e)
	}
	if len(h.calls) != 0 {
		t.Fatalf("expected no wizard calls for blocked preflight, got %+v", h.calls)
	}
}
