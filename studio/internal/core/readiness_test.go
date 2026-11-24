package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func quoteWizardArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestDefaultWorkspaceAndDerivedLocations(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)

	s, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	wantWorkspace := filepath.Join(home, ".inferyard", "workspace")
	wantResults := filepath.Join(wantWorkspace, "results")
	wantModels := filepath.Join(wantWorkspace, "models")
	wantReports := filepath.Join(wantWorkspace, "reports")
	wantEngines := filepath.Join(wantWorkspace, "engines")

	if !s.FirstRun() {
		t.Fatalf("expected FirstRun() == true before configuration")
	}
	initial := s.Settings()
	if got := str(initial.Effective.WorkspaceRoot); got != wantWorkspace {
		t.Fatalf("Effective.WorkspaceRoot = %q, want %q", got, wantWorkspace)
	}
	if initial.Effective.ResultsRoot != nil || initial.Effective.ModelsRoot != nil || initial.Effective.ReportsRoot != nil {
		t.Fatalf("derived roots must be nil before workspace is configured: %+v", initial.Effective)
	}
	if roots := EngineRoots(wantWorkspace); !slices.Equal(roots, []string{wantEngines}) {
		t.Fatalf("EngineRoots = %v, want [%q]", roots, wantEngines)
	}

	provisioned, apiErr := s.Provision()
	if apiErr != nil {
		t.Fatalf("Provision failed: %+v", apiErr)
	}
	if s.FirstRun() {
		t.Fatalf("expected FirstRun() == false after Provision")
	}
	if str(provisioned.Settings.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot = %q, want %q", str(provisioned.Settings.WorkspaceRoot), wantWorkspace)
	}
	if str(provisioned.Effective.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot = %q, want %q", str(provisioned.Effective.ResultsRoot), wantResults)
	}
	if str(provisioned.Effective.ModelsRoot) != wantModels {
		t.Fatalf("ModelsRoot = %q, want %q", str(provisioned.Effective.ModelsRoot), wantModels)
	}
	if str(provisioned.Effective.ReportsRoot) != wantReports {
		t.Fatalf("ReportsRoot = %q, want %q", str(provisioned.Effective.ReportsRoot), wantReports)
	}
	for _, dir := range []string{wantWorkspace, wantResults, wantModels, wantEngines} {
		st, statErr := os.Stat(dir)
		if statErr != nil || !st.IsDir() {
			t.Fatalf("Provision did not create %q: %v", dir, statErr)
		}
	}
	r := s.Readiness()
	if !r.Workspace.Exists || r.Workspace.Initialized {
		t.Fatalf("Readiness.Workspace after Provision = %+v, want exists/not initialized", r.Workspace)
	}
	if !r.Results.Exists || !r.Models.Exists {
		t.Fatalf("Readiness after Provision = %+v, want results and models to exist", r)
	}
}

