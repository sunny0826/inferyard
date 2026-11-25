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

func TestShutdownKeepsStandaloneAndReopenAdoptsThenStops(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	port := freePort(t)
	workspace := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s1 := engineService(t, workspace)
	startup := Startup{
		Kind:             "direct_process",
		Path:             binary,
		Args:             []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port)},
		Family:           "llama-cpp",
		Adapter:          "prism_llama_server_v1",
		WorkingDirectory: filepath.Dir(binary),
		Endpoint:         "http://127.0.0.1:" + strconv.Itoa(port),
		ModelPath:        "/models/qwen.gguf",
		ModelName:        "qwen.gguf",
		EngineName:       "llama-cpp",
		ModelKind:        "gguf",
		ContextSize:      8192,
	}
	st1, apiErr := s1.startFromStartup(startup, EngineOriginStandalone, "qwen-llama-8192")
	if apiErr != nil {
		t.Fatalf("startFromStartup failed: %+v", apiErr)
	}
	st1 = pollEngine(t, s1, "running")

	// Default Shutdown keeps standalone service running and preserves engine-owner.json.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s1.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("s1.Shutdown failed: %v", err)
	}
	if _, err := os.Stat(ownerPath(workspace)); err != nil {
		t.Fatalf("expected engine-owner.json preserved on keep-services shutdown: %v", err)
	}
	if _, err := processStartTicks(st1.Pid); err != nil {
		t.Fatalf("expected standalone process %d still alive after shutdown: %v", st1.Pid, err)
	}

	// Reopen a new Service on the same workspace: verifies PID+StartTicks and auto-adopts.
	s2 := engineService(t, workspace)
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = s2.ShutdownWithOptions(ctx, true)
	})
	st2 := s2.Engine()
	if st2.State != "running" || !st2.Adopted || st2.Pid != st1.Pid || st2.Origin != EngineOriginStandalone {
		t.Fatalf("expected adopted running standalone service, got %+v", st2)
	}
	if st2.ModelName != "qwen.gguf" || st2.EngineName != "llama-cpp" || st2.Port != port {
		t.Fatalf("expected preserved metadata on adoption, got %+v", st2)
	}

	// StopEngine terminates the adopted service cleanly and clears engine-owner.json.
	if _, stopErr := s2.StopEngine(); stopErr != nil {
		t.Fatalf("StopEngine on adopted service failed: %+v", stopErr)
	}
	pollEngine(t, s2, "stopped")
	assertOwnerGone(t, ownerPath(workspace))
	wait(t, func() bool {
		_, err := processStartTicks(st1.Pid)
		return errors.Is(err, os.ErrNotExist)
	})
}

func TestAdoptionRefusedWhenIdentityUnreadable(t *testing.T) {
	workspace := t.TempDir()
	record := engineOwner{
		PID:        4242,
		StartTicks: 123,
		Origin:     EngineOriginStandalone,
		Family:     "llama-cpp",
		Engine:     "llama-cpp",
		Model:      "/models/qwen.gguf",
		ModelName:  "qwen.gguf",
		Endpoint:   "http://127.0.0.1:48857",
		LogFile:    "engine.log",
		Candidate:  "qwen-llama-8192",
	}
	if err := writeEngineOwner(ownerPath(workspace), record); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath: settingsPath,
		ProcessStartTicks: func(int64) (int64, error) {
			return 0, os.ErrPermission
		},
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			return inferyard.Execution{}, errors.New("unexpected CLI call")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	got := s.Engine()
	if got.State != "orphaned" || got.Adopted {
		t.Fatalf("expected orphaned (refused adoption), got %+v", got)
	}
	if got.Error == nil || got.Error.Code != "service_identity_unreadable" {
		t.Fatalf("expected service_identity_unreadable error, got %+v", got.Error)
	}
}

