package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain doubles the test binary as a fake llama-server: with ENGINE_HELPER
// set it parses --port, serves /health (503 until ENGINE_HELPER_DELAY_MS has
// elapsed, or always when ENGINE_HELPER_NOREADY), or exits immediately when
// ENGINE_HELPER_EXIT is set.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "local-queue-helper" {
		runLocalQueueHelper(os.Args[2:])
		return
	}
	if os.Getenv("ENGINE_HELPER") == "1" {
		if len(os.Args) > 1 && os.Args[1] == "run" {
			runDrainingHelper()
			return
		}
		runEngineHelper(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

func runEngineHelper(args []string) {
	ignoreHelperTermination()
	if os.Getenv("ENGINE_HELPER_EXIT") == "1" {
		os.Exit(3)
	}
	_, port := startupAddress(args)
	if port <= 0 {
		os.Exit(2)
	}
	delay, _ := strconv.Atoi(os.Getenv("ENGINE_HELPER_DELAY_MS"))
	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if os.Getenv("ENGINE_HELPER_NOREADY") == "1" || time.Since(started) < time.Duration(delay)*time.Millisecond {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe("127.0.0.1:"+strconv.Itoa(port), mux)
	os.Exit(1)
}

func engineWorkspace(t *testing.T, port int) (root, candidate string) {
	t.Helper()
	root = t.TempDir()
	candidate = filepath.Join(root, "candidates", "c1")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toml := "[engine]\nbinary_path = " + strconv.Quote(binary) + "\nstartup_args = [\"--port\", \"" + strconv.Itoa(port) + "\", \"--host\", \"127.0.0.1\"]\n"
	if err := os.WriteFile(filepath.Join(candidate, "candidate.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, candidate
}

func engineService(t *testing.T, workspace string) *Service {
	t.Helper()
	return engineServiceCtx(t, t.Context(), workspace)
}

func engineServiceCtx(t *testing.T, ctx context.Context, workspace string) *Service {
	t.Helper()
	raw := []byte(`{"workspace_root": ` + strconv.Quote(workspace) + `, "results_root": ` + strconv.Quote(filepath.Join(workspace, "results")) + `}`)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, Options{SettingsPath: settings, EngineStartTimeout: 3 * time.Second, Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if out, ok := version(args); ok {
			return out, nil
		}
		if out, ok := engineFixtureDescription(args); ok {
			return out, nil
		}
		return (inferyard.Client{Path: path}).Exec(ctx, args...)
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}

func pollEngine(t *testing.T, s *Service, want string) EngineState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state := s.Engine(); state.State == want {
			return state
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("engine never reached %q, now %q", want, s.Engine().State)
	return EngineState{}
}

func freePort(t *testing.T) int {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listen.Close() }()
	return listen.Addr().(*net.TCPAddr).Port
}

func TestStartEngineHealthyStopAndIdempotent(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_DELAY_MS", "300")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	first, e := s.StartEngine(candidate)
	if e != nil || first.State != "starting" || first.Pid <= 0 {
		t.Fatalf("%+v %v", first, e)
	}
	if _, err := os.Stat(ownerPath(workspace)); err != nil {
		t.Fatal("owner record missing", err)
	}
	again, e := s.StartEngine(candidate)
	if e != nil || again.Pid != first.Pid {
		t.Fatalf("start while starting must be a no-op: %+v %v", again, e)
	}
	other := filepath.Join(workspace, "candidates", "c2")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	candidateBytes, _ := os.ReadFile(filepath.Join(candidate, "candidate.toml"))
	if err := os.WriteFile(filepath.Join(other, "candidate.toml"), candidateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, failure := s.StartEngine(other); failure == nil || failure.Code != "engine_candidate_mismatch" {
		t.Fatal(failure)
	}
	running := pollEngine(t, s, "running")
	if running.Endpoint == "" || running.LogPath == "" {
		t.Fatalf("%+v", running)
	}
	if _, err := os.Stat(running.LogPath); err != nil {
		t.Fatal(err)
	}
	stopped, e := s.StopEngine()
	if e != nil {
		t.Fatalf("%v", e)
	}
	if stopped.State != "stopping" && stopped.State != "stopped" {
		t.Fatalf("%+v", stopped)
	}
	pollEngine(t, s, "stopped")
	assertOwnerGone(t, ownerPath(workspace))
}

func TestStartEnginePortInUse(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	port := freePort(t)
	hold, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Close() }()
	workspace, candidate := engineWorkspace(t, port)
	s := engineService(t, workspace)
	_, e := s.StartEngine(candidate)
	if e == nil || e.Code != "engine_port_in_use" {
		t.Fatalf("err = %+v", e)
	}
}

func TestStartEngineCrash(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_EXIT", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatalf("%v", e)
	}
	failed := pollEngine(t, s, "failed")
	if failed.Error == nil || failed.Error.Code != "engine_exited" {
		t.Fatalf("%+v", failed)
	}
}

func TestStartEngineTimeout(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_NOREADY", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatalf("%v", e)
	}
	failed := pollEngine(t, s, "failed")
	if failed.Error == nil || failed.Error.Code != "engine_start_timeout" {
		t.Fatalf("%+v", failed)
	}
}

func TestShutdownStopsManagedEngine(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	ctx, cancel := context.WithCancel(context.Background())
	s := engineServiceCtx(t, ctx, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatalf("%v", e)
	}
	running := pollEngine(t, s, "running")
	cancel()
	pollEngine(t, s, "stopped")
	if proc, err := os.FindProcess(int(running.Pid)); err == nil {
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			t.Fatal("engine process survived Studio shutdown")
		}
	}
}

// Only the engine test fixture consumes its tiny TOML stub. Production startup
// always obtains and validates the structured CLI description.
func engineFixtureDescription(args []string) (inferyard.Execution, bool) {
	if len(args) != 4 || args[0] != "config" || args[1] != "describe" || args[2] != "--candidate" {
		return inferyard.Execution{}, false
	}
	raw, err := os.ReadFile(args[3])
	if err != nil {
		return inferyard.Execution{ExitCode: 2}, true
	}
	var binary string
	var argv []string
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "binary_path":
			binary, _ = strconv.Unquote(strings.TrimSpace(value))
		case "startup_args":
			_ = json.Unmarshal([]byte(strings.TrimSpace(value)), &argv)
		}
	}
	hash := strings.Repeat("a", 64)
	details := map[string]any{"schema_version": 3, "definition": "engine-description.v1", "adapter": "llama_cpp_b11146_v1", "model_requests_sent": 0,
		"launch":   map[string]any{"kind": "direct_process", "argv": append([]string{binary}, argv...), "working_directory": nil, "environment": nil, "ownership": "operator"},
		"assets":   map[string]any{"engine": map[string]any{"path": binary, "sha256": hash}, "model": map[string]any{"path": "/fixture/model.gguf", "kind": "gguf", "content_sha256": hash}, "template": map[string]any{"path": "/fixture/template.jinja", "sha256": hash}},
		"endpoint": map[string]any{"url": "http://127.0.0.1:8080", "server_pid": 1, "process_start_ticks": 1}, "conditions": map[string]any{"context_size": 4096}, "generation": map[string]any{"max_tokens": 32, "temperature": 0}}
	result, _ := json.Marshal(map[string]any{"command": "config describe", "status": "prepared", "completeness": "complete", "run_id": nil, "evidence_dir": nil, "limitations": []string{}, "details": details})
	return inferyard.Execution{Stdout: result}, true
}
