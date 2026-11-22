package inferyard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDeviceCheckStarterRecommendationBackwardCompat(t *testing.T) {
	// Legacy response without starter_recommendation must still parse cleanly
	legacy := CommandResult{
		Command:      "device-check",
		Status:       "checked",
		Completeness: "complete",
		Details: json.RawMessage(`{
			"kind": "device_preflight.v1",
			"hardware": {"platform": "Darwin"},
			"models": [],
			"recommendation": {"mode": "metal"},
			"platform_capabilities": {"live_supported": true}
		}`),
	}
	parsedLegacy, err := ParseDeviceCheck(legacy)
	if err != nil {
		t.Fatalf("legacy device-check failed: %v", err)
	}
	if len(parsedLegacy.StarterRecommendation) != 0 {
		t.Fatalf("expected empty starter_recommendation on legacy payload, got %s", parsedLegacy.StarterRecommendation)
	}

	// Modern response with starter_recommendation parses cleanly
	modern := CommandResult{
		Command:      "device-check",
		Status:       "checked",
		Completeness: "complete",
		Details: json.RawMessage(`{
			"kind": "device_preflight.v1",
			"hardware": {"platform": "Darwin"},
			"models": [],
			"recommendation": {"mode": "metal"},
			"starter_recommendation": {"definition": "starter-recommendation.v1", "status": "recommended", "engine": "polystrata"},
			"platform_capabilities": {"live_supported": true}
		}`),
	}
	parsedModern, err := ParseDeviceCheck(modern)
	if err != nil {
		t.Fatalf("modern device-check failed: %v", err)
	}
	if !strings.Contains(string(parsedModern.StarterRecommendation), `"polystrata"`) {
		t.Fatalf("unexpected starter_recommendation: %s", parsedModern.StarterRecommendation)
	}

	// Non-object starter_recommendation must fail
	bad := CommandResult{
		Command:      "device-check",
		Status:       "checked",
		Completeness: "complete",
		Details: json.RawMessage(`{
			"kind": "device_preflight.v1",
			"hardware": {},
			"models": [],
			"recommendation": {},
			"starter_recommendation": "invalid",
			"platform_capabilities": {}
		}`),
	}
	if _, err := ParseDeviceCheck(bad); err == nil {
		t.Fatal("expected error for non-object starter_recommendation")
	}
}

func TestParseCandidateDescriptionPolystrataAndLegacyIsolation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	buildDesc := func(family string, ctxSize int, requireTemp bool, reasoningEffort string) []byte {
		gen := map[string]any{"temperature": 0, "max_tokens": 512}
		if reasoningEffort != "" {
			gen["reasoning_effort"] = reasoningEffort
		}
		payload := map[string]any{
			"command": "config describe", "status": "prepared", "completeness": "complete",
			"run_id": nil, "evidence_dir": nil, "limitations": []string{},
			"details": map[string]any{
				"schema_version": 3, "definition": "local-engine-description.v2",
				"family": family, "bound": false, "model_requests_sent": 0,
				"effective_parameters": nil, "effective_template": nil,
				"effective_unknown_reason": "requires_live_engine_observation",
				"launch": map[string]any{
					"kind": "direct_process", "argv": []string{"/opt/ps/.venv/bin/python", "serve/server.py"},
					"working_directory": "/opt/ps", "environment": map[string]string{"PYTHONDONTWRITEBYTECODE": "1"},
					"ownership": "operator",
				},
				"assets": map[string]any{
					"installation": map[string]any{"path": "/opt/ps/installation.json", "sha256": hash, "declaration_sha256": hash},
					"model":        map[string]any{"path": "/models/qwen36.gguf", "kind": "gguf", "content_sha256": hash, "manifest_path": "/out/model-assets.json", "manifest_sha256": hash},
					"template":     map[string]any{"path": "/out/chat-template.jinja", "sha256": hash},
				},
				"endpoint":   map[string]any{"url": "http://127.0.0.1:8080", "server_pid": 1, "process_start_ticks": 1},
				"conditions": map[string]any{"context_size": ctxSize},
				"generation": gen,
				"safety": map[string]any{
					"interval_seconds": 1, "max_temperature_celsius": 85,
					"require_temperature": requireTemp, "check_environment": true,
				},
			},
		}
		raw, _ := json.Marshal(payload)
		return raw
	}

	// PolyStrata accepts reasoning_effort="none", 131072 context, require_temperature=false (Linux/Windows)
	desc, err := ParseCandidateDescription(buildDesc("polystrata", 131072, false, "none"), 0)
	if err != nil {
		t.Fatalf("expected polystrata candidate description to parse: %v", err)
	}
	if desc.Family != "polystrata" || desc.Generation.ReasoningEffort != "none" || desc.Conditions.ContextSize != 131072 {
		t.Fatalf("unexpected parsed polystrata description: %+v", desc)
	}

	// Legacy llama-cpp still rejects require_temperature=false, context > 32768, and reasoning_effort
	if _, err := ParseCandidateDescription(buildDesc("llama-cpp", 8192, false, ""), 0); err == nil {
		t.Fatal("llama-cpp must still require require_temperature=true")
	}
	if _, err := ParseCandidateDescription(buildDesc("llama-cpp", 131072, true, ""), 0); err == nil {
		t.Fatal("llama-cpp must still enforce context_size <= 32768")
	}
	if _, err := ParseCandidateDescription(buildDesc("llama-cpp", 8192, true, "none"), 0); err == nil {
		t.Fatal("llama-cpp must still reject reasoning_effort in generation")
	}
}

