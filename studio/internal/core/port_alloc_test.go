package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestChooseFreeLoopbackPortUsesPreferredWhenAvailable(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	preferred := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	got, err := ChooseFreeLoopbackPort(preferred)
	if err != nil {
		t.Fatalf("ChooseFreeLoopbackPort(%d) error = %v", preferred, err)
	}
	if got != preferred {
		t.Fatalf("got %d, want preferred %d", got, preferred)
	}
}

func TestChooseFreeLoopbackPortFallsBackWhenPreferredOccupied(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	occupied := holder.Addr().(*net.TCPAddr).Port

	got, err := ChooseFreeLoopbackPort(occupied)
	if err != nil {
		t.Fatalf("ChooseFreeLoopbackPort(%d) error = %v", occupied, err)
	}
	if got <= 0 || got == occupied {
		t.Fatalf("got %d, want non-zero port different from occupied %d", got, occupied)
	}
	verify, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(got)))
	if err != nil {
		t.Fatalf("chosen port %d is not bindable: %v", got, err)
	}
	_ = verify.Close()
}

func TestChooseFreeLoopbackPortFallsBackWhenDefaultPortHeld(t *testing.T) {
	holder, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(DefaultBenchmarkPort)))
	if err == nil {
		defer holder.Close()
	}
	got, err := ChooseFreeLoopbackPort(DefaultBenchmarkPort)
	if err != nil {
		t.Fatalf("ChooseFreeLoopbackPort(DefaultBenchmarkPort) error = %v", err)
	}
	if got <= 0 || got == DefaultBenchmarkPort {
		t.Fatalf("got %d, want fallback port != %d", got, DefaultBenchmarkPort)
	}
}

func TestChooseFreeLoopbackPortHandlesInvalidPreferredAndNoFreePort(t *testing.T) {
	got, err := ChooseFreeLoopbackPort(0)
	if err != nil || got <= 0 {
		t.Fatalf("ChooseFreeLoopbackPort(0) = (%d, %v), want valid ephemeral port", got, err)
	}

	orig := reservePortFunc
	t.Cleanup(func() { reservePortFunc = orig })
	reservePortFunc = func(int) (int, error) {
		return 0, errors.New("ports exhausted")
	}
	if _, err := ChooseFreeLoopbackPort(DefaultBenchmarkPort); err == nil {
		t.Fatal("expected error when no loopback ports are available")
	}
}

func setupLocalCandidateFixture(t *testing.T, family string) (*Service, *string) {
	t.Helper()
	workspace, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(workspace, "results")
	modelsRoot := filepath.Join(workspace, "models")
	if err := os.MkdirAll(modelsRoot, 0700); err != nil {
		t.Fatal(err)
	}
	bundlesDir := filepath.Join(workspace, "bundles")
	if err := os.MkdirAll(bundlesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundlesDir, "zh-core.json"), []byte(`{"bundle_id":"zh-core"}`), 0600); err != nil {
		t.Fatal(err)
	}

	var modelPath, modelKind string
	instPayload := map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            family,
		"executable":        os.Args[0],
		"working_directory": workspace,
		"runtime_roots":     []string{workspace},
		"environment":       map[string]string{"PYTHONDONTWRITEBYTECODE": "1"},
	}
	if family == "lmstudio" {
		modelKind = "gguf"
		modelPath = filepath.Join(modelsRoot, "demo.gguf")
		if err := os.WriteFile(modelPath, []byte("GGUF"), 0600); err != nil {
			t.Fatal(err)
		}
		instPayload["models_root"] = modelsRoot
	} else {
		modelKind = "directory"
		modelPath = filepath.Join(modelsRoot, "demo-dir-model")
		if err := os.MkdirAll(modelPath, 0700); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{
			"config.json":           "{}",
			"tokenizer_config.json": `{"chat_template":"{{ messages }}"}`,
			"weights.safetensors":   "weights",
		} {
			if err := os.WriteFile(filepath.Join(modelPath, name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}

	instPath := filepath.Join(workspace, family+".json")
	instRaw, _ := json.Marshal(instPayload)
	if err := os.WriteFile(instPath, instRaw, 0600); err != nil {
		t.Fatal(err)
	}
	declHash := fmt.Sprintf("%x", sha256.Sum256(instRaw))
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))

	var recordedPort string
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if out, ok := version(args); ok {
			return out, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
			raw, _ := json.Marshal(map[string]any{
				"command": "engine check", "status": "checked", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{
					"definition": "local-engine-check.v2", "eligible": true, "reason": nil,
					"family": family, "declaration_sha256": declHash,
					"verification_scope": "installation_runtime_closure_only", "model_requests_sent": 0, "ready_to_run": false,
				},
			})
			return inferyard.Execution{Stdout: raw}, nil
		}
		if len(args) >= 2 && args[0] == "config" && args[1] == "create" {
			recordedPort = fixtureFlag(args, "--port")
			out := fixtureFlag(args, "--out")
			if err := os.MkdirAll(out, 0700); err != nil {
				return inferyard.Execution{}, err
			}
			if err := os.WriteFile(filepath.Join(out, "candidate.toml"), []byte("[output]\n"), 0600); err != nil {
				return inferyard.Execution{}, err
			}
			return inferyard.Execution{Stdout: []byte(`{"command":"config create","status":"prepared","completeness":"complete","limitations":[]}`)}, nil
		}
		if len(args) >= 2 && args[0] == "config" && args[1] == "describe" {
			launch := map[string]any{
				"kind": "direct_process", "argv": []string{os.Args[0], "--host", "127.0.0.1", "--port", recordedPort},
				"working_directory": workspace, "environment": map[string]string{}, "ownership": "operator",
			}
			if family == "lmstudio" {
				delete(launch, "argv")
				launch["kind"] = "lmstudio"
				launch["lms_executable"] = os.Args[0]
				launch["models_root"] = modelsRoot
				launch["background_argv"] = []string{os.Args[0]}
				launch["served_model"] = "demo"
				launch["requires_loaded_instance"] = true
			}
			details := map[string]any{
				"schema_version": 3, "definition": "local-engine-description.v2", "family": family, "model_requests_sent": 0, "bound": false,
				"launch": launch,
				"assets": map[string]any{
					"installation": map[string]any{"path": instPath, "sha256": hash, "declaration_sha256": declHash},
					"model":        map[string]any{"path": modelPath, "kind": modelKind, "content_sha256": hash, "manifest_path": filepath.Join(workspace, "m.json"), "manifest_sha256": hash},
					"template":     map[string]any{"path": filepath.Join(workspace, "t.jinja"), "sha256": hash},
				},
				"endpoint":                 map[string]any{"url": "http://127.0.0.1:" + recordedPort, "server_pid": 1, "process_start_ticks": 1},
				"conditions":               map[string]any{"context_size": 8192},
				"generation":               map[string]any{"max_tokens": 512, "temperature": 0},
				"safety":                   map[string]any{"interval_seconds": 1, "max_temperature_celsius": 85, "require_temperature": true, "check_environment": true},
				"effective_parameters":     nil,
				"effective_template":       nil,
				"effective_unknown_reason": "requires_live_engine_observation",
			}
			raw, _ := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
			return inferyard.Execution{Stdout: raw}, nil
		}
		return inferyard.Execution{}, fmt.Errorf("unexpected command: %v", args)
	})
	s.settings.WorkspaceRoot = &workspace
	s.settings.ResultsRoot = &results
	s.settings.EngineInstallations = []string{instPath}
	if family == "lmstudio" {
		s.device.Snapshot, _ = json.Marshal(map[string]any{
			"models": []map[string]any{{"path": modelPath, "status": "available"}},
		})
	} else {
		s.settings.ModelsRoot = &modelsRoot
	}
	return s, &recordedPort
}

