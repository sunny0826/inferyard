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

func TestDescribeCandidateCachesUntilFileOrAssetChanges(t *testing.T) {
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

	if _, apiErr := svc.DescribeCandidate(context.Background(), candFile); apiErr != nil {
		t.Fatalf("first DescribeCandidate failed: %+v", apiErr)
	}
	if _, apiErr := svc.DescribeCandidate(context.Background(), candFile); apiErr != nil {
		t.Fatalf("second DescribeCandidate failed: %+v", apiErr)
	}
	if describeCalls != 1 {
		t.Fatalf("expected cached DescribeCandidate to invoke CLI once, got %d", describeCalls)
	}

	// Modifying referenced model file invalidates candidate description cache.
	if err := os.WriteFile(modelFile, []byte("gguf-v2-larger"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := svc.DescribeCandidate(context.Background(), candFile); apiErr != nil {
		t.Fatalf("third DescribeCandidate failed: %+v", apiErr)
	}
	if describeCalls != 2 {
		t.Fatalf("expected model asset change to invalidate DescribeCandidate cache, got %d calls", describeCalls)
	}
}
