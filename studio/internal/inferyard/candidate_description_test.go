package inferyard

import (
	"encoding/json"
	"strings"
	"testing"
)

func candidateFixture(family string) map[string]any {
	hash := strings.Repeat("a", 64)
	launch := map[string]any{"kind": "direct_process", "argv": []string{"/engine", "--port", "48857"}, "ownership": "operator", "working_directory": nil, "environment": nil}
	details := map[string]any{
		"schema_version": 3, "definition": "engine-description.v1", "adapter": "prism_llama_server_v1", "launch": launch,
		"assets": map[string]any{
			"engine":   map[string]any{"path": "/engine", "sha256": hash},
			"model":    map[string]any{"path": "/model", "kind": "gguf", "content_sha256": hash},
			"template": map[string]any{"path": "/template", "sha256": hash},
		},
		"generation":          map[string]any{"temperature": 0, "max_tokens": 512},
		"conditions":          map[string]any{"context_size": 8192},
		"endpoint":            map[string]any{"url": "http://127.0.0.1:48857", "server_pid": 1, "process_start_ticks": 1},
		"model_requests_sent": 0,
	}
	if family != "" {
		delete(details, "adapter")
		details["definition"], details["family"] = "local-engine-description.v2", family
		details["bound"] = false
		details["effective_parameters"], details["effective_template"] = nil, nil
		details["effective_unknown_reason"] = "requires_live_engine_observation"
		details["safety"] = map[string]any{"interval_seconds": 1, "max_temperature_celsius": 85, "require_temperature": true, "check_environment": true}
		launch["working_directory"], launch["environment"] = "/runtime", map[string]string{"HF_HUB_OFFLINE": "1"}
		assets := details["assets"].(map[string]any)
		delete(assets, "engine")
		assets["installation"] = map[string]any{"path": "/runtime.json", "sha256": hash, "declaration_sha256": strings.Repeat("b", 64)}
		model := assets["model"].(map[string]any)
		model["manifest_path"], model["manifest_sha256"] = "/model.json", hash
		if family != "llama-cpp" {
			model["kind"] = "directory"
		}
		if family == "lmstudio" {
			delete(launch, "argv")
			launch["kind"], launch["lms_executable"], launch["models_root"] = "lmstudio", "/lms", "/models"
			launch["background_argv"], launch["served_model"], launch["requires_loaded_instance"] = []string{"/daemon"}, "bench", true
		}
	}
	return details
}