func TestExitedWhileAwayPersistsNoticeWithOOMHintAndDismiss(t *testing.T) {
	workspace := t.TempDir()
	logDir := filepath.Join(workspace, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logFile := "engine-away.log"
	logContent := "loading model weights\nmetal buffer alloc failed: out of memory\nprocess exiting\n"
	if err := os.WriteFile(filepath.Join(logDir, logFile), []byte(logContent), 0o600); err != nil {
		t.Fatal(err)
	}
	gpu := 32
	record := engineOwner{
		PID:         999999,
		StartTicks:  123,
		Origin:      EngineOriginStandalone,
		Family:      "llama-cpp",
		Engine:      "llama-cpp",
		Model:       "/models/big.gguf",
		ModelName:   "big.gguf",
		Port:        48999,
		ContextSize: 32768,
		GPULayers:   &gpu,
		Endpoint:    "http://127.0.0.1:48999",
		LogFile:     logFile,
		Candidate:   "big-llama-32768",
	}
	if err := writeEngineOwner(ownerPath(workspace), record); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath: settingsPath,
		ProcessStartTicks: func(int64) (int64, error) {
			return 0, os.ErrNotExist
		},
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			return inferyard.Execution{}, errors.New("unexpected CLI call")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	// engine-owner.json should be cleaned up after recording the exit notice.
	assertOwnerGone(t, ownerPath(workspace))
	if s.Engine().State != "stopped" {
		t.Fatalf("expected stopped state after dead standalone cleanup, got %+v", s.Engine())
	}

	notice := s.ExitedServiceNotice()
	if notice == nil {
		t.Fatalf("expected ServiceExitNotice recorded for exited-while-away standalone service")
	}
	if !notice.WhileAway || notice.Model != "big.gguf" || notice.Engine != "llama-cpp" || notice.Port != 48999 || notice.ContextSize != 32768 {
		t.Fatalf("unexpected notice fields: %+v", notice)
	}
	if notice.ExitCode != nil {
		t.Fatalf("expected nil exit_code for exited-while-away, got %v", *notice.ExitCode)
	}
	if len(notice.LogTail) < 2 || notice.Hint == "" {
		t.Fatalf("expected log tail and OOM hint, got tail=%v hint=%q", notice.LogTail, notice.Hint)
	}

	if err := s.DismissExitedServiceNotice(); err != nil {
		t.Fatalf("DismissExitedServiceNotice failed: %v", err)
	}
	if got := s.ExitedServiceNotice(); got != nil {
		t.Fatalf("expected nil notice after dismiss, got %+v", got)
	}
}

func TestAdoptedStandaloneUnexpectedExitPublishesNotice(t *testing.T) {
	f := &ownerFixture{ticks: 123}
	workspace := t.TempDir()
	logDir := filepath.Join(workspace, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "engine-live.log"), []byte("CUDA out of memory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := engineOwner{
		PID:         4242,
		StartTicks:  123,
		Origin:      EngineOriginStandalone,
		Family:      "llama-cpp",
		Engine:      "llama-cpp",
		Model:       "/models/qwen.gguf",
		ModelName:   "qwen.gguf",
		Port:        48888,
		ContextSize: 8192,
		Endpoint:    "http://127.0.0.1:48888",
		LogFile:     "engine-live.log",
		Candidate:   "qwen-llama-8192",
	}
	if err := writeEngineOwner(ownerPath(workspace), record); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath:      settingsPath,
		ProcessStartTicks: f.read,
		SignalProcess:     f.signal,
		OrphanStopTimeout: time.Millisecond,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			return inferyard.Execution{}, errors.New("unexpected CLI call")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if st := s.Engine(); st.State != "running" || !st.Adopted {
		t.Fatalf("expected adopted running service, got %+v", st)
	}

	_, unsub, cursor := s.Bus.Subscribe()
	defer unsub()

	// Simulate unexpected crash of adopted process.
	f.set(0, nil)

	pollEngine(t, s, "failed")
	assertOwnerGone(t, ownerPath(workspace))

	notice := s.ExitedServiceNotice()
	if notice == nil || notice.WhileAway || notice.Hint == "" {
		t.Fatalf("expected in-session unexpected exit notice with OOM hint, got %+v", notice)
	}

	events, _, _ := s.Bus.Since(cursor)
	gotEvent := false
	for _, evt := range events {
		if evt.Type == "service.exited" {
			gotEvent = true
			break
		}
	}
	if !gotEvent {
		t.Fatalf("expected service.exited event in bus replay, got %+v", events)
	}
}