func TestWizardInitAndConfigCreateUseDerivedWorkspaceDirs(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	wantWorkspace := filepath.Join(home, ".inferyard", "workspace")
	wantResults := filepath.Join(wantWorkspace, "results")
	wantModels := filepath.Join(wantWorkspace, "models")
	wantReports := filepath.Join(wantWorkspace, "reports")

	var mu sync.Mutex
	var captured [][]string
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			mu.Lock()
			captured = append(captured, append([]string(nil), args...))
			mu.Unlock()
			if len(args) > 0 && args[0] == "init" {
				out := args[2]
				_ = os.MkdirAll(filepath.Join(out, "results"), 0700)
				_ = os.MkdirAll(filepath.Join(out, "reports"), 0700)
				_ = os.WriteFile(filepath.Join(out, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600)
				return inferyard.Execution{}, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				raw, _ := json.Marshal(inferyard.WorkspaceProgress{
					Kind:            "progress",
					ProgressVersion: 2,
					Workspace:       wantWorkspace,
					DevicePreflight: inferyard.ProgressPreflight{Status: "missing"},
					Report:          inferyard.ProgressReport{Status: "missing", Runs: []string{}},
					NextAction:      "device_check",
					Bundles:         []inferyard.BundleProgress{},
				})
				return inferyard.Execution{Stdout: raw}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	rawCLI, _ := json.Marshal(exe)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": rawCLI}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out " + quoteWizardArg(wantWorkspace)}); apiErr != nil {
		t.Fatalf("WizardExecute init: %+v", apiErr)
	}
	if _, apiErr := s.Provision(); apiErr != nil {
		t.Fatal(apiErr)
	}
	got := s.Settings()
	if str(got.Settings.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot after init = %q, want %q", str(got.Settings.WorkspaceRoot), wantWorkspace)
	}
	if str(got.Effective.ResultsRoot) != wantResults || str(got.Effective.ModelsRoot) != wantModels || str(got.Effective.ReportsRoot) != wantReports {
		t.Fatalf("derived effective roots = %+v", got.Effective)
	}

	// First-time init of default workspace must trigger asset/device refresh with derived models root.
	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, args := range captured {
			if len(args) >= 4 && args[0] == "device-check" && slices.Contains(args, wantModels) {
				return true
			}
		}
		return false
	})

	// config create without --results injects derived workspace/results.
	candOut := filepath.Join(wantWorkspace, "candidates", "zh-core-1")
	if _, apiErr := s.WizardExecute(context.Background(), WizardRequest{
		Command: "inferyard config create --workspace " + quoteWizardArg(wantWorkspace) + " --bundle zh-core --out " + quoteWizardArg(candOut),
	}); apiErr != nil {
		t.Fatalf("WizardExecute config create: %+v", apiErr)
	}
	mu.Lock()
	var configArgs []string
	for i := len(captured) - 1; i >= 0; i-- {
		if len(captured[i]) >= 2 && captured[i][0] == "config" && captured[i][1] == "create" {
			configArgs = captured[i]
			break
		}
	}
	mu.Unlock()
	foundResults := ""
	for i := 0; i+1 < len(configArgs); i++ {
		if configArgs[i] == "--results" {
			foundResults = configArgs[i+1]
		}
	}
	if foundResults != wantResults {
		t.Fatalf("config create args %v did not inject --results %q", configArgs, wantResults)
	}

	// Progress omits --results and --reports when derived inside workspace.
	if _, apiErr := s.Progress(context.Background()); apiErr != nil {
		t.Fatalf("Progress failed: %+v", apiErr)
	}
	mu.Lock()
	var progArgs []string
	for i := len(captured) - 1; i >= 0; i-- {
		if len(captured[i]) >= 1 && captured[i][0] == "progress" {
			progArgs = captured[i]
			break
		}
	}
	mu.Unlock()
	if slices.Contains(progArgs, "--results") || slices.Contains(progArgs, "--reports") {
		t.Fatalf("progress args %v should not pass external --results/--reports for unified workspace", progArgs)
	}
}

