package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportCacheInvalidationAndCloneIsolation(t *testing.T) {
	ws := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())
	wsRaw, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": wsRaw}); apiErr != nil {
		t.Fatal(apiErr)
	}
	reportsRoot := filepath.Join(ws, "reports")
	tps1 := 42.0
	t1 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	writeStudioReportFixture(t, reportsRoot, "rep-1", t1, []map[string]any{
		makeReportRunFixture("run-1", "Model-A", "/opt/llama-server", "prism_llama_server_v1", &tps1),
	})
	svc.recordReportExclusivity("rep-1", []string{"run-1"})
	svc.recordRunExclusivity("run-1", ExclusivityExclusive)

	if _, ok := svc.CachedReportIndex("rep-1"); ok {
		t.Fatal("expected cold cache before first Reports/ReportIndex read")
	}

	_, summaries, apiErr := svc.Reports()
	if apiErr != nil || len(summaries) != 1 {
		t.Fatalf("Reports failed: err=%v summaries=%+v", apiErr, summaries)
	}
	cached, ok := svc.CachedReportIndex("rep-1")
	if !ok || cached == nil || len(cached.Runs) != 1 || cached.Runs[0].Model != "Model-A" {
		t.Fatalf("expected Reports() to warm CachedReportIndex, got ok=%v cached=%+v", ok, cached)
	}

	// Mutating returned ReportIndex must not mutate cached copy.
	cached.Runs[0].Model = "Mutated-In-Memory"
	cached.Runs = nil
	again, apiErr := svc.ReportIndex("rep-1")
	if apiErr != nil || len(again.Runs) != 1 || again.Runs[0].Model != "Model-A" {
		t.Fatalf("cache leaked caller mutation: err=%v again=%+v", apiErr, again)
	}

	// Full directory path lookup in ReportExclusivity hits report ID without failing.
	fullDir := filepath.Join(reportsRoot, "rep-1")
	if got := svc.ReportExclusivity(fullDir); got != ExclusivityExclusive {
		t.Fatalf("ReportExclusivity(fullDir) = %q, want %q", got, ExclusivityExclusive)
	}

	// Updating studio.json on disk invalidates cache immediately.
	tps2 := 88.5
	t2 := t1.Add(2 * time.Second)
	writeStudioReportFixture(t, reportsRoot, "rep-1", t2, []map[string]any{
		makeReportRunFixture("run-2", "Model-B", "/opt/llama-server", "prism_llama_server_v1", &tps2),
	})
	updated, apiErr := svc.ReportIndex("rep-1")
	if apiErr != nil || updated.Runs[0].Model != "Model-B" {
		t.Fatalf("expected updated studio.json to invalidate cache, got err=%v model=%v", apiErr, updated)
	}
}

