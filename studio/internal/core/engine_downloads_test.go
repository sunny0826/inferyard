package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const engineSource = "https://example.test/engine.tar.gz"

func engineDownloadService(t *testing.T) *Service {
	t.Helper()
	stubInstall(t)
	s := installService(t)
	s.settings.WorkspaceRoot = ptr(t.TempDir())
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s
}

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

func TestEngineDownloadLifecycle(t *testing.T) {
	s := engineDownloadService(t)
	archive := engineTarGzWithLink(t, "libimpl.dylib", "libimpl.0.dylib")
	gate := make(chan struct{})
	defer close(gate)
	installHTTPDo = func(req *http.Request) (*http.Response, error) {
		select {
		case <-gate:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	}
	_, unsubscribe, cursor := s.Bus.Subscribe()
	defer unsubscribe()
	task, e := s.CreateEngineDownload(engineSource, "prism")
	if e != nil {
		t.Fatal(e)
	}
	if task.Kind != "engine" || task.State != "downloading" || len(s.Downloads()) != 1 {
		t.Fatalf("task: %+v", task)
	}
	expected, err := Resolve(*s.settings.WorkspaceRoot, "engines/prism")
	if err != nil || task.OutDir != expected {
		t.Fatalf("out: %q %v", task.OutDir, err)
	}
	assertEngineNullProgress(t, task)
	if _, e := s.CreateEngineDownload(engineSource, "prism"); e == nil || e.Details["reason"] != "output_exists" {
		t.Fatalf("duplicate: %v", e)
	}
	gate <- struct{}{}
	wait(t, func() bool { return s.Downloads()[0].State == "done" })
	task = s.Downloads()[0]
	assertEngineNullProgress(t, task)
	if task.EnginePath == nil || task.FinishedAt == nil || task.Result != nil {
		t.Fatalf("result: %+v", task)
	}
	if info, err := os.Stat(*task.EnginePath); err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("downloaded engine is not executable", info, err)
	}
	if body, err := os.ReadFile(filepath.Join(task.OutDir, "dist/libimpl.dylib")); err != nil || string(body) != "lib" {
		t.Fatalf("library: %q %v", body, err)
	}
	events, reset, _ := s.Bus.After(cursor)
	if reset {
		t.Fatal("unexpected event reset")
	}
	states := []string{}
	progress := 0
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
			json.Unmarshal(event.Data, &payload)
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
