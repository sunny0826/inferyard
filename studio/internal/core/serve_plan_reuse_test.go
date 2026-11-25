package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartStandaloneCacheHitSkipsCLIProbeAndRewritesExplicitPort(t *testing.T) {
	var prepareCalls, totalCalls atomic.Int32
	s, _, model, engine := setupStandaloneTestService(t, true, &prepareCalls, &totalCalls)

	st1, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("1st StartStandalone failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	// Clear serve-support probe cache completely: cached plan hit must not call resolveServeCLI.
	s.clearServeSupport()
	beforeCalls := totalCalls.Load()
	st2, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("2nd StartStandalone failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if totalCalls.Load() != beforeCalls || prepareCalls.Load() != 1 {
		t.Fatalf("expected 0 CLI calls on cache hit even when probe cache cleared, totalDelta=%d prepare=%d", totalCalls.Load()-beforeCalls, prepareCalls.Load())
	}
	if st2.Endpoint != st1.Endpoint {
		t.Fatalf("endpoint=%q want %q", st2.Endpoint, st1.Endpoint)
	}

	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	// Explicit new port for non-polystrata engine rewrites --port & Endpoint without calling CLI.
	newPort := freePort(t)
	if newPort == st1.Port {
		newPort = freePort(t)
	}
	s.clearServeSupport()
	beforePortSwitch := totalCalls.Load()
	st3, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192, Port: newPort,
	})
	if apiErr != nil {
		t.Fatalf("StartStandalone with new explicit port failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if totalCalls.Load() != beforePortSwitch || prepareCalls.Load() != 1 {
		t.Fatalf("expected 0 CLI calls on explicit port hot rewrite, totalDelta=%d prepare=%d", totalCalls.Load()-beforePortSwitch, prepareCalls.Load())
	}
	if st3.Port != newPort || st3.Endpoint != "http://127.0.0.1:"+strconv.Itoa(newPort) {
		t.Fatalf("expected port=%d endpoint=http://127.0.0.1:%d, got port=%d endpoint=%q", newPort, newPort, st3.Port, st3.Endpoint)
	}
}

