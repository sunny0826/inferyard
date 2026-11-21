package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const engineSource = "https://example.test/engine.tar.gz"

func assertEngineNullProgress(t *testing.T, task DownloadTask) {
	t.Helper()
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"downloaded_bytes", "bytes_per_second"} {
		value, exists := obj[key]
		if !exists || value != nil {
			t.Fatalf("%s = %v, JSON: %s", key, value, raw)
		}
	}
	if obj["progress_reason"] != "engine_transfer_progress_unavailable" {
		t.Fatalf("missing reason: %s", raw)
	}
}

func enginePrepareResult(enginePath string, bytes int64, sha256 string) inferyard.Execution {
	payload, _ := json.Marshal(map[string]any{
		"command":      "engine prepare",
		"status":       "prepared",
		"completeness": "complete",
		"limitations":  []string{},
		"details": map[string]any{
			"engine":              enginePath,
			"bytes":               bytes,
			"sha256":              sha256,
			"ready_to_run":        false,
			"model_requests_sent": 0,
		},
	})
	return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}
}

func TestEngineDownloadLifecycleAndCatalogArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	entry, ok := CatalogEntryByID("engine-macos-arm64")
	if !ok {
		t.Fatal("missing catalog entry engine-macos-arm64")
	}
	gate := make(chan struct{})
	var mu sync.Mutex
	var gotArgs []string
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
			return result(inferyard.ExitBlocked), nil
		}
		mu.Lock()
		gotArgs = slices.Clone(args)
		mu.Unlock()
		select {
		case <-gate:
		case <-ctx.Done():
			return result(inferyard.ExitCancelled), ctx.Err()
		}
		return enginePrepareResult(filepath.Join(args[5], "dist", "llama-server"), entry.Bytes, entry.SHA256), nil
	})
	workspace := t.TempDir()
	s.settings.WorkspaceRoot = &workspace

	_, unsubscribe, cursor := s.Bus.Subscribe()
	defer unsubscribe()
	task, e := s.CreateEngineDownload(entry.URL, "prism")
	if e != nil {
		t.Fatal(e)
	}
	if task.Kind != "engine" || task.State != "downloading" || len(s.Downloads()) != 1 {
		t.Fatalf("task: %+v", task)
	}
	expectedDir, err := Resolve(workspace, "engines/prism")
	if err != nil || task.OutDir != expectedDir {
		t.Fatalf("out: %q %v", task.OutDir, err)
	}
	assertEngineNullProgress(t, task)
	if _, e := s.CreateEngineDownload(entry.URL, "prism"); e == nil || e.Details["reason"] != "output_exists" {
		t.Fatalf("duplicate: %v", e)
	}
	close(gate)
	wait(t, func() bool { return s.Downloads()[0].State == "done" })

	wantArgs := []string{
		"engine", "prepare",
		"--source", entry.URL,
		"--out", expectedDir,
		"--expected-sha256", entry.SHA256,
	}
	mu.Lock()
	actualArgs := slices.Clone(gotArgs)
	mu.Unlock()
	if !slices.Equal(actualArgs, wantArgs) {
		t.Fatalf("cli args = %v, want %v", actualArgs, wantArgs)
	}
	task = s.Downloads()[0]
	assertEngineNullProgress(t, task)
	wantEngine := filepath.Join(expectedDir, "dist", "llama-server")
	if task.EnginePath == nil || *task.EnginePath != wantEngine || task.FinishedAt == nil {
		t.Fatalf("engine path: %+v", task)
	}
	if task.Result == nil || task.Result.Bytes != entry.Bytes || task.Result.Sha256 != entry.SHA256 {
		t.Fatalf("result: %+v", task.Result)
	}

	events, reset, _ := s.Bus.After(cursor)
	if reset {
		t.Fatal("unexpected event reset")
	}
	states, progress := []string{}, 0
	for _, event := range events {
		switch event.Type {
		case "download.state":
			var payload struct {
				Task DownloadTask `json:"task"`
			}
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				t.Fatal(err)
			}
			states = append(states, payload.Task.State)
			assertEngineNullProgress(t, payload.Task)
		case "download.progress":
			var payload map[string]any
			_ = json.Unmarshal(event.Data, &payload)
			if payload["downloaded_bytes"] != nil || payload["bytes_per_second"] != nil || payload["progress_reason"] == nil {
				t.Fatalf("progress: %s", event.Data)
			}
			progress++
		}
	}
	if len(states) < 2 || states[0] != "downloading" || states[len(states)-1] != "done" || progress == 0 {
		t.Fatalf("events: %v progress=%d", states, progress)
	}
	if _, e := s.CancelDownload(task.TaskId); e == nil || e.Details["reason"] != "invalid_state" {
		t.Fatalf("terminal cancel: %v", e)
	}
}

