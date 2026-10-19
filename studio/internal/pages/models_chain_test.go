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

// chainHarness drives runStartChain with a mutable fake progress; every
// WizardExecute advances the fake the way the real CLI would.
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
			h.progress.DevicePreflight.Path = &path
			return core.WizardResult{}, nil
		}
		if len(args) < 2 || args[0] != "config" || args[1] != "create" ||
			q.EngineName == "" || flags["--out"] == "" || flags["--bundle"] == "" ||
			!h.preflightMatch || q.PreflightPath != *h.progress.DevicePreflight.Path {
			return core.WizardResult{ExitCode: 2}, nil
		}
		// A successful config create freezes the selected combination at its --out.
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

func TestStartChainSkipsWorkWhenCombinationMatches(t *testing.T) {
	h := newChainHarness()
	h.matched["/w/candidates/zh-core-1"] = true
	h.matched["/w/candidates/zh-svg-pelican-1"] = true
	h.progress.Bundles[0].Phase, h.progress.Bundles[0].NextAction = "bound_missing", "bind"
	h.progress.Bundles[0].CandidatePath = ptr("/w/candidates/zh-core-1")
	candidate, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e != nil || candidate.candidate != "/w/candidates/zh-core-1" {
		t.Fatalf("%+v %v", candidate, e)
	}
	if len(h.calls) != 0 {
		t.Fatal("matching combination must not run commands", h.calls)
	}
}

func TestStartChainCommandSequence(t *testing.T) {
	h := newChainHarness()
	h.preflightMatch = false
	candidate, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e != nil {
		t.Fatal(e)
	}
	if candidate.candidate != "/w/candidates/zh-core-1" {
		t.Fatal(candidate)
	}
	if len(h.calls) != 3 {
		t.Fatalf("calls: %+v", h.calls)
	}
	// 1. The changed model reruns device-check first, with the model name.
	if !strings.Contains(h.calls[0].command, "device-check") || !strings.Contains(h.calls[0].command, "/w/preflight/device-1'") {
		t.Fatal(h.calls[0])
	}
	if h.calls[0].model != "model.gguf" || h.calls[0].engine != "" {
		t.Fatalf("%+v", h.calls[0])
	}
	// 2. The candidate_missing bundle reuses its progress next_command.
	if h.calls[1].command != *h.progress.Bundles[0].NextCommand && !strings.Contains(h.calls[1].command, "config create") {
		t.Fatal(h.calls[1])
	}
	if !strings.Contains(h.calls[1].command, "${INFERYARD_ENGINE") || h.calls[1].engine != "engine-2 · llama-server" {
		t.Fatalf("next_command must come from progress: %+v", h.calls[1])
	}
	// 3. The mismatched existing candidate gets a constructed config create.
	if !strings.Contains(h.calls[2].command, "config create") || !strings.Contains(h.calls[2].command, "--bundle 'zh-svg-pelican'") ||
		!strings.Contains(h.calls[2].command, "--out '/w/candidates/zh-svg-pelican-") || h.calls[2].engine != "engine-2 · llama-server" {
		t.Fatalf("%+v", h.calls[2])
	}
}

func TestStartChainStopsOnBlockedDeviceCheck(t *testing.T) {
	h := newChainHarness()
	h.preflightMatch = false
	h.failAt = 0
	h.failResult = core.WizardResult{ExitCode: 2, Status: inferyard.Blocked, Limitations: []string{"insufficient_host_memory"}}
	_, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e == nil || !strings.Contains(e.Message, "主机可用内存不足") {
		t.Fatal(e)
	}
	if len(h.calls) != 1 {
		t.Fatal("chain continued after a blocked device-check", h.calls)
	}
}

func TestStartChainStopsOnFailedCreate(t *testing.T) {
	h := newChainHarness()
	h.failAt = 0
	h.failResult = core.WizardResult{ExitCode: 2, Status: inferyard.Blocked, Limitations: []string{"invalid_library_manifest"}}
	_, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e == nil || e.Message != human("invalid_library_manifest") {
		t.Fatal(e)
	}
	if len(h.calls) != 1 {
		t.Fatal("second bundle created after the first failed", h.calls)
	}
}

func TestStartChainReverifiesCandidates(t *testing.T) {
	h := newChainHarness()
	h.matched["/w/candidates/zh-core-1"] = true
	h.progress.Bundles[0].Phase, h.progress.Bundles[0].NextAction = "bound_missing", "bind"
	h.progress.Bundles[0].CandidatePath = ptr("/w/candidates/zh-core-1")
	// The pelican create "succeeds" but the re-read candidate still mismatches.
	h.keepMismatch = "/w/candidates/zh-svg-pelican-1"
	_, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e == nil || e.Code != "candidate_mismatch" {
		t.Fatal(e)
	}
}

func TestStartChainRequiresPreflight(t *testing.T) {
	h := newChainHarness()
	h.progress.DevicePreflight = inferyard.ProgressPreflight{Status: "missing"}
	_, e := h.run(t, "model.gguf", "engine-2 · llama-server")
	if e == nil || e.Code != "preflight_missing" || len(h.calls) != 0 {
		t.Fatal(e, h.calls)
	}
}

