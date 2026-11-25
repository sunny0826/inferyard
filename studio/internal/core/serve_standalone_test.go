package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setupStandaloneTestService(
	t *testing.T,
	supportsServe bool,
	prepareCalls *atomic.Int32,
	totalCLICalls *atomic.Int32,
) (*Service, string, string, string) {
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
	modelFile := filepath.Join(modelsDir, "tiny.gguf")
	if err := os.WriteFile(modelFile, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	helperBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engineDir := filepath.Join(enginesDir, "llama-test")
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

	sizeBytes := int64(4)
	ctxLen := int64(32768)
	arch := "llama"
	scanPayload := inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{
				Path:          modelFile,
				Name:          "tiny.gguf",
				Format:        "gguf",
				SizeBytes:     &sizeBytes,
				Architecture:  &arch,
				ContextLength: &ctxLen,
				Available:     true,
			},
		},
		Engines: []inferyard.ScannedEngine{
			{
				Path:       engineBin,
				ModifiedNs: time.Now().UnixNano(),
			},
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
		EngineStartTimeout: 3 * time.Second,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if totalCLICalls != nil {
				totalCLICalls.Add(1)
			}
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{ExitCode: 0, Stdout: scanResult}, nil
			}
			if len(args) == 2 && args[0] == "--schema" && args[1] == "serve_plan" {
				if !supportsServe {
					return inferyard.Execution{ExitCode: 2, Stderr: []byte("invalid choice")}, nil
				}
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
				plan := inferyard.ServePlan{
					SchemaVersion:    3,
					Argv:             []string{engineBin, "--host", "127.0.0.1", "--port", portArg},
					Environment:      map[string]string{},
					WorkingDirectory: filepath.Dir(engineBin),
					Endpoint:         "http://127.0.0.1:" + portArg,
					ModelPath:        modelArg,
					ModelKind:        "gguf",
					ContextSize:      8192,
					Family:           "llama-cpp",
					Adapter:          "prism_llama_server_v1",
					Local:            false,
				}
				if idx := indexOfArg(args, "--context-size"); idx >= 0 && idx+1 < len(args) {
					if ctxVal, err := strconv.ParseInt(args[idx+1], 10, 64); err == nil {
						plan.ContextSize = ctxVal
					}
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

	s.registryRoots = func(w string) []string { return []string{filepath.Join(w, "engines")} }
	s.mu.Lock()
	s.assets.Snapshot = scanDetails
	s.assets.Parsed = &scanPayload
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.Models) == 0 || len(reg.Engines) == 0 {
		t.Fatalf("expected seeded model and engine in registry, got %+v", reg)
	}
	return s, workspace, reg.Models[0].Name, reg.Engines[0].Name
}

func indexOfArg(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}

func waitEngineState(t *testing.T, s *Service, want string) EngineState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := s.Engine()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("engine state=%q want %q (full=%+v)", st.State, want, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartStandaloneReusesDirAndReallocatesOnPortConflict(t *testing.T) {
	var calls, totalCalls atomic.Int32
	s, _, model, engine := setupStandaloneTestService(t, true, &calls, &totalCalls)

	if !s.ServeSupported(context.Background()) {
		t.Fatalf("expected ServeSupported=true")
	}

	// 1st start: invokes serve prepare once.
	st1, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 12000,
	})
	if apiErr != nil {
		t.Fatalf("1st StartStandalone failed: %+v", apiErr)
	}
	if st1.Origin != EngineOriginStandalone {
		t.Fatalf("origin=%q want standalone", st1.Origin)
	}
	st1 = waitEngineState(t, s, "running")
	if calls.Load() != 1 {
		t.Fatalf("prepare calls=%d want 1", calls.Load())
	}

	// Same-combination request while running is idempotent even if serve support cache is cleared.
	s.clearServeSupport()
	beforeIdem := totalCalls.Load()
	stIdem, idemErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 12000,
	})
	if idemErr != nil || stIdem.Pid != st1.Pid {
		t.Fatalf("expected idempotent running state, got %+v err=%+v", stIdem, idemErr)
	}
	if totalCalls.Load() != beforeIdem {
		t.Fatalf("expected 0 CLI calls on running idempotency, delta=%d", totalCalls.Load()-beforeIdem)
	}
	// Restore positive support probe before testing TTL-expired reuse.
	if !s.ServeSupported(context.Background()) {
		t.Fatalf("expected ServeSupported=true")
	}

	// Single-slot conflict for a different context size while running.
	if _, mismatch := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 16384,
	}); mismatch == nil || mismatch.Code != "engine_candidate_mismatch" {
		t.Fatalf("expected engine_candidate_mismatch, got %+v", mismatch)
	}

	// Stop, expire probeTTL, and start the same combination again: 0 CLI calls total.
	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	s.probeMu.Lock()
	s.probeCheckedAt = time.Now().Add(-time.Hour)
	s.serveProbeChecked = time.Now().Add(-time.Hour)
	s.probeMu.Unlock()

	beforeReuse := totalCalls.Load()
	st2, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 12000,
	})
	if apiErr != nil {
		t.Fatalf("2nd StartStandalone failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if calls.Load() != 1 {
		t.Fatalf("expected reuse without re-running serve prepare, calls=%d", calls.Load())
	}
	if totalCalls.Load() != beforeReuse {
		t.Fatalf("expected 0 total CLI calls on cached reuse after TTL expiry, delta=%d", totalCalls.Load()-beforeReuse)
	}
	if st2.Endpoint != st1.Endpoint {
		t.Fatalf("expected reused endpoint %q, got %q", st1.Endpoint, st2.Endpoint)
	}

	// Stop, occupy the cached port externally, and start again: rewrites port in Go without re-running CLI.
	if _, err := s.StopEngine(); err != nil {
		t.Fatalf("StopEngine failed: %+v", err)
	}
	waitEngineState(t, s, "stopped")

	addr := strings.TrimPrefix(st1.Endpoint, "http://")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("occupy port %s: %v", addr, err)
	}
	defer ln.Close()

	beforeHotRewrite := totalCalls.Load()
	st3, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 12000,
	})
	if apiErr != nil {
		t.Fatalf("3rd StartStandalone after port occupied failed: %+v", apiErr)
	}
	waitEngineState(t, s, "running")
	if calls.Load() != 1 {
		t.Fatalf("expected hot port rewrite without re-running serve prepare, calls=%d", calls.Load())
	}
	if totalCalls.Load() != beforeHotRewrite {
		t.Fatalf("expected 0 CLI calls on hot port rewrite, delta=%d", totalCalls.Load()-beforeHotRewrite)
	}
	if st3.Endpoint == st1.Endpoint {
		t.Fatalf("expected a newly allocated endpoint, got same %q", st3.Endpoint)
	}
}