func TestEngineDownloadWindowsCUDAArgsAndCustomURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cuda, ok := CatalogEntryByID("engine-windows-cuda-x64")
	if !ok {
		t.Fatal("missing catalog entry engine-windows-cuda-x64")
	}
	var mu sync.Mutex
	recorded := map[string][]string{}
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
			return result(inferyard.ExitBlocked), nil
		}
		mu.Lock()
		recorded[args[3]] = slices.Clone(args)
		mu.Unlock()
		return enginePrepareResult(filepath.Join(args[5], "llama-server.exe"), 123, strings.Repeat("b", 64)), nil
	})
	workspace := t.TempDir()
	s.settings.WorkspaceRoot = &workspace

	t1, e := s.CreateEngineDownload(cuda.URL, "cuda-win")
	if e != nil {
		t.Fatal(e)
	}
	t2, e := s.CreateEngineDownload(engineSource, "custom")
	if e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool {
		ds := s.Downloads()
		return len(ds) == 2 && ds[0].State == "done" && ds[1].State == "done"
	})

	wantCUDA := []string{
		"engine", "prepare",
		"--source", cuda.URL,
		"--out", t1.OutDir,
		"--expected-sha256", cuda.SHA256,
		"--profile", cuda.Profile,
		"--runtime-source", cuda.RuntimeURL,
		"--runtime-expected-sha256", cuda.RuntimeSHA256,
	}
	if !slices.Equal(recorded[cuda.URL], wantCUDA) {
		t.Fatalf("cuda args = %v, want %v", recorded[cuda.URL], wantCUDA)
	}
	wantCustom := []string{
		"engine", "prepare",
		"--source", engineSource,
		"--out", t2.OutDir,
	}
	if !slices.Equal(recorded[engineSource], wantCustom) {
		t.Fatalf("custom args = %v, want %v", recorded[engineSource], wantCustom)
	}
}

func TestEngineDownloadCancellationBlockedAndCLIUnavailable(t *testing.T) {
	started := make(chan struct{}, 1)
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		started <- struct{}{}
		<-ctx.Done()
		return result(inferyard.ExitCancelled), ctx.Err()
	})
	workspace := t.TempDir()
	s.settings.WorkspaceRoot = &workspace

	task, e := s.CreateEngineDownload(engineSource, "cancel-me")
	if e != nil {
		t.Fatal(e)
	}
	<-started
	if _, e := s.CancelDownload(task.TaskId); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })

	blockedService := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		return inferyard.Execution{
			ExitCode: inferyard.ExitBlocked,
			Stdout:   []byte(`{"command":"engine prepare","status":"blocked","completeness":"incomplete","limitations":["engine_checksum_mismatch"]}`),
		}, nil
	})
	blockedWorkspace := t.TempDir()
	blockedService.settings.WorkspaceRoot = &blockedWorkspace
	if _, e := blockedService.CreateEngineDownload(engineSource, "bad-sha"); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return blockedService.Downloads()[0].State == "failed" })
	failed := blockedService.Downloads()[0]
	if failed.Error == nil || failed.Error.Code != "cli_blocked" || !slices.Equal(failed.Error.Limitations, []string{"engine_checksum_mismatch"}) {
		t.Fatalf("blocked failure: %+v", failed.Error)
	}

	unavailable := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		return inferyard.Execution{ExitCode: inferyard.ExitToolError}, nil
	})
	unavailableWorkspace := t.TempDir()
	unavailable.settings.WorkspaceRoot = &unavailableWorkspace
	unavailable.settings.InferyardPath = ptr(filepath.Join(t.TempDir(), "missing-inferyard"))
	if _, e := unavailable.CreateEngineDownload(engineSource, "no-cli"); e == nil || e.Code != "cli_unavailable" {
		t.Fatalf("cli_unavailable = %+v", e)
	}
}