func TestStartStandalonePolystrataPortChangeFallsBackToCLI(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	workspace := t.TempDir()
	modelsDir := filepath.Join(workspace, "models")
	instDir := filepath.Join(workspace, "local-engines", "polystrata-1")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(modelsDir, "tiny.gguf")
	if err := os.WriteFile(modelFile, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	helperBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engineBin := filepath.Join(instDir, "polystrata-server")
	if err := os.WriteFile(engineBin, []byte("#!/bin/sh\nexec "+strconv.Quote(helperBin)+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(instDir, "installation.json")
	instRaw, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "polystrata",
		"working_directory": instDir,
		"executable":        engineBin,
	})
	if err := os.WriteFile(instPath, instRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeCLI := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rawSettings, _ := json.Marshal(UserSettings{
		WorkspaceRoot:       &workspace,
		InferyardPath:       &fakeCLI,
		EngineInstallations: []string{instPath},
	})
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "cli", "schemas", "serve_plan.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	sizeBytes := int64(4)
	ctxLen := int64(32768)
	arch := "llama"
	scanPayload := inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{{
			Path: modelFile, Name: "tiny.gguf", Format: "gguf",
			SizeBytes: &sizeBytes, Architecture: &arch, ContextLength: &ctxLen, Available: true,
		}},
	}
	scanDetails, _ := json.Marshal(scanPayload)
	scanResult, _ := json.Marshal(map[string]any{
		"command":      "assets scan",
		"status":       "scanned",
		"completeness": "complete",
		"limitations":  []string{},
		"details":      json.RawMessage(scanDetails),
	})
	var prepareCalls atomic.Int32
	s, err := New(context.Background(), Options{
		SettingsPath:       settingsPath,
		EngineStartTimeout: 3 * time.Second,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{ExitCode: 0, Stdout: scanResult}, nil
			}
			if len(args) == 2 && args[0] == "--schema" && args[1] == "serve_plan" {
				return inferyard.Execution{ExitCode: 0, Stdout: schemaBytes}, nil
			}
			if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
				prepareCalls.Add(1)
				var modelArg, portArg, outArg string
				for i := 2; i+1 < len(args); i++ {
					switch args[i] {
					case "--model":
						modelArg = args[i+1]
					case "--port":
						portArg = args[i+1]
					case "--out":
						outArg = args[i+1]
					}
				}
				if err := os.MkdirAll(outArg, 0o700); err != nil {
					t.Fatal(err)
				}
				cfgPath := filepath.Join(outArg, "polystrata.json")
				if err := os.WriteFile(cfgPath, []byte(`{"host":"127.0.0.1","port":`+portArg+`}`), 0o600); err != nil {
					t.Fatal(err)
				}
				plan := inferyard.ServePlan{
					SchemaVersion:    3,
					Argv:             []string{engineBin, "--config", cfgPath, "--host", "127.0.0.1", "--port", portArg},
					Environment: map[string]string{
						"ENGINE_HELPER":             "1",
						"ENGINE_HELPER_HEALTH_BODY": `{"service":"strata","loaded":true}`,
					},
					WorkingDirectory: instDir,
					Endpoint:         "http://127.0.0.1:" + portArg,
					ModelPath:        modelArg,
					ModelKind:        "gguf",
					ContextSize:      8192,
					Family:           "polystrata",
					Adapter:          "local_openai_v1",
					Local:            true,
				}
				rawPlan, _ := json.Marshal(plan)
				if err := os.WriteFile(filepath.Join(outArg, "serve-plan.json"), rawPlan, 0o600); err != nil {
					t.Fatal(err)
				}
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"serve prepare","status":"prepared","completeness":"complete","limitations":[]}`)}, nil
			}
			return inferyard.Execution{ExitCode: 2, Stderr: []byte("unexpected: " + strings.Join(args, " "))}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.ShutdownWithOptions(ctx, true)
	})
	s.registryRoots = func(string) []string { return nil }
	s.mu.Lock()
	s.assets.Snapshot = scanDetails
	s.assets.Parsed = &scanPayload
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.Models) == 0 || len(reg.Engines) == 0 {
		t.Fatalf("expected seeded model and local polystrata engine, got %+v", reg)
	}
	modelName, engName := reg.Models[0].Name, reg.Engines[0].Name

	port1 := freePort(t)
	st1, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: modelName, Engine: engName, ContextSize: 8192, Port: port1,
	})
	if apiErr != nil {
		t.Fatalf("1st polystrata start failed (model=%q eng=%q reg=%+v): %+v", modelName, engName, reg, apiErr)
	}
	waitEngineState(t, s, "running")
	if prepareCalls.Load() != 1 || st1.Port != port1 {
		t.Fatalf("expected 1st start prepare=1 port=%d, got prepare=%d port=%d", port1, prepareCalls.Load(), st1.Port)
	}
	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	// Same port reuses cache without CLI.
	s.clearServeSupport()
	if _, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: modelName, Engine: engName, ContextSize: 8192, Port: port1,
	}); apiErr != nil {
		t.Fatalf("2nd polystrata same-port start failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if prepareCalls.Load() != 1 {
		t.Fatalf("expected polystrata same-port cache hit without CLI, prepare=%d", prepareCalls.Load())
	}
	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	// Different port must fall back to CLI serve prepare.
	port2 := freePort(t)
	if port2 == port1 {
		port2 = freePort(t)
	}
	st3, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: modelName, Engine: engName, ContextSize: 8192, Port: port2,
	})
	if apiErr != nil {
		t.Fatalf("3rd polystrata new-port start failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if prepareCalls.Load() != 2 || st3.Port != port2 {
		t.Fatalf("expected polystrata port change to invoke CLI (prepare=2, port=%d), got prepare=%d port=%d", port2, prepareCalls.Load(), st3.Port)
	}
}