func candidateWire(t *testing.T, details any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCandidateDescriptionFamilies(t *testing.T) {
	for _, family := range []string{"", "llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
		t.Run(family, func(t *testing.T) {
			value, err := ParseCandidateDescription(candidateWire(t, candidateFixture(family)), 0)
			if err != nil || value.Family != family || value.Generation.MaxTokens != 512 || value.Conditions.ContextSize != 8192 {
				t.Fatalf("%+v %v", value, err)
			}
			if family == "" {
				if value.Launch.WorkingDirectory != nil || value.Launch.Environment != nil || value.Assets.Engine.Path != "/engine" {
					t.Fatal(value)
				}
			} else if value.Assets.Installation.DeclarationSHA256 != strings.Repeat("b", 64) {
				t.Fatal(value)
			}
		})
	}
}

func TestCandidateDescriptionRejectsMalformedFields(t *testing.T) {
	cases := map[string]func(map[string]any){
		"old local query":     func(d map[string]any) { d["definition"] = "local-engine-description.v1" },
		"unknown version":     func(d map[string]any) { d["definition"] = "engine-description.v2" },
		"bool version":        func(d map[string]any) { d["schema_version"] = true },
		"null count":          func(d map[string]any) { d["model_requests_sent"] = nil },
		"bool count":          func(d map[string]any) { d["model_requests_sent"] = false },
		"model requests":      func(d map[string]any) { d["model_requests_sent"] = 1 },
		"unknown family":      func(d map[string]any) { d["family"] = "other" },
		"null bound":          func(d map[string]any) { d["bound"] = nil },
		"invented bound":      func(d map[string]any) { d["bound"] = true },
		"assets array":        func(d map[string]any) { d["assets"] = []any{} },
		"launch array":        func(d map[string]any) { d["launch"] = []any{} },
		"argv empty":          func(d map[string]any) { d["launch"].(map[string]any)["argv"] = []string{} },
		"argv null":           func(d map[string]any) { d["launch"].(map[string]any)["argv"] = nil },
		"argv null element":   func(d map[string]any) { d["launch"].(map[string]any)["argv"] = []any{"/engine", nil} },
		"relative executable": func(d map[string]any) { d["launch"].(map[string]any)["argv"] = []string{"engine"} },
		"nul argument":        func(d map[string]any) { d["launch"].(map[string]any)["argv"] = []string{"/engine", "a\x00b"} },
		"wrong launch":        func(d map[string]any) { d["launch"].(map[string]any)["kind"] = "lmstudio" },
		"null cwd":            func(d map[string]any) { d["launch"].(map[string]any)["working_directory"] = nil },
		"null env":            func(d map[string]any) { d["launch"].(map[string]any)["environment"] = nil },
		"null env value":      func(d map[string]any) { d["launch"].(map[string]any)["environment"] = map[string]any{"X": nil} },
		"bad hash": func(d map[string]any) {
			d["assets"].(map[string]any)["installation"].(map[string]any)["declaration_sha256"] = "bad"
		},
		"public endpoint":         func(d map[string]any) { d["endpoint"].(map[string]any)["url"] = "http://example.com" },
		"endpoint credentials":    func(d map[string]any) { d["endpoint"].(map[string]any)["url"] = "http://u:p@127.0.0.1" },
		"endpoint zero port":      func(d map[string]any) { d["endpoint"].(map[string]any)["url"] = "http://127.0.0.1:0" },
		"endpoint oversized port": func(d map[string]any) { d["endpoint"].(map[string]any)["url"] = "http://127.0.0.1:65536" },
		"endpoint true pid":       func(d map[string]any) { d["endpoint"].(map[string]any)["server_pid"] = true },
		"endpoint null pid":       func(d map[string]any) { d["endpoint"].(map[string]any)["server_pid"] = nil },
		"binding incomplete":      func(d map[string]any) { d["endpoint"].(map[string]any)["binding_path"] = "/binding.json" },
		"conditions string":       func(d map[string]any) { d["conditions"] = "context" },
		"context bool":            func(d map[string]any) { d["conditions"].(map[string]any)["context_size"] = true },
		"missing temperature":     func(d map[string]any) { delete(d["generation"].(map[string]any), "temperature") },
		"too many tokens":         func(d map[string]any) { d["generation"].(map[string]any)["max_tokens"] = 8193 },
		"safety null":             func(d map[string]any) { d["safety"] = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := candidateFixture("mlx-lm")
			mutate(d)
			if _, err := ParseCandidateDescription(candidateWire(t, d), 0); err == nil {
				t.Fatal("accepted invalid description")
			}
		})
	}
	base := string(candidateWire(t, candidateFixture("lmstudio")))
	for _, raw := range []string{base + `{}`, strings.Replace(base, `"schema_version":3`, `"schema_version":3,"schema_version":3`, 1), strings.Replace(base, `"requires_loaded_instance":true`, `"requires_loaded_instance":null`, 1), strings.Replace(base, `"limitations":[]`, `"limitations":null`, 1), strings.Replace(base, `"status":"prepared"`, `"status":"checked"`, 1)} {
		if _, err := ParseCandidateDescription([]byte(raw), 0); err == nil {
			t.Fatal("accepted invalid wire")
		}
	}
}

func TestCandidateDescriptionLegacyRejectsLocalFields(t *testing.T) {
	for _, mutation := range []func(map[string]any){
		func(d map[string]any) { d["family"] = "llama-cpp" },
		func(d map[string]any) { d["adapter"] = "local_openai_v1" },
		func(d map[string]any) { d["launch"].(map[string]any)["environment"] = map[string]string{} },
		func(d map[string]any) { d["assets"].(map[string]any)["engine"].(map[string]any)["path"] = "/different" },
	} {
		d := candidateFixture("")
		mutation(d)
		if _, err := ParseCandidateDescription(candidateWire(t, d), 0); err == nil {
			t.Fatal("accepted invalid legacy")
		}
	}
}
