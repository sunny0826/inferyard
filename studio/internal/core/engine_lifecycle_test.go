package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runDrainingHelper() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	_ = os.WriteFile(os.Getenv("RUN_READY"), []byte("ready"), 0600)
	<-signals
	time.Sleep(150 * time.Millisecond)
	response, err := http.Get(os.Getenv("RUN_ENGINE") + "/health")
	if err != nil || response.StatusCode != 200 {
		os.Exit(4)
	}
	response.Body.Close()
	_ = os.WriteFile(os.Getenv("RUN_DRAINED"), []byte("drained"), 0600)
	os.Exit(130)
}

func TestOrderedShutdownDrainsRealRunBeforeEngine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT drain helper requires POSIX signals")
	}
	t.Setenv("ENGINE_HELPER", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	drained, ready := filepath.Join(workspace, "drained"), filepath.Join(workspace, "ready")
	t.Setenv("RUN_DRAINED", drained)
	t.Setenv("RUN_READY", ready)
	s := engineService(t, workspace)
	s.settings.InferyardPath = ptr(os.Args[0])
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if out, ok := engineFixtureDescription(args); ok {
			return out, nil
		}
		return (inferyard.Client{Path: path}).Exec(ctx, args...)
	}
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	engine := pollEngine(t, s, "running")
	t.Setenv("RUN_ENGINE", engine.Endpoint)
	cfg := config(t, *s.settings.ResultsRoot, "zh-core")
	tasks, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{cfg}, AllowSingleBundle: ptr(true)})
	if e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { _, e := os.Stat(ready); return e == nil })
	if _, e := s.StopEngine(); e == nil || e.Code != "run_active" {
		t.Fatalf("stop during run: %v", e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(drained); err != nil {
		t.Fatal("run could not drain against live engine:", err)
	}
	detail, e := s.RunDetail(tasks[0].TaskId)
	if e != nil || detail.Task.State != "cancelled" || detail.Task.Exit == nil {
		t.Fatalf("run not finalized: %+v %v", detail, e)
	}
	if s.Engine().State != "stopped" || s.engine.cmd.ProcessState == nil {
		t.Fatal("shutdown returned before engine reaped")
	}
	if _, e := processStartTicks(engine.Pid); !os.IsNotExist(e) {
		t.Fatalf("process survived: %v", e)
	}
	if _, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{cfg}, AllowSingleBundle: ptr(true)}); e == nil {
		t.Fatal("shutdown accepted run")
	}
	if _, e := s.StartEngine(candidate); e == nil || e.Code != "service_closing" {
		t.Fatalf("shutdown accepted engine: %v", e)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStopEngineEscalatesAndReaps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM grace is POSIX-only")
	}
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_IGNORE_TERM", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	state := pollEngine(t, s, "running")
	if _, e := s.StopEngine(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-s.engine.done:
	case <-time.After(15 * time.Second):
		t.Fatal("SIGKILL fallback did not reap")
	}
	if s.Engine().State != "stopped" || s.engine.cmd.ProcessState == nil {
		t.Fatal("incomplete stop")
	}
	if _, e := processStartTicks(state.Pid); !os.IsNotExist(e) {
		t.Fatalf("process survived: %v", e)
	}
}

func TestBindingRecoveryAcrossRestartAndCrash(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	engine := pollEngine(t, s, "running")
	ticks, err := processStartTicks(engine.Pid)
	if err != nil {
		t.Fatal(err)
	}
	bound := filepath.Join(workspace, "bound", "zh-core-1")
	if err := os.MkdirAll(bound, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte(fmt.Sprintf("[endpoint]\nserver_pid = %d\nprocess_start_ticks = %d\n", engine.Pid, ticks))
	path := filepath.Join(bound, "config.toml")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	reopened := engineService(t, workspace)
	if reopened.Engine().State != "orphaned" || reopened.engine != nil {
		t.Fatal("ownership hint must not adopt a managed child")
	}
	if alive, e := reopened.BindingAlive(bound); e != nil || !alive {
		t.Fatalf("live binding: %v %v", alive, e)
	}
	if _, e := reopened.StopEngine(); e != nil {
		t.Fatal(e)
	}
	if alive, e := reopened.BindingAlive(bound); e != nil || !alive {
		t.Fatal("stopped unowned process")
	}
	mismatch := []byte(fmt.Sprintf("[endpoint]\nserver_pid = %d\nprocess_start_ticks = %d\n", engine.Pid, ticks+1))
	os.WriteFile(path, mismatch, 0600)
	if alive, e := reopened.BindingAlive(bound); e != nil || alive {
		t.Fatalf("PID reuse accepted: %v %v", alive, e)
	}
	os.WriteFile(path, content, 0600)
	s.engine.cmd.Process.Kill()
	pollEngine(t, s, "failed")
	if alive, e := reopened.BindingAlive(bound); e != nil || alive {
		t.Fatalf("dead binding: %v %v", alive, e)
	}
	if _, e := reopened.StopOrphanEngine(); e != nil {
		t.Fatal(e)
	}
	if _, e := reopened.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	pollEngine(t, reopened, "running")
	if alive, e := reopened.BindingAlive(bound); e != nil || alive {
		t.Fatal("new service incorrectly revived old binding")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(content) {
		t.Fatal("old binding overwritten")
	}
}

func TestStartValidationRejectsBeforeLaunch(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		args       []string
		code       string
	}{
		{name: "wildcard", args: []string{"--host", "0.0.0.0", "--port", "48857"}, code: "engine_host_not_loopback"},
		{name: "inline", args: []string{"--host=192.168.1.1", "--port=48857"}, code: "engine_host_not_loopback"},
		{name: "credential", args: []string{"--host", "127.0.0.1", "--port", "48857", "--api-key=secret"}, code: "engine_credentials_forbidden"},
		{name: "token", args: []string{"--host", "127.0.0.1", "--port", "48857", "sk-secret"}, code: "engine_credentials_forbidden"},
		{name: "missing", path: "/missing/engine", args: []string{"--host", "127.0.0.1", "--port", "48857"}, code: "engine_not_executable"},
		{name: "permission", path: "non-executable", args: []string{"--host", "127.0.0.1", "--port", "48857"}, code: "engine_not_executable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace, candidate := engineWorkspace(t, freePort(t))
			path, _ := os.Executable()
			if tc.path != "" {
				path = tc.path
			}
			if tc.path == "non-executable" {
				path = filepath.Join(workspace, "not-executable")
				os.WriteFile(path, []byte("not executable"), 0600)
			}
			args, _ := json.Marshal(tc.args)
			raw := fmt.Sprintf("[engine]\nbinary_path = %q\nstartup_args = %s\n", path, args)
			os.WriteFile(filepath.Join(candidate, "candidate.toml"), []byte(raw), 0600)
			s := engineService(t, workspace)
			if _, e := s.StartEngine(candidate); e == nil || e.Code != tc.code || strings.Contains(e.Message, "secret") {
				t.Fatalf("%+v", e)
			}
			if s.engine != nil {
				t.Fatal("started rejected engine")
			}
			if _, err := os.Stat(filepath.Join(workspace, "logs")); !os.IsNotExist(err) {
				t.Fatal("created log for rejected command")
			}
		})
	}
}

func ignoreHelperTermination() {
	if os.Getenv("ENGINE_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
}