func TestLegacySettingsIgnoredAndDeprecatedKeysRejected(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "custom-ws")
	legacyResults := filepath.Join(t.TempDir(), "old-results")
	legacyModels := filepath.Join(t.TempDir(), "old-models")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	legacyJSON, _ := json.Marshal(map[string]any{
		"workspace_root":       ws,
		"results_root":         legacyResults,
		"models_root":          legacyModels,
		"model_download_name":  "custom-model-dir",
		"engine_download_name": "custom-engine-dir",
	})
	if err := os.WriteFile(settingsPath, legacyJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	got := s.Settings()
	if str(got.Effective.ResultsRoot) != filepath.Join(ws, "results") {
		t.Fatalf("ResultsRoot = %q, want %q", str(got.Effective.ResultsRoot), filepath.Join(ws, "results"))
	}
	if str(got.Effective.ModelsRoot) != filepath.Join(ws, "models") {
		t.Fatalf("ModelsRoot = %q, want %q", str(got.Effective.ModelsRoot), filepath.Join(ws, "models"))
	}
	if str(got.Effective.ReportsRoot) != filepath.Join(ws, "reports") {
		t.Fatalf("ReportsRoot = %q, want %q", str(got.Effective.ReportsRoot), filepath.Join(ws, "reports"))
	}

	for _, key := range []string{"results_root", "models_root", "model_download_name", "engine_download_name"} {
		val, _ := json.Marshal(filepath.Join(t.TempDir(), "x"))
		if key == "model_download_name" || key == "engine_download_name" {
			val, _ = json.Marshal("custom")
		}
		if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{key: val}); apiErr == nil || apiErr.Code != "invalid_request" {
			t.Fatalf("UpdateSettings(%q) = %+v, want invalid_request", key, apiErr)
		}
	}

	// Saving workspace_root rewrites settings.json without legacy keys.
	nextWS := filepath.Join(t.TempDir(), "next-ws")
	rawNext, _ := json.Marshal(nextWS)
	updated, apiErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawNext})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(workspace_root): %+v", apiErr)
	}
	if str(updated.Effective.ResultsRoot) != filepath.Join(nextWS, "results") || str(updated.Effective.ModelsRoot) != filepath.Join(nextWS, "models") {
		t.Fatalf("updated derived roots = %+v", updated.Effective)
	}
	persisted, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var rawMap map[string]any
	if err := json.Unmarshal(persisted, &rawMap); err != nil {
		t.Fatal(err)
	}
	for _, legacyKey := range []string{"results_root", "models_root", "model_download_name", "engine_download_name"} {
		if _, exists := rawMap[legacyKey]; exists {
			t.Fatalf("persisted settings.json still contains legacy key %q: %s", legacyKey, persisted)
		}
	}

	// BoundPaths mode also rejects deprecated keys with invalid_request and respects bound dirs.
	boundRoot, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boundCLI := filepath.Join(boundRoot, "cli")
	if err := os.WriteFile(boundCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	boundPaths := &BoundPaths{
		CLI:       boundCLI,
		Workspace: filepath.Join(boundRoot, "ws"),
		Results:   filepath.Join(boundRoot, "ws", "results"),
		Models:    filepath.Join(boundRoot, "shared", "models"),
		Engines:   filepath.Join(boundRoot, "shared", "engines"),
		Settings:  filepath.Join(boundRoot, "settings.json"),
	}
	bs, err := New(context.Background(), Options{
		Paths:         boundPaths,
		HostStatePath: filepath.Join(boundRoot, "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bs.Shutdown(context.Background())
	bgot := bs.Settings().Effective
	if str(bgot.ModelsRoot) != boundPaths.Models || str(bgot.ResultsRoot) != boundPaths.Results {
		t.Fatalf("BoundPaths effective dirs = %+v", bgot)
	}
	for _, key := range []string{"results_root", "models_root", "model_download_name", "engine_download_name"} {
		val, _ := json.Marshal("x")
		if _, apiErr := bs.UpdateSettings(map[string]json.RawMessage{key: val}); apiErr == nil || apiErr.Code != "invalid_request" {
			t.Fatalf("BoundPaths UpdateSettings(%q) = %+v, want invalid_request", key, apiErr)
		}
	}
	// Combined patch with bound key + deprecated key must deterministically return invalid_request.
	wsPatch, _ := json.Marshal(filepath.Join(boundRoot, "other"))
	oldPatch, _ := json.Marshal(filepath.Join(boundRoot, "old-results"))
	for range 10 {
		if _, apiErr := bs.UpdateSettings(map[string]json.RawMessage{
			"workspace_root": wsPatch,
			"results_root":   oldPatch,
		}); apiErr == nil || apiErr.Code != "invalid_request" {
			t.Fatalf("BoundPaths combined deprecated patch = %+v, want invalid_request", apiErr)
		}
	}
}

func TestWorkspaceSwitchIsolationAndOrdering(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	defaultWS := filepath.Join(home, ".inferyard", "workspace")
	// Pre-populate a binary in default ~/.inferyard/workspace/engines before workspace is configured.
	preEngine := filepath.Join(defaultWS, "engines", "old", engineBinaryName())
	if err := os.MkdirAll(filepath.Dir(preEngine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preEngine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Finding 2: Unconfigured workspace must not scan ~/.inferyard/workspace/engines.
	if reg := s.Registry(); len(reg.Engines) != 0 {
		t.Fatalf("unconfigured workspace leaked engines: %+v", reg.Engines)
	}

	// Finding 1: First-time saving default workspace must not drop an active engine on that same default workspace.
	s.mu.Lock()
	s.engine = &engineEntry{state: EngineState{State: "running"}}
	s.mu.Unlock()
	rawDefault, _ := json.Marshal(defaultWS)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawDefault}); apiErr != nil {
		t.Fatalf("first-time save of default workspace failed: %+v", apiErr)
	}
	s.mu.Lock()
	stillHasEngine := s.engine != nil
	s.engine = nil
	s.mu.Unlock()
	if !stillHasEngine {
		t.Fatalf("saving default workspace cleared running engine on the same workspace")
	}

	// Finding 6: Switching workspace_root A -> B while keeping A/models in extra_models_roots must be order-independent.
	wsA := filepath.Join(t.TempDir(), "ws-a")
	wsB := filepath.Join(t.TempDir(), "ws-b")
	rawA, _ := json.Marshal(wsA)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawA}); apiErr != nil {
		t.Fatalf("set wsA: %+v", apiErr)
	}
	modelsA := filepath.Join(wsA, "models")
	rawB, _ := json.Marshal(wsB)
	rawExtras, _ := json.Marshal([]string{modelsA})
	for range 10 {
		resp, apiErr := s.UpdateSettings(map[string]json.RawMessage{
			"workspace_root":     rawB,
			"extra_models_roots": rawExtras,
		})
		if apiErr != nil {
			t.Fatalf("switch wsA->wsB with extra_models_roots: %+v", apiErr)
		}
		if !slices.Equal(resp.Settings.ExtraModelsRoots, []string{modelsA}) {
			t.Fatalf("ExtraModelsRoots = %v, want [%q]", resp.Settings.ExtraModelsRoots, modelsA)
		}
		// Reset back to wsA for next iteration.
		if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawA}); apiErr != nil {
			t.Fatal(apiErr)
		}
	}

	// Finding 5: History() during rebuilding after workspace switch must not return old ResultsRoot/runs.
	s.mu.Lock()
	s.history = RunHistoryResponse{ResultsRoot: ptr(filepath.Join(wsA, "results")), StandaloneRuns: []RunSummary{{RunId: "old-run"}}}
	s.mu.Unlock()
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawB}); apiErr != nil {
		t.Fatal(apiErr)
	}
	h, apiErr := s.History()
	if apiErr != nil {
		t.Fatalf("History: %+v", apiErr)
	}
	if str(h.ResultsRoot) != filepath.Join(wsB, "results") || len(h.StandaloneRuns) != 0 {
		t.Fatalf("History leaked old workspace cache: %+v", h)
	}

	// Finding 3: Active download in wsA must not dedup a download requested in wsB.
	source := "https://huggingface.co/o/r/blob/0123456789012345678901234567890123456789/model.gguf"
	modelsB := filepath.Join(wsB, "models")
	s.mu.Lock()
	s.downloads = append(s.downloads, &downloadEntry{
		Task: DownloadTask{
			Kind:      "model",
			TaskId:    "dl-a",
			State:     "downloading",
			SourceUrl: source,
			OutDir:    filepath.Join(modelsA, "model"),
		},
	})
	_, foundInB := s.activeDownloadLocked("model", modelsB, source, "")
	_, foundInA := s.activeDownloadLocked("model", modelsA, source, "")
	s.mu.Unlock()
	if foundInB {
		t.Fatalf("activeDownloadLocked matched wsA download for wsB root")
	}
	if !foundInA {
		t.Fatalf("activeDownloadLocked did not match wsA download for wsA root")
	}

	// Finding 4: Retrying a failed PolyStrata setup after switching wsA -> wsB must use wsB's models/polystrata-data.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var gotDataDir string
	var mu sync.Mutex
	psSvc, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--data-dir" {
					mu.Lock()
					gotDataDir = args[i+1]
					mu.Unlock()
				}
			}
			return inferyard.Execution{ExitCode: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer psSvc.Shutdown(context.Background())
	rawCLI, _ := json.Marshal(exe)
	if _, apiErr := psSvc.UpdateSettings(map[string]json.RawMessage{"inferyard_path": rawCLI, "workspace_root": rawB}); apiErr != nil {
		t.Fatal(apiErr)
	}
	psSvc.mu.Lock()
	psSvc.downloads = append(psSvc.downloads, &downloadEntry{
		Task: DownloadTask{
			Kind:      "polystrata",
			TaskId:    "ps-failed-a",
			State:     "failed",
			SourceUrl: polystrataDefaultSourceURL,
			OutName:   "polystrata",
			OutDir:    filepath.Join(wsA, "engines", "polystrata"),
		},
		polystrata: &PolystrataSetupRequest{Family: "polystrata", Model: "qwen", Context: 4096, Backend: "metal"},
		dataDir:    filepath.Join(modelsA, "polystrata-data"),
	})
	psSvc.mu.Unlock()
	retried, apiErr := psSvc.RetryDownload("ps-failed-a")
	if apiErr != nil {
		t.Fatalf("RetryDownload(polystrata): %+v", apiErr)
	}
	wantDataDir := filepath.Join(modelsB, "polystrata-data")
	wait(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotDataDir != ""
	})
	if gotDataDir != wantDataDir {
		t.Fatalf("retried PolyStrata --data-dir = %q, want %q (outDir=%q)", gotDataDir, wantDataDir, retried.OutDir)
	}

	// Cross-workspace PolyStrata CreatePolystrataSetup and RetryDownload must return polystrata_setup_active.
	psReq := PolystrataSetupRequest{Family: "polystrata", Model: "qwen", Context: 4096, Backend: "metal"}
	psSvc.mu.Lock()
	psSvc.downloads = []*downloadEntry{
		{
			Task: DownloadTask{
				Kind:      "polystrata",
				TaskId:    "ps-active-a",
				State:     "downloading",
				SourceUrl: polystrataDefaultSourceURL,
				OutName:   "polystrata",
				OutDir:    filepath.Join(wsA, "engines", "polystrata"),
			},
			polystrata: &psReq,
		},
		{
			Task: DownloadTask{
				Kind:      "polystrata",
				TaskId:    "ps-failed-b",
				State:     "failed",
				SourceUrl: polystrataDefaultSourceURL,
				OutName:   "polystrata",
				OutDir:    filepath.Join(wsB, "engines", "polystrata"),
			},
			polystrata: &psReq,
		},
	}
	psSvc.mu.Unlock()
	if _, apiErr := psSvc.CreatePolystrataSetup(psReq); apiErr == nil || apiErr.Code != "conflict" || apiErr.Details["reason"] != "polystrata_setup_active" {
		t.Fatalf("cross-workspace CreatePolystrataSetup = %+v, want polystrata_setup_active", apiErr)
	}
	if _, apiErr := psSvc.RetryDownload("ps-failed-b"); apiErr == nil || apiErr.Code != "conflict" || apiErr.Details["reason"] != "polystrata_setup_active" {
		t.Fatalf("cross-workspace RetryDownload(polystrata) = %+v, want polystrata_setup_active", apiErr)
	}
}