func TestParseEngineDetectAndCheckPolystrata(t *testing.T) {
	detectRaw := []byte(`{
		"definition": "local-engine-detect.v1",
		"model_requests_sent": 0,
		"candidates": [{
			"family": "polystrata",
			"executable": "/opt/polystrata/.venv/bin/python",
			"working_directory": "/opt/polystrata",
			"runtime_roots": ["/opt/polystrata/.venv", "/opt/polystrata/serve"],
			"models_root": null,
			"data_dir": "/opt/PolyStrata-data",
			"version": "v0.1.45-poly.10",
			"model_configs": ["/opt/polystrata/strata-qwen36.json"],
			"label": "polystrata · v0.1.45-poly.10 (/opt/polystrata)"
		}]
	}`)
	detected, err := ParseEngineDetect(detectRaw)
	if err != nil {
		t.Fatalf("ParseEngineDetect failed for polystrata: %v", err)
	}
	if len(detected.Candidates) != 1 || detected.Candidates[0].Family != "polystrata" ||
		detected.Candidates[0].DataDir == nil || *detected.Candidates[0].DataDir != "/opt/PolyStrata-data" ||
		detected.Candidates[0].Version == nil || *detected.Candidates[0].Version != "v0.1.45-poly.10" ||
		len(detected.Candidates[0].ModelConfigs) != 1 {
		t.Fatalf("unexpected detected polystrata candidate: %+v", detected.Candidates)
	}

	// Non-polystrata must reject data_dir
	badDetect := []byte(`{
		"definition": "local-engine-detect.v1",
		"model_requests_sent": 0,
		"candidates": [{
			"family": "llama-cpp",
			"executable": "/opt/bin/llama-server",
			"working_directory": "/opt/bin",
			"runtime_roots": ["/opt/bin"],
			"models_root": null,
			"data_dir": "/opt/data",
			"label": "llama-cpp"
		}]
	}`)
	if _, err := ParseEngineDetect(badDetect); err == nil {
		t.Fatal("expected llama-cpp with data_dir to be rejected")
	}

	hash := strings.Repeat("b", 64)
	checkRaw, _ := json.Marshal(map[string]any{
		"command": "engine check", "status": "checked", "completeness": "complete",
		"run_id": nil, "evidence_dir": nil, "limitations": []string{},
		"details": map[string]any{
			"definition": "local-engine-check.v2", "eligible": true, "reason": nil,
			"family": "polystrata", "declaration_sha256": hash,
			"verification_scope": "installation_runtime_closure_only",
			"model_requests_sent": 0, "ready_to_run": false,
		},
	})
	checked, err := ParseEngineCheck(checkRaw, 0)
	if err != nil || !checked.Eligible || checked.Family == nil || *checked.Family != "polystrata" {
		t.Fatalf("ParseEngineCheck failed for polystrata: err=%v checked=%+v", err, checked)
	}
}

func TestStudioReportAcceptsHIPBackend(t *testing.T) {
	raw := []byte(`{
		"schema_version": 3,
		"kind": "studio-report.v1",
		"runs": [{
			"source": {"run_id": "run-hip-1", "path": "/tmp/run-hip-1"},
			"model": "qwen36-ud-q4-k-m",
			"threads": null,
			"diagnostic": false,
			"facets": {"bundle_label": "zh-core"},
			"profile": {
				"title": null,
				"engine": {"binary_path": "/opt/ps/.venv/bin/python", "adapter": "local_openai_v1", "backend": "hip"},
				"machine": {"cpu": "Ryzen 9", "logical_cpus": 16, "memory_total_bytes": 68719476736}
			},
			"summary": {"completion_rate": {"numerator": 1, "denominator": 1, "excluded": 0}, "stop_reason": "completed"},
			"dashboard": {
				"quality": {"numerator": 1, "denominator": 1, "excluded": 0, "complete": true, "reason": null},
				"categories": [{"id": "qa", "label": "问答", "rate": {"numerator": 1, "denominator": 1, "excluded": 0}}],
				"latency": {"sample_count": 1, "excluded": 0, "min": 100.0, "p50": 100.0, "p95": null, "max": 100.0},
				"sampled_peak_rss_bytes": 1024, "sampled_peak_rss_reason": null,
				"sampled_minimum_available_bytes": 2048, "minimum_available_reason": null
			},
			"requests": [{"case_id": "case-1", "category_label": "问答", "state": "completed", "quality": "pass", "latency_ms": 100.0}]
		}]
	}`)
	report, err := ParseStudioReport(raw)
	if err != nil {
		t.Fatalf("expected studio report with hip backend to parse: %v", err)
	}
	if report.Runs[0].Profile.Engine.Backend != "hip" {
		t.Fatalf("backend = %q, want hip", report.Runs[0].Profile.Engine.Backend)
	}
}
