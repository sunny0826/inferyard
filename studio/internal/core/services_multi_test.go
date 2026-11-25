package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func setupMultiServiceTestEnv(t *testing.T, modelSizes map[string]int64, hwSnapshot json.RawMessage) (*Service, string, string) {
	t.Helper()
	t.Setenv("ENGINE_HELPER", "1")
	workspace := t.TempDir()
	modelsDir := filepath.Join(workspace, "models")
	enginesDir := filepath.Join(workspace, "engines")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enginesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	helperBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engineDir := filepath.Join(enginesDir, "llama-multi")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	engineBin := filepath.Join(engineDir, "llama-server")
	script := "#!/bin/sh\nexec " + strconv.Quote(helperBin) + " \"$@\"\n"
	if err := os.WriteFile(engineBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCLI := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rawSettings, _ := json.Marshal(UserSettings{
		WorkspaceRoot: &workspace,
		InferyardPath: &fakeCLI,
	})
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	schemaPath := filepath.Join("..", "..", "..", "cli", "schemas", "serve_plan.schema.json")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}

	ctxLen := int64(32768)
	arch := "llama"
	scannedModels := make([]inferyard.ScannedModel, 0, len(modelSizes))
	for name, sz := range modelSizes {
		modelFile := filepath.Join(modelsDir, name)
		if err := os.WriteFile(modelFile, []byte("GGUF"), 0o600); err != nil {
			t.Fatal(err)
		}
		sizeCopy := sz
		scannedModels = append(scannedModels, inferyard.ScannedModel{
			Path:          modelFile,
			Name:          name,
			Format:        "gguf",
			SizeBytes:     &sizeCopy,
			Architecture:  &arch,
			ContextLength: &ctxLen,
			Available:     true,
		})
	}
	scanPayload := inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models:     scannedModels,
		Engines: []inferyard.ScannedEngine{
			{Path: engineBin, ModifiedNs: time.Now().UnixNano()},
		},
	}
	scanDetails, _ := json.Marshal(scanPayload)
	scanResult, _ := json.Marshal(map[string]any{
		"command":      "assets scan",
		"status":       "scanned",
		"completeness": "complete",
		"limitations":  []string{},
		"details":      json.RawMessage(scanDetails),
	})

	s, err := New(context.Background(), Options{
		SettingsPath:       settingsPath,
		EngineStartTimeout: 5 * time.Second,
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
				var modelArg, portArg, outArg string
				ctxSize := int64(8192)
				for i := 2; i+1 < len(args); i++ {
					switch args[i] {
					case "--model":
						modelArg = args[i+1]
					case "--port":
						portArg = args[i+1]
					case "--out":
						outArg = args[i+1]
					case "--context-size":
						if v, parseErr := strconv.ParseInt(args[i+1], 10, 64); parseErr == nil {
							ctxSize = v
						}
					}
				}
				if err := os.MkdirAll(outArg, 0o700); err != nil {
					t.Fatal(err)
				}
				plan := inferyard.ServePlan{
					SchemaVersion:    3,
					Argv:             []string{engineBin, "--host", "127.0.0.1", "--port", portArg},
					Environment:      map[string]string{},
					WorkingDirectory: filepath.Dir(engineBin),
					Endpoint:         "http://127.0.0.1:" + portArg,
					ModelPath:        modelArg,
					ModelKind:        "gguf",
					ContextSize:      ctxSize,
					Family:           "llama-cpp",
					Adapter:          "prism_llama_server_v1",
					Local:            false,
				}
				rawPlan, _ := json.Marshal(plan)
				if err := os.WriteFile(filepath.Join(outArg, "serve-plan.json"), rawPlan, 0o600); err != nil {
					t.Fatal(err)
				}
				res, _ := json.Marshal(map[string]any{
					"command":      "serve prepare",
					"status":       "prepared",
					"completeness": "complete",
					"limitations":  []string{},
					"details":      json.RawMessage(rawPlan),
				})
				return inferyard.Execution{ExitCode: 0, Stdout: res}, nil
			}
			return inferyard.Execution{ExitCode: 1}, nil
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
	wait(t, func() bool {
		reg := s.Registry()
		return !reg.Refreshing && len(reg.Models) == len(modelSizes) && len(reg.Engines) == 1
	})
	if len(hwSnapshot) > 0 {
		s.mu.Lock()
		s.device.Snapshot = hwSnapshot
		s.mu.Unlock()
	}
	engineName := s.Registry().Engines[0].Name
	return s, workspace, engineName
}

