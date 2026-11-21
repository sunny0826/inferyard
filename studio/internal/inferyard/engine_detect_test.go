package inferyard

import "testing"

func TestParseEngineDetectValidDirectAndWrapped(t *testing.T) {
	direct := []byte(`{
		"definition": "local-engine-detect.v1",
		"candidates": [
			{
				"family": "llama-cpp",
				"executable": "/opt/homebrew/bin/llama-server",
				"working_directory": "/opt/homebrew/bin",
				"runtime_roots": ["/opt/homebrew/bin"],
				"models_root": null,
				"label": "llama-cpp · /opt/homebrew/bin/llama-server"
			},
			{
				"family": "lmstudio",
				"executable": "/Users/demo/.lmstudio/bin/lms",
				"working_directory": "/Users/demo/.lmstudio/bin",
				"runtime_roots": ["/Users/demo/.lmstudio/bin"],
				"models_root": "/Users/demo/.lmstudio/models",
				"label": "lmstudio · /Users/demo/.lmstudio/bin/lms"
			}
		],
		"model_requests_sent": 0
	}`)
	parsed, err := ParseEngineDetect(direct)
	if err != nil {
		t.Fatalf("ParseEngineDetect(direct) unexpected error: %v", err)
	}
	if parsed.Definition != LocalEngineDetectDefinition || len(parsed.Candidates) != 2 || parsed.ModelRequestsSent != 0 {
		t.Fatalf("unexpected detect result: %+v", parsed)
	}
	if parsed.Candidates[1].ModelsRoot == nil || *parsed.Candidates[1].ModelsRoot != "/Users/demo/.lmstudio/models" {
		t.Fatalf("unexpected lmstudio models_root: %+v", parsed.Candidates[1])
	}

	wrapped := []byte(`{
		"command": "engine detect",
		"status": "detected",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": ` + string(direct) + `
	}`)
	wrappedParsed, err := ParseEngineDetect(wrapped)
	if err != nil {
		t.Fatalf("ParseEngineDetect(wrapped) unexpected error: %v", err)
	}
	if len(wrappedParsed.Candidates) != 2 {
		t.Fatalf("unexpected wrapped candidates: %+v", wrappedParsed)
	}
}

func TestParseEngineDetectRejectsInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"duplicate_keys":     `{"definition":"local-engine-detect.v1","definition":"local-engine-detect.v1","candidates":[],"model_requests_sent":0}`,
		"trailing_token":     `{"definition":"local-engine-detect.v1","candidates":[],"model_requests_sent":0}{}`,
		"wrong_definition":   `{"definition":"local-engine-detect.v2","candidates":[],"model_requests_sent":0}`,
		"nonzero_requests":   `{"definition":"local-engine-detect.v1","candidates":[],"model_requests_sent":1}`,
		"extra_field":        `{"definition":"local-engine-detect.v1","candidates":[],"model_requests_sent":0,"extra":true}`,
		"bad_family":         `{"definition":"local-engine-detect.v1","candidates":[{"family":"ollama","executable":"/a","working_directory":"/","runtime_roots":["/"],"models_root":null,"label":"x"}],"model_requests_sent":0}`,
		"relative_exe":       `{"definition":"local-engine-detect.v1","candidates":[{"family":"llama-cpp","executable":"bin/llama","working_directory":"/","runtime_roots":["/"],"models_root":null,"label":"x"}],"model_requests_sent":0}`,
		"empty_roots":        `{"definition":"local-engine-detect.v1","candidates":[{"family":"llama-cpp","executable":"/a","working_directory":"/","runtime_roots":[],"models_root":null,"label":"x"}],"model_requests_sent":0}`,
		"non_lms_models_dir": `{"definition":"local-engine-detect.v1","candidates":[{"family":"llama-cpp","executable":"/a","working_directory":"/","runtime_roots":["/"],"models_root":"/m","label":"x"}],"model_requests_sent":0}`,
		"wrong_command":      `{"command":"assets scan","status":"detected","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"local-engine-detect.v1","candidates":[],"model_requests_sent":0}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEngineDetect([]byte(payload)); err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}

func TestParseInitInstallationValidAndInvalid(t *testing.T) {
	direct := []byte(`{
		"definition": "local-engine-init.v1",
		"out": "/tmp/ws/engine-installations/mlx-lm.json",
		"family": "mlx-lm",
		"installation": {
			"schema_version": 3,
			"definition": "local-engine-installation.v1",
			"family": "mlx-lm",
			"executable": "/tmp/venv/bin/python",
			"working_directory": "/tmp/venv",
			"runtime_roots": ["/tmp/venv"],
			"environment": {"PYTHONDONTWRITEBYTECODE":"1","HF_HUB_OFFLINE":"1","TRANSFORMERS_OFFLINE":"1"}
		},
		"verified": true,
		"model_requests_sent": 0
	}`)
	wrapped := []byte(`{
		"command": "engine init-installation",
		"status": "prepared",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": ` + string(direct) + `
	}`)
	for _, raw := range [][]byte{direct, wrapped} {
		parsed, err := ParseInitInstallation(raw)
		if err != nil {
			t.Fatalf("ParseInitInstallation unexpected error: %v", err)
		}
		if parsed.Family != "mlx-lm" || !parsed.Verified || parsed.Out != "/tmp/ws/engine-installations/mlx-lm.json" {
			t.Fatalf("unexpected parsed init: %+v", parsed)
		}
	}

	badCases := map[string]string{
		"null_verified":   `{"definition":"local-engine-init.v1","out":"/tmp/a.json","family":"llama-cpp","installation":{"definition":"local-engine-installation.v1","family":"llama-cpp"},"verified":null,"model_requests_sent":0}`,
		"family_mismatch": `{"definition":"local-engine-init.v1","out":"/tmp/a.json","family":"llama-cpp","installation":{"definition":"local-engine-installation.v1","family":"vllm"},"verified":false,"model_requests_sent":0}`,
		"relative_out":    `{"definition":"local-engine-init.v1","out":"a.json","family":"llama-cpp","installation":{"definition":"local-engine-installation.v1","family":"llama-cpp"},"verified":false,"model_requests_sent":0}`,
	}
	for name, payload := range badCases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseInitInstallation([]byte(payload)); err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}
