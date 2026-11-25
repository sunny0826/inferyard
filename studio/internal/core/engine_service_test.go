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
	"sync"
	"testing"
	"time"
)

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
	hangMs, _ := strconv.Atoi(os.Getenv("ENGINE_HELPER_HEALTH_HANG_MS"))
	signalFile := os.Getenv("ENGINE_HELPER_HEALTH_SIGNAL_FILE")
	var signalOnce sync.Once
	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if hangMs > 0 {
			if signalFile != "" {
				signalOnce.Do(func() { _ = os.WriteFile(signalFile, []byte("1"), 0o600) })
			}
			timer := time.NewTimer(time.Duration(hangMs) * time.Millisecond)
			select {
			case <-timer.C:
			case <-r.Context().Done():
				timer.Stop()
				return
			}
		}
		if os.Getenv("ENGINE_HELPER_NOREADY") == "1" || time.Since(started) < time.Duration(delay)*time.Millisecond {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		if body := os.Getenv("ENGINE_HELPER_HEALTH_BODY"); body != "" {
			_, _ = w.Write([]byte(body))
		}
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
	raw := []byte(`{"workspace_root": ` + strconv.Quote(workspace) + `}`)
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

func TestStartingProbeIntervalProgression(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, 150 * time.Millisecond},
		{4999 * time.Millisecond, 150 * time.Millisecond},
		{5 * time.Second, 500 * time.Millisecond},
		{14999 * time.Millisecond, 500 * time.Millisecond},
		{15 * time.Second, time.Second},
		{30 * time.Second, time.Second},
	}
	for _, tc := range cases {
		if got := startingProbeInterval(tc.elapsed); got != tc.want {
			t.Fatalf("startingProbeInterval(%v)=%v want %v", tc.elapsed, got, tc.want)
		}
	}
}

func TestEngineFastHealthReadinessUnder400ms(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_DELAY_MS", "50")
	port := freePort(t)
	workspace, candidate := engineWorkspace(t, port)
	s := engineService(t, workspace)

	start := time.Now()
	st, apiErr := s.StartEngine(candidate)
	if apiErr != nil {
		t.Fatalf("StartEngine failed: %+v", apiErr)
	}
	if st.State != "starting" {
		t.Fatalf("initial state=%q want starting", st.State)
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cur := s.Engine(); cur.State == "running" {
			elapsed := time.Since(start)
			if elapsed >= 400*time.Millisecond {
				t.Fatalf("engine became running in %v, want < 400ms", elapsed)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine did not reach running within 400ms (state=%q elapsed=%v)", s.Engine().State, time.Since(start))
}

func TestEngineStopInterruptsSlowHealthProbe(t *testing.T) {
	signalFile := filepath.Join(t.TempDir(), "health-received")
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_HEALTH_HANG_MS", "1800")
	t.Setenv("ENGINE_HELPER_HEALTH_SIGNAL_FILE", signalFile)
	port := freePort(t)
	workspace, candidate := engineWorkspace(t, port)
	s := engineService(t, workspace)

	if _, apiErr := s.StartEngine(candidate); apiErr != nil {
		t.Fatalf("StartEngine failed: %+v", apiErr)
	}
	sigDeadline := time.Now().Add(4 * time.Second)
	for {
		if _, err := os.Stat(signalFile); err == nil {
			break
		}
		if time.Now().After(sigDeadline) {
			t.Fatalf("health handler never received probe request")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopStart := time.Now()
	if _, apiErr := s.StopEngine(); apiErr != nil {
		t.Fatalf("StopEngine failed: %+v", apiErr)
	}
	deadline := stopStart.Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		if s.Engine().State == "stopped" {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("StopEngine blocked by in-flight health probe: state=%q elapsed=%v", s.Engine().State, time.Since(stopStart))
}

func TestEngineReadyRightBeforeDeadline(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_DELAY_MS", "480")
	port := freePort(t)
	workspace, candidate := engineWorkspace(t, port)
	s := engineService(t, workspace)
	s.engineTimeout = 600 * time.Millisecond

	if _, apiErr := s.StartEngine(candidate); apiErr != nil {
		t.Fatalf("StartEngine failed: %+v", apiErr)
	}
	st := pollEngine(t, s, "running")
	if st.State != "running" {
		t.Fatalf("expected running when ready before deadline, got %+v", st)
	}
}