func TestPrepareLocalCandidatesUsesAllocatedFreePort(t *testing.T) {
	s, recordedPort := setupLocalCandidateFixture(t, "mlx-lm")
	orig := reservePortFunc
	t.Cleanup(func() { reservePortFunc = orig })
	reservePortFunc = func(port int) (int, error) {
		if port == DefaultBenchmarkPort {
			return 0, errors.New("address already in use")
		}
		return 49555, nil
	}

	runs, apiErr := s.PrepareLocalCandidates(context.Background(), LocalCandidateRequest{
		ModelName:  "demo-dir-model",
		EngineName: "mlx-lm · mlx-lm.json",
		Bundles:    []string{"zh-core"},
	})
	if apiErr != nil {
		t.Fatalf("PrepareLocalCandidates failed: %#v", apiErr)
	}
	if len(runs) != 1 || *recordedPort != "49555" {
		t.Fatalf("runs=%+v recordedPort=%q, want port 49555", runs, *recordedPort)
	}

	reservePortFunc = func(int) (int, error) {
		return 0, errors.New("ports exhausted")
	}
	if _, apiErr := s.PrepareLocalCandidates(context.Background(), LocalCandidateRequest{
		ModelName:  "demo-dir-model",
		EngineName: "mlx-lm · mlx-lm.json",
		Bundles:    []string{"zh-core"},
	}); apiErr == nil {
		t.Fatal("expected error when port allocation fails")
	}
}

func TestPrepareLocalCandidatesLMStudioUsesExplicitEndpointPort(t *testing.T) {
	s, recordedPort := setupLocalCandidateFixture(t, "lmstudio")
	orig := reservePortFunc
	t.Cleanup(func() { reservePortFunc = orig })
	reservePortFunc = func(int) (int, error) {
		return 0, errors.New("lmstudio should not probe free ports")
	}

	runs, apiErr := s.PrepareLocalCandidates(context.Background(), LocalCandidateRequest{
		ModelName:  "demo.gguf",
		EngineName: "lmstudio · lmstudio.json",
		Bundles:    []string{"zh-core"},
		ServicePID: 4001,
		Endpoint:   "http://127.0.0.1:1234",
	})
	if apiErr != nil {
		t.Fatalf("PrepareLocalCandidates failed: %#v", apiErr)
	}
	if len(runs) != 1 || *recordedPort != "1234" {
		t.Fatalf("runs=%+v recordedPort=%q, want port 1234", runs, *recordedPort)
	}
}