func waitManagedServiceState(t *testing.T, s *Service, serviceID, want string) EngineState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, ok := s.ManagedService(serviceID)
		if ok && st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("service %s did not reach state %q; last=%+v ok=%v", serviceID, want, st, ok)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMultiServiceParallelStartIndependentOwnersAndStop(t *testing.T) {
	s, workspace, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
		"model-b.gguf": 4,
	}, nil)

	_, unsub, cursor := s.Bus.Subscribe()
	defer unsub()

	stA, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
	})
	if apiErr != nil {
		t.Fatalf("start model-a failed: %+v", apiErr)
	}
	stA = waitManagedServiceState(t, s, stA.ServiceID, "running")

	stB, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 4096,
	})
	if apiErr != nil {
		t.Fatalf("start model-b failed: %+v", apiErr)
	}
	stB = waitManagedServiceState(t, s, stB.ServiceID, "running")

	if stA.ServiceID == "" || stB.ServiceID == "" || stA.ServiceID == stB.ServiceID {
		t.Fatalf("expected distinct non-empty ServiceIDs, got %q and %q", stA.ServiceID, stB.ServiceID)
	}
	if stA.Port == stB.Port {
		t.Fatalf("expected distinct auto-allocated ports, both got %d", stA.Port)
	}
	if _, err := os.Stat(serviceOwnerPath(workspace, stA.ServiceID)); err != nil {
		t.Fatalf("expected owner file for A: %v", err)
	}
	if _, err := os.Stat(serviceOwnerPath(workspace, stB.ServiceID)); err != nil {
		t.Fatalf("expected owner file for B: %v", err)
	}
	if list := s.ManagedServices(); len(list) != 2 {
		t.Fatalf("expected 2 managed services, got %d: %+v", len(list), list)
	}

	// Verify services.state event was emitted.
	replayed, _, _ := s.Bus.Since(cursor)
	sawServicesEvent := false
	for _, ev := range replayed {
		if ev.Type == "services.state" {
			sawServicesEvent = true
			break
		}
	}
	if !sawServicesEvent {
		t.Fatalf("expected services.state event on EventBus, got %+v", replayed)
	}

	// ServiceLogTail works per service ID.
	if _, logErr := s.ServiceLogTail(stA.ServiceID, 10); logErr != nil {
		t.Fatalf("ServiceLogTail(A) failed: %+v", logErr)
	}

	// Stopping A must leave B running and clean only A's owner file.
	if _, stopErr := s.StopManagedService(stA.ServiceID); stopErr != nil {
		t.Fatalf("StopManagedService(A) failed: %+v", stopErr)
	}
	waitManagedServiceState(t, s, stA.ServiceID, "stopped")
	assertOwnerGone(t, serviceOwnerPath(workspace, stA.ServiceID))

	if curB, ok := s.ManagedService(stB.ServiceID); !ok || curB.State != "running" {
		t.Fatalf("expected service B still running, got %+v ok=%v", curB, ok)
	}
	if _, err := os.Stat(serviceOwnerPath(workspace, stB.ServiceID)); err != nil {
		t.Fatalf("expected owner file for B still present: %v", err)
	}

	// StopAllManagedServices stops B and cleans its owner file.
	s.StopAllManagedServices()
	if curB, ok := s.ManagedService(stB.ServiceID); !ok || curB.State != "stopped" {
		t.Fatalf("expected service B stopped after StopAllManagedServices, got %+v", curB)
	}
	assertOwnerGone(t, serviceOwnerPath(workspace, stB.ServiceID))
}

func TestMultiServicePortConflictNeverSilentlyReallocates(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
		"model-b.gguf": 4,
	}, nil)

	fixedPort := freePort(t)
	stA, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 4096, Port: fixedPort,
	})
	if apiErr != nil {
		t.Fatalf("start model-a on port %d failed: %+v", fixedPort, apiErr)
	}
	stA = waitManagedServiceState(t, s, stA.ServiceID, "running")

	// Model B requesting the same fixed port must fail with engine_port_in_use (never switch ports).
	if _, conflictErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 4096, Port: fixedPort,
	}); conflictErr == nil || conflictErr.Code != "engine_port_in_use" {
		t.Fatalf("expected engine_port_in_use for conflicting fixed port %d, got %+v", fixedPort, conflictErr)
	}

	// Model A must still be running unaffected.
	if curA, ok := s.ManagedService(stA.ServiceID); !ok || curA.State != "running" {
		t.Fatalf("expected model-a unaffected after port conflict, got %+v", curA)
	}

	// Model B with Port=0 automatically picks a different free port.
	stB, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 4096, Port: 0,
	})
	if apiErr != nil {
		t.Fatalf("start model-b with auto port failed: %+v", apiErr)
	}
	stB = waitManagedServiceState(t, s, stB.ServiceID, "running")
	if stB.Port == fixedPort {
		t.Fatalf("expected model-b auto port != %d", fixedPort)
	}
}