func TestChainFailureTranslatesDeviceReasons(t *testing.T) {
	e := chainFailure(core.WizardResult{ExitCode: 2, Status: inferyard.Blocked, Limitations: []string{"no_local_model"}})
	if !strings.Contains(e.Message, "未发现本地 GGUF 模型") {
		t.Fatal(e)
	}
	e = chainFailure(core.WizardResult{ExitCode: 2, Status: inferyard.Blocked, Limitations: []string{"startup_arguments_mismatch"}})
	if e.Message != human("startup_arguments_mismatch") {
		t.Fatal(e)
	}
}

func TestStartChainReturnsFreshProgressAfterCreate(t *testing.T) {
	h := newChainHarness()
	h.progress.Bundles = h.progress.Bundles[:1]
	result, e := h.run(t, "model.gguf", "engine")
	if e != nil || len(h.calls) != 1 {
		t.Fatal(e, h.calls)
	}
	if result.workspace != "/w" || result.candidate != "/w/candidates/zh-core-1" {
		t.Fatalf("fresh progress not returned: %+v", result)
	}
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

func TestStartChainAllCompletedRejectsNewCombination(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "mismatch", false: "unknown"}[known], func(t *testing.T) {
			h := newChainHarness()
			h.preflightMatch = false
			for i := range h.progress.Bundles {
				b := &h.progress.Bundles[i]
				b.Phase = "run_complete"
				b.CandidatePath = ptr("/sealed/" + b.Bundle)
				if known {
					h.matched[*b.CandidatePath] = false
				}
			}
			result, e := h.run(t, "new-model.gguf", "new-engine")
			if e == nil || e.Code != "candidate_mismatch" || result != (startChainResult{}) {
				t.Fatalf("result=%+v error=%v", result, e)
			}
			if len(h.calls) != 1 || !strings.Contains(h.calls[0].command, "device-check") {
				t.Fatalf("only device-check should execute: %+v", h.calls)
			}
		})
	}
}

func TestStartChainAllCompletedReusesMatchingCombination(t *testing.T) {
	for _, firstMatches := range []bool{true, false} {
		t.Run(map[bool]string{true: "first_matches", false: "second_matches"}[firstMatches], func(t *testing.T) {
			h := newChainHarness()
			for i := range h.progress.Bundles {
				b := &h.progress.Bundles[i]
				b.Phase = "run_complete"
				b.CandidatePath = ptr("/sealed/" + b.Bundle)
				h.matched[*b.CandidatePath] = i > 0 || firstMatches
			}
			want := *h.progress.Bundles[0].CandidatePath
			if !firstMatches {
				want = *h.progress.Bundles[1].CandidatePath
			}
			result, e := h.run(t, "model.gguf", "engine")
			if e != nil || result.candidate != want || result.workspace != h.progress.Workspace {
				t.Fatalf("result=%+v error=%v", result, e)
			}
			if len(h.calls) != 0 {
				t.Fatalf("sealed candidates must not be recreated: %+v", h.calls)
			}
		})
	}
}

func TestStartChainBlockedRecommendationIsAMiss(t *testing.T) {
	p := &inferyard.WorkspaceProgress{Workspace: t.TempDir(), DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: ptr("preflight")}, Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "bound_missing", CandidatePath: ptr("candidate")}}}
	matches, calls := 0, 0
	_, e := runStartChain(context.Background(), "chosen.gguf", "engine",
		func(context.Context) (core.ProgressResponse, *core.ApiError) {
			return core.ProgressResponse{Progress: p}, nil
		},
		func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			calls++
			if !strings.Contains(q.Command, "device-check") || q.ModelName != "chosen.gguf" {
				t.Fatal(q)
			}
			return core.WizardResult{}, nil
		}, func(string, string) (bool, *core.ApiError) {
			matches++
			if matches == 1 {
				return false, core.Err("device_preflight_blocked")
			}
			return true, nil
		},
		func(string, string, string) (bool, bool) { return true, true })
	if e != nil || calls != 1 || matches != 2 {
		t.Fatal(e, calls, matches)
	}
}

func TestStartChainFreshTaskRecreatesSealedSelection(t *testing.T) {
	root := t.TempDir()
	p := &inferyard.WorkspaceProgress{Workspace: root, DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: ptr("preflight")}, Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete", CandidatePath: ptr("old")}, {Bundle: "zh-svg-pelican", Phase: "candidate_missing"}}}
	calls := 0
	result, e := runStartChain(context.Background(), "chosen", "engine",
		func(context.Context) (core.ProgressResponse, *core.ApiError) {
			return core.ProgressResponse{Progress: selectedProgress(p, []string{"zh-core"})}, nil
		},
		func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			calls++
			args, _ := inferyard.SplitCommand(q.Command)
			if !strings.Contains(q.Command, "zh-core") || strings.Contains(q.Command, "zh-svg-pelican") {
				t.Fatal(q)
			}
			for i, a := range args {
				if a == "--out" {
					p.Bundles[0].CandidatePath = &args[i+1]
				}
			}
			p.Bundles[0].Phase = "bound_missing"
			return core.WizardResult{}, nil
		}, func(string, string) (bool, *core.ApiError) { return true, nil }, func(string, string, string) (bool, bool) { return true, true }, true)
	if e != nil || calls != 1 || result.candidate == "old" || p.Bundles[1].Phase != "candidate_missing" {
		t.Fatal(result, e, calls)
	}
}