func TestStartStandaloneBlocksWhenCLIOutdatedOrRunActive(t *testing.T) {
	var calls, oldTotal atomic.Int32
	sOld, _, model, engine := setupStandaloneTestService(t, false, &calls, &oldTotal)
	if sOld.ServeSupported(context.Background()) {
		t.Fatalf("expected ServeSupported=false for old CLI")
	}
	afterFirstProbe := oldTotal.Load()
	if sOld.ServeSupported(context.Background()) {
		t.Fatalf("expected cached ServeSupported=false for old CLI")
	}
	if oldTotal.Load() != afterFirstProbe {
		t.Fatalf("expected negative ServeSupported to be cached within TTL, delta=%d", oldTotal.Load()-afterFirstProbe)
	}

	// Outdated CLI semver (probe status == "outdated") is also cached within TTL.
	t.Run("semver_outdated_cached", func(t *testing.T) {
		prevVersion := Version
		Version = "1.0.0"
		defer func() { Version = prevVersion }()
		var outdatedVersionCalls, outdatedSchemaCalls atomic.Int32
		fakeOldCLI := filepath.Join(t.TempDir(), "inferyard-old")
		if err := os.WriteFile(fakeOldCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		oldWS := t.TempDir()
		sSemverOld := &Service{
			ctx:      context.Background(),
			settings: UserSettings{WorkspaceRoot: &oldWS, InferyardPath: &fakeOldCLI},
			exec: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				if len(args) == 1 && args[0] == "--version" {
					outdatedVersionCalls.Add(1)
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.1\n")}, nil
				}
				if len(args) == 2 && args[0] == "--schema" {
					outdatedSchemaCalls.Add(1)
				}
				return inferyard.Execution{ExitCode: 2}, nil
			},
		}
		if sSemverOld.ServeSupported(context.Background()) {
			t.Fatalf("expected ServeSupported=false for semver-outdated CLI")
		}
		if sSemverOld.ServeSupported(context.Background()) {
			t.Fatalf("expected cached ServeSupported=false for semver-outdated CLI")
		}
		if outdatedVersionCalls.Load() != 1 || outdatedSchemaCalls.Load() != 0 {
			t.Fatalf("expected 1 --version and 0 --schema across two ServeSupported calls, got version=%d schema=%d", outdatedVersionCalls.Load(), outdatedSchemaCalls.Load())
		}
	})
	if _, apiErr := sOld.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	}); apiErr == nil || apiErr.Code != "cli_outdated" {
		t.Fatalf("expected cli_outdated, got %+v", apiErr)
	}

	sActive, _, model2, engine2 := setupStandaloneTestService(t, true, &calls, nil)
	sActive.mu.Lock()
	sActive.runs = append(sActive.runs, &runEntry{Task: RunTask{State: "running"}})
	sActive.mu.Unlock()
	if _, apiErr := sActive.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model2, Engine: engine2, ContextSize: 8192,
	}); apiErr == nil || apiErr.Code != "run_active" {
		t.Fatalf("expected run_active, got %+v", apiErr)
	}

	// Cancelled context returns cancelled without launching.
	sCancel, _, model3, engine3 := setupStandaloneTestService(t, true, &calls, nil)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, apiErr := sCancel.StartStandalone(cancelledCtx, StandaloneStartRequest{
		Model: model3, Engine: engine3, ContextSize: 8192,
	}); apiErr == nil || apiErr.Code != "cancelled" {
		t.Fatalf("expected cancelled, got %+v", apiErr)
	}
}