func TestMultiServiceShutdownKeepAndReopenMixedAdoptAndExitedWhileAway(t *testing.T) {
	s1, workspace, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-alive.gguf": 4,
		"model-away.gguf":  4,
	}, nil)

	stAlive, err := s1.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-alive.gguf", Engine: engine, ContextSize: 4096, LANAccess: true,
	})
	if err != nil {
		t.Fatalf("start model-alive failed: %+v", err)
	}
	stAlive = waitManagedServiceState(t, s1, stAlive.ServiceID, "running")

	stAway, err := s1.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-away.gguf", Engine: engine, ContextSize: 4096,
	})
	if err != nil {
		t.Fatalf("start model-away failed: %+v", err)
	}
	stAway = waitManagedServiceState(t, s1, stAway.ServiceID, "running")

	// Default Shutdown keeps both services running and preserves both owner files.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutErr := s1.Shutdown(ctx); shutErr != nil {
		t.Fatalf("s1.Shutdown failed: %v", shutErr)
	}
	if _, statErr := os.Stat(serviceOwnerPath(workspace, stAlive.ServiceID)); statErr != nil {
		t.Fatalf("expected alive owner preserved: %v", statErr)
	}
	if _, statErr := os.Stat(serviceOwnerPath(workspace, stAway.ServiceID)); statErr != nil {
		t.Fatalf("expected away owner preserved: %v", statErr)
	}

	// Simulate model-away crashing with OOM while Studio is closed.
	_ = os.WriteFile(stAway.LogPath, []byte("fatal: out of memory\n"), 0o600)
	if proc, findErr := os.FindProcess(int(stAway.Pid)); findErr == nil {
		_ = proc.Kill()
		wait(t, func() bool {
			_, tickErr := processStartTicks(stAway.Pid)
			return errors.Is(tickErr, os.ErrNotExist)
		})
	}

	// Reopen a new Service on the same workspace.
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if writeErr := os.WriteFile(settingsPath, rawSettings, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	s2, newErr := New(context.Background(), Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			return inferyard.Execution{}, errors.New("unexpected CLI call")
		},
	})
	if newErr != nil {
		t.Fatal(newErr)
	}
	t.Cleanup(func() {
		c, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		_ = s2.ShutdownWithOptions(c, true)
	})

	// Alive service is adopted with LANAccess preserved.
	adopted, ok := s2.ManagedService(stAlive.ServiceID)
	if !ok || !adopted.Adopted || adopted.State != "running" || !adopted.LANAccess || adopted.Pid != stAlive.Pid {
		t.Fatalf("expected adopted running LAN service, got %+v ok=%v", adopted, ok)
	}
	// Exited-while-away service produced a notice and cleared its owner file.
	assertOwnerGone(t, serviceOwnerPath(workspace, stAway.ServiceID))
	notices := s2.ExitedServiceNotices()
	if len(notices) != 1 || !notices[0].WhileAway || notices[0].DetectedAt == "" || notices[0].ExitedAt != "" || notices[0].Hint != ExitHintOOM {
		t.Fatalf("unexpected exit notices: %+v", notices)
	}
	if dismissErr := s2.DismissExitedServiceNoticeByID(notices[0].ID); dismissErr != nil {
		t.Fatalf("DismissExitedServiceNoticeByID failed: %v", dismissErr)
	}
	if len(s2.ExitedServiceNotices()) != 0 {
		t.Fatalf("expected 0 exit notices after dismiss, got %+v", s2.ExitedServiceNotices())
	}

	// ShutdownWithOptions(stopServices=true) terminates the adopted service and clears its owner file.
	c2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if shutErr := s2.ShutdownWithOptions(c2, true); shutErr != nil {
		t.Fatalf("s2.ShutdownWithOptions(stopServices=true) failed: %v", shutErr)
	}
	assertOwnerGone(t, serviceOwnerPath(workspace, stAlive.ServiceID))
}
