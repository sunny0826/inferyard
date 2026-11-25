package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeStudioReportFixture(t *testing.T, reportsRoot, reportID string, modTime time.Time, runs []map[string]any) {
	t.Helper()
	dir := filepath.Join(reportsRoot, reportID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"schema_version": 3,
		"kind":           "studio-report.v1",
		"runs":           runs,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inferyard.ParseStudioReport(raw); err != nil {
		t.Fatalf("invalid fixture %s: %v", reportID, err)
	}
	p := filepath.Join(dir, "studio.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func makeReportRunFixture(runID, model, binaryPath, adapter string, tps *float64) map[string]any {
	var tpsVal any
	var tpsReason any = "missing_tokens"
	if tps != nil {
		tpsVal = *tps
		tpsReason = nil
	}
	return map[string]any{
		"source":     map[string]any{"run_id": runID, "path": "/tmp/run/" + runID},
		"model":      model,
		"threads":    8,
		"diagnostic": false,
		"facets":     map[string]any{"bundle_label": "zh-core"},
		"profile": map[string]any{
			"title": model,
			"engine": map[string]any{
				"binary_path": binaryPath,
				"adapter":     adapter,
				"backend":     "metal",
			},
			"machine": map[string]any{
				"cpu":                "Apple M4",
				"logical_cpus":       10,
				"memory_total_bytes": 34359738368,
			},
		},
		"summary": map[string]any{
			"completion_rate": map[string]any{"numerator": 1, "denominator": 1, "excluded": 0},
			"stop_reason":     "completed",
		},
		"dashboard": map[string]any{
			"quality": map[string]any{
				"numerator": 1, "denominator": 1, "excluded": 0, "complete": true, "reason": nil,
			},
			"categories": []any{
				map[string]any{
					"id": "qa", "label": "QA",
					"rate": map[string]any{"numerator": 1, "denominator": 1, "excluded": 0},
				},
			},
			"latency":                         map[string]any{"sample_count": 1, "excluded": 0, "min": 500.0, "p50": 500.0, "p95": nil, "max": 500.0},
			"sampled_peak_rss_bytes":          1932735283.2,
			"sampled_peak_rss_reason":         nil,
			"sampled_minimum_available_bytes": 10093173145.6,
			"minimum_available_reason":        nil,
			"throughput": map[string]any{
				"requests_per_second":            1.0,
				"requests_per_second_reason":     nil,
				"output_tokens_per_second":       tpsVal,
				"output_tokens_per_second_reason": tpsReason,
				"goodput_per_second":             0.0,
				"goodput_per_second_reason":      nil,
			},
			"output_block_intervals": map[string]any{
				"interval_count": 25, "request_count": 1,
				"min": 0.0, "p50": 100.0, "p95": 200.0, "p95_reason": nil, "max": 250.0, "reason": nil,
			},
		},
		"requests": []any{
			map[string]any{
				"case_id": "c1", "category_label": "QA", "state": "completed", "quality": "pass", "latency_ms": 500.0,
			},
		},
	}
}

func TestModelBenchmarkEvidenceAggregation(t *testing.T) {
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

	t1 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

	tpsOld := 40.0
	tpsLlama := 52.5
	tpsMLX := 41.2

	writeStudioReportFixture(t, reportsRoot, "report-old", t1, []map[string]any{
		makeReportRunFixture("run-old-llama", "Qwen3-30B-A3B-Q4_K_M.gguf", "/opt/llama-server", "prism_llama_server_v1", &tpsOld),
	})
	writeStudioReportFixture(t, reportsRoot, "report-new", t2, []map[string]any{
		makeReportRunFixture("run-new-llama", "Qwen3-30B-A3B-Q4_K_M.gguf", "/opt/llama-server", "prism_llama_server_v1", &tpsLlama),
		makeReportRunFixture("run-mlx", "Qwen3-30B-A3B-Q4_K_M", "/opt/venv/bin/mlx_lm.server", "local_openai_v1", &tpsMLX),
	})
	writeStudioReportFixture(t, reportsRoot, "report-missing", t3, []map[string]any{
		makeReportRunFixture("run-missing-tps", "NoSpeed-7B.gguf", "/opt/llama-server", "prism_llama_server_v1", nil),
	})

	svc.recordRunExclusivity("run-new-llama", ExclusivityExclusive)
	svc.recordRunExclusivity("run-mlx", ExclusivityExternal)

	ev := svc.ModelBenchmarkEvidence()
	if len(ev) != 1 {
		t.Fatalf("expected 1 aggregated model (skipping missing speed), got %d: %+v", len(ev), ev)
	}
	key := NormalizeModelEvidenceKey("Qwen3-30B-A3B-Q4_K_M.gguf")
	summary, ok := ev[key]
	if !ok {
		t.Fatalf("missing key %q in %+v", key, ev)
	}
	if summary.Best == nil || summary.Best.Engine != "llama.cpp" || summary.Best.TokensPerSecond != 52.5 {
		t.Fatalf("unexpected Best: %+v", summary.Best)
	}
	if summary.Best.TTFTMs != nil {
		t.Fatalf("expected TTFTMs to remain nil when absent, got %v", *summary.Best.TTFTMs)
	}
	if summary.Best.Exclusivity != ExclusivityExclusive {
		t.Fatalf("expected Best exclusivity=exclusive, got %q", summary.Best.Exclusivity)
	}
	if len(summary.ByEngine) != 2 {
		t.Fatalf("expected 2 engines in ByEngine, got %+v", summary.ByEngine)
	}
	if summary.ByEngine[0].Engine != "llama.cpp" || summary.ByEngine[1].Engine != "mlx-lm" {
		t.Fatalf("expected ByEngine sorted descending, got %+v", summary.ByEngine)
	}
	if summary.ByEngine[1].Exclusivity != ExclusivityExternal {
		t.Fatalf("expected mlx-lm exclusivity=external, got %q", summary.ByEngine[1].Exclusivity)
	}

	best, ok := svc.BestSpeedFor("Qwen3-30B-A3B-Q4_K_M.gguf", "")
	if !ok || best == nil || best.Engine != "llama.cpp" || best.TokensPerSecond != 52.5 {
		t.Fatalf("BestSpeedFor overall mismatch: ok=%v best=%+v", ok, best)
	}
	mlxRec, ok := svc.BestSpeedFor("qwen3-30b-a3b-q4_k_m", "mlx-lm")
	if !ok || mlxRec == nil || mlxRec.TokensPerSecond != 41.2 {
		t.Fatalf("BestSpeedFor mlx-lm mismatch: ok=%v rec=%+v", ok, mlxRec)
	}
	if _, ok := svc.BestSpeedFor("NoSpeed-7B.gguf", ""); ok {
		t.Fatalf("expected missing throughput model to return ok=false")
	}
}

func TestModelBenchmarkEvidencePicksMaxThroughputAndMultiKeyAliases(t *testing.T) {
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

	// Create a run directory with inputs.json linking GGUF general.name to filename.
	runDir := filepath.Join(ws, "results", "run-fast")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "inputs.json"), []byte(`{"model_path":"/models/qwen2.5-7b-instruct-q4_k_m.gguf"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tOlder := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	tNewer := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	tpsFast := 64.8
	tpsSlower := 48.2

	fastRun := makeReportRunFixture("run-fast", "Qwen2.5-7B-Instruct", "/opt/llama-server", "llama_cpp", &tpsFast)
	fastRun["source"] = map[string]any{"run_id": "run-fast", "path": runDir}
	slowRun := makeReportRunFixture("run-slow", "Qwen2.5-7B-Instruct", "/opt/llama-server", "llama.cpp", &tpsSlower)

	writeStudioReportFixture(t, reportsRoot, "rep-fast-old", tOlder, []map[string]any{fastRun})
	writeStudioReportFixture(t, reportsRoot, "rep-slow-new", tNewer, []map[string]any{slowRun})

	// Must pick highest throughput (64.8 from older report) rather than newer slower run (48.2).
	byGeneralName, ok := svc.BestSpeedFor("Qwen2.5-7B-Instruct", "llama.cpp")
	if !ok || byGeneralName == nil || byGeneralName.TokensPerSecond != 64.8 || byGeneralName.ReportRef != "rep-fast-old" {
		t.Fatalf("expected max throughput 64.8 from rep-fast-old, got ok=%v rec=%+v", ok, byGeneralName)
	}
	// Multi-key alias: querying by GGUF filename must also hit the report.
	byFilename, ok := svc.BestSpeedFor("qwen2.5-7b-instruct-q4_k_m.gguf", "llama-cpp")
	if !ok || byFilename == nil || byFilename.TokensPerSecond != 64.8 {
		t.Fatalf("expected filename lookup to hit multi-key evidence, got ok=%v rec=%+v", ok, byFilename)
	}
}