func TestStartStandaloneRejectsSymlinkServeRootAndInstallationExecutableChange(t *testing.T) {
	var calls atomic.Int32
	s, workspace, model, engine := setupStandaloneTestService(t, true, &calls, nil)

	// In-workspace symlinked <slug> directory is also rejected.
	slug := standaloneSlug(model, engine, 8192)
	otherDir := filepath.Join(workspace, "serve", "other-combo")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherDir, filepath.Join(workspace, "serve", slug)); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	}); apiErr == nil || apiErr.Code != "invalid_input" {
		t.Fatalf("expected invalid_input for symlinked slug dir, got %+v", apiErr)
	}
	_ = os.RemoveAll(filepath.Join(workspace, "serve"))

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "serve")); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	}); apiErr == nil || apiErr.Code != "invalid_input" {
		t.Fatalf("expected invalid_input for symlinked serve dir, got %+v", apiErr)
	}

	// Two venv python symlinks pointing to the same base interpreter must NOT share cache.
	instDir := t.TempDir()
	basePython := filepath.Join(instDir, "base-python")
	if err := os.WriteFile(basePython, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	venvA := filepath.Join(instDir, "venv-a", "bin", "python")
	venvB := filepath.Join(instDir, "venv-b", "bin", "python")
	for _, link := range []string{venvA, venvB} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(basePython, link); err != nil {
			t.Fatal(err)
		}
	}
	instPath := filepath.Join(instDir, "installation.json")
	writeInst := func(exe string) {
		raw, _ := json.Marshal(map[string]any{"executable": exe, "family": "vllm"})
		if err := os.WriteFile(instPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeInst(venvA)
	modelDir := filepath.Join(instDir, "hf-model")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	planPath := filepath.Join(t.TempDir(), "serve-plan.json")
	planRaw, _ := json.Marshal(inferyard.ServePlan{
		SchemaVersion:    3,
		Argv:             []string{venvA, "--host", "127.0.0.1", "--port", strconv.Itoa(port)},
		Environment:      map[string]string{},
		WorkingDirectory: instDir,
		Endpoint:         "http://127.0.0.1:" + strconv.Itoa(port),
		ModelPath:        modelDir,
		ModelKind:        "directory",
		ContextSize:      8192,
		Family:           "vllm",
		Adapter:          "local_openai_v1",
		Local:            true,
	})
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	eng := registryEntry{installation: instPath, family: "vllm"}
	if _, _, _, ok := loadCachedStandalonePlan(planPath, modelDir, eng, 8192); !ok {
		t.Fatalf("expected cached venvA plan to be reusable")
	}
	writeInst(venvB)
	if _, _, _, ok := loadCachedStandalonePlan(planPath, modelDir, eng, 8192); ok {
		t.Fatalf("expected cached plan to invalidate when switching venvA -> venvB symlink")
	}
	// Symlinked serve-plan.json must be rejected.
	writeInst(venvA)
	linkPlan := filepath.Join(t.TempDir(), "serve-plan.json")
	if err := os.Symlink(planPath, linkPlan); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := loadCachedStandalonePlan(linkPlan, modelDir, eng, 8192); ok {
		t.Fatalf("expected symlinked serve-plan.json to be rejected")
	}
}