func TestHistoryScanCacheTracksDirtyAndEventUpdates(t *testing.T) {
	s := testService(t, nil)
	ws, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ws, "results")
	hostFile := filepath.Join(ws, "host-dirty.json")
	s.settings.WorkspaceRoot = &ws
	s.hostStatePath = hostFile

	writeFinishedRun(t, root, "plan_finished")
	h1, apiErr := s.History()
	if apiErr != nil || len(h1.StandaloneRuns) != 1 || h1.StandaloneRuns[0].Status != "complete" {
		t.Fatalf("initial History mismatch: err=%v h=%+v", apiErr, h1)
	}

	// Host dirty marker change must update Status even when run files are unchanged (cache hit).
	dirtyDoc := `{"schema_version":1,"dirty":true,"dirty_token":"tok","run_id":"plan_finished","kind":"run","server_pid":123,"process_start_ticks":456}`
	if err := os.WriteFile(hostFile, []byte(dirtyDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	// Modify stop_reason so it isn't complete? Wait: RunStatus checks dirty BEFORE !sealed || completeness != "complete"!
	hDirty, apiErr := s.History()
	if apiErr != nil || len(hDirty.StandaloneRuns) != 1 || hDirty.StandaloneRuns[0].Status != "dirty" {
		t.Fatalf("expected dirty status on cached run when host marker turns dirty, got err=%v h=%+v", apiErr, hDirty)
	}

	// Clearing host dirty marker restores complete status.
	if err := os.Remove(hostFile); err != nil {
		t.Fatal(err)
	}
	hClean, apiErr := s.History()
	if apiErr != nil || hClean.StandaloneRuns[0].Status != "complete" {
		t.Fatalf("expected complete status after clearing host marker, got err=%v h=%+v", apiErr, hClean)
	}
}

func TestStartupCachedScopesCacheWhileAdmissionRevalidates(t *testing.T) {
	ws, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(ws, "models", "m.gguf")
	engineFile := filepath.Join(ws, "engines", "llama-server")
	candDir := filepath.Join(ws, "candidates", "zh-core-1")
	for _, dir := range []string{filepath.Dir(modelFile), filepath.Dir(engineFile), candDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(modelFile, []byte("gguf-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engineFile, []byte("bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	candFile := filepath.Join(candDir, "candidate.toml")
	if err := os.WriteFile(candFile, []byte("bundle = \"zh-core\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	describeCalls := 0
	hash := strings.Repeat("b", 64)
	execFn := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) == 1 && args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard 99.0.0 source=release\n")}, nil
		}
		if len(args) >= 2 && args[0] == "config" && args[1] == "describe" {
			describeCalls++
			details := map[string]any{
				"schema_version": 3, "definition": "engine-description.v1", "adapter": "llama_cpp_b11146_v1", "model_requests_sent": 0,
				"launch":     map[string]any{"kind": "direct_process", "argv": []string{engineFile, "--port", "8080"}, "working_directory": nil, "environment": nil, "ownership": "operator"},
				"assets":     map[string]any{"engine": map[string]any{"path": engineFile, "sha256": hash}, "model": map[string]any{"path": modelFile, "kind": "gguf", "content_sha256": hash}, "template": map[string]any{"path": "/t.jinja", "sha256": hash}},
				"endpoint":   map[string]any{"url": "http://127.0.0.1:8080", "server_pid": 1, "process_start_ticks": 1},
				"conditions": map[string]any{"context_size": 4096},
				"generation": map[string]any{"max_tokens": 32, "temperature": 0},
			}
			raw, _ := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
			return inferyard.Execution{Stdout: raw}, nil
		}
		return inferyard.Execution{ExitCode: 2}, nil
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(UserSettings{WorkspaceRoot: &ws, InferyardPath: &cliBin})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(context.Background(), Options{SettingsPath: settingsPath, Execute: execFn})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	if _, apiErr := svc.StartupCached(candFile); apiErr != nil {
		t.Fatalf("first StartupCached failed: %+v", apiErr)
	}
	if _, apiErr := svc.StartupCached(candFile); apiErr != nil {
		t.Fatalf("second StartupCached failed: %+v", apiErr)
	}
	if describeCalls != 1 {
		t.Fatalf("expected StartupCached to invoke CLI once, got %d", describeCalls)
	}

	// Startup and DescribeCandidate (used by StartEngine and run admission) must always
	// invoke CLI directly so directory-model asset manifest checks are never bypassed.
	if _, apiErr := svc.Startup(candFile); apiErr != nil {
		t.Fatalf("Startup failed: %+v", apiErr)
	}
	if describeCalls != 2 {
		t.Fatalf("expected uncached Startup to revalidate via CLI, got %d calls", describeCalls)
	}

	// Modifying referenced model file invalidates StartupCached display cache.
	if err := os.WriteFile(modelFile, []byte("gguf-v2-larger"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := svc.StartupCached(candFile); apiErr != nil {
		t.Fatalf("StartupCached after model change failed: %+v", apiErr)
	}
	if describeCalls != 3 {
		t.Fatalf("expected model asset change to invalidate StartupCached, got %d calls", describeCalls)
	}
}

func TestReadContextSpeedSkipsCachingPartialAndCachesComplete(t *testing.T) {
	ws, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	batchDir := filepath.Join(ws, "results", "speed-batch-1")
	if err := os.MkdirAll(batchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(batchDir, "batch.json"), []byte(`{"schema_version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}

	rawContract, err := os.ReadFile("../inferyard/testdata/context_speed_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string         `json:"name"`
		ExitCode int            `json:"exit_code"`
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(rawContract, &cases); err != nil {
		t.Fatal(err)
	}
	fixture := func(name string) (int, []byte) {
		for _, c := range cases {
			if c.Name == name {
				c.Response["evidence_dir"] = batchDir
				raw, _ := json.Marshal(c.Response)
				return c.ExitCode, raw
			}
		}
		t.Fatalf("contract fixture %q not found", name)
		return 0, nil
	}

	mode := "zero_runs"
	readCalls := 0
	execFn := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 1 && args[0] == "speed-results" {
			readCalls++
			code, raw := fixture(mode)
			return inferyard.Execution{ExitCode: code, Stdout: raw}, nil
		}
		return inferyard.Execution{ExitCode: 2}, nil
	}

	svc := testService(t, execFn)
	svc.settings.WorkspaceRoot = &ws
	task := &ContextSpeedTask{Batch: &SpeedBatchRef{Path: batchDir}}

	partial, apiErr := svc.readContextSpeed(context.Background(), "/bin/inferyard", task)
	if apiErr != nil || partial.Execution.Counts.Completed != 0 {
		t.Fatalf("expected readable partial speed result (0 completed), err=%v counts=%+v", apiErr, partial.Execution.Counts)
	}
	if readCalls != 1 {
		t.Fatalf("expected 1 speed-results call, got %d", readCalls)
	}

	// External CLI finishes all 5 trials without modifying batch.json size/mtime.
	mode = "five_complete"
	complete, apiErr := svc.readContextSpeed(context.Background(), "/bin/inferyard", task)
	if apiErr != nil || complete.Execution.Counts.Completed != 5 || complete.Summary == nil {
		t.Fatalf("partial speed result masked completed batch: err=%v counts=%+v summary=%+v", apiErr, complete.Execution.Counts, complete.Summary)
	}
	if readCalls != 2 {
		t.Fatalf("expected second read after partial to invoke CLI (2 calls), got %d", readCalls)
	}

	// Subsequent reads of a finished/complete batch hit in-memory cache without spawning CLI.
	again, apiErr := svc.readContextSpeed(context.Background(), "/bin/inferyard", task)
	if apiErr != nil || again.Execution.Counts.Completed != 5 {
		t.Fatalf("cached complete speed read failed: err=%v counts=%+v", apiErr, again.Execution.Counts)
	}
	if readCalls != 2 {
		t.Fatalf("expected complete batch to hit cache without extra CLI call, got %d calls", readCalls)
	}
}
