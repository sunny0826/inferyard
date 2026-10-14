package core

import (
	"archive/zip"
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
	if _, err := os.Stat(*task.EnginePath); err != nil {
		t.Fatal(err)
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
	if len(states) != 2 || states[0] != "downloading" || states[1] != "done" || progress == 0 {
		t.Fatalf("events: %v progress=%d", states, progress)
	}
	if _, e := s.CancelDownload(task.TaskId); e == nil || e.Details["reason"] != "invalid_state" {
		t.Fatalf("terminal cancel: %v", e)
	}
}

func TestEngineDownloadCancel(t *testing.T) {
	s := engineDownloadService(t)
	started := make(chan struct{})
	installHTTPDo = func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	task, e := s.CreateEngineDownload(engineSource, "cancel")
	if e != nil {
		t.Fatal(e)
	}
	<-started
	if _, e := s.CancelDownload(task.TaskId); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })
	task = s.Downloads()[0]
	assertEngineNullProgress(t, task)
	if task.EnginePath != nil || task.Error != nil || task.FinishedAt == nil {
		t.Fatalf("cancel: %+v", task)
	}
	if _, e := s.CreateEngineDownload(engineSource, "cancel"); e == nil || e.Details["reason"] != "output_exists" {
		t.Fatalf("cancelled reservation: %v", e)
	}
}

func TestEngineDownloadFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		status int
		code   string
	}{
		{"network", nil, 503, "download_failed"},
		{"corrupt", []byte("bad archive"), 200, "engine_unpack_failed"},
		{"missing", engineTarGz(t, map[string]string{"README": "text"}), 200, "engine_binary_missing"},
		{"link", engineTarGzWithLink(t, "evil", "../../escape"), 200, "engine_unpack_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := engineDownloadService(t)
			installHTTPDo = func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(bytes.NewReader(tc.body))}, nil
			}
			if _, e := s.CreateEngineDownload(engineSource, tc.name); e != nil {
				t.Fatal(e)
			}
			wait(t, func() bool { return s.Downloads()[0].State == "failed" })
			task := s.Downloads()[0]
			if task.Error == nil || task.Error.Code != tc.code || task.EnginePath != nil || task.FinishedAt == nil {
				t.Fatalf("failure: %+v", task)
			}
			assertEngineNullProgress(t, task)
		})
	}
}

func TestEngineDownloadAdmission(t *testing.T) {
	s := engineDownloadService(t)
	for _, name := range []string{"", ".", "..", "...", "../escape", "a/b", "a b"} {
		if _, e := s.CreateEngineDownload(engineSource, name); e == nil || e.Code != "invalid_request" {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	if _, e := s.CreateEngineDownload("http://example.test/a", "valid"); e == nil || e.Code != "invalid_request" {
		t.Fatalf("source: %v", e)
	}
	root := filepath.Join(*s.settings.WorkspaceRoot, "engines")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "exists"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := s.CreateEngineDownload(engineSource, "exists"); e == nil || e.Details["reason"] != "output_exists" {
		t.Fatalf("existing file: %v", e)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, e := s.CreateEngineDownload(engineSource, "escape"); e == nil || e.Code != "invalid_request" {
		t.Fatalf("escape: %v", e)
	}
	s.settings.WorkspaceRoot = nil
	if _, e := s.CreateEngineDownload(engineSource, "valid"); e == nil || e.Details["reason"] != "settings_incomplete" {
		t.Fatalf("settings: %v", e)
	}
	s.Shutdown(context.Background())
	if _, e := s.CreateEngineDownload(engineSource, "valid"); e == nil || e.Code != "service_closing" {
		t.Fatalf("closing: %v", e)
	}
}

func TestEngineExtractionCancellation(t *testing.T) {
	for _, format := range []string{"tar.gz", "zip", "bin"} {
		t.Run(format, func(t *testing.T) {
			s := engineDownloadService(t)
			archive := engineTarGz(t, map[string]string{engineBinaryName(): "binary"})
			if format == "zip" {
				var buf bytes.Buffer
				zw := zip.NewWriter(&buf)
				w, err := zw.Create(engineBinaryName())
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte("binary"))
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				archive = buf.Bytes()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			installHTTPDo = func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}
			out := filepath.Join(t.TempDir(), "out")
			path, e := s.FetchEngine(ctx, "https://example.test/engine."+format, out, func(status string) {
				if status == "正在解压引擎…" {
					cancel()
				}
				if format == "bin" {
					cancel()
				}
			})
			if e == nil || path != "" {
				t.Fatalf("cancel extraction: %q %v", path, e)
			}
			if _, err := os.Stat(filepath.Join(out, engineBinaryName())); !os.IsNotExist(err) {
				t.Fatalf("wrote cancelled binary: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buf := make([]byte, 1)
	if _, err := (engineContextReader{ctx, bytes.NewReader([]byte("x"))}).Read(buf); err != context.Canceled {
		t.Fatalf("reader: %v", err)
	}
}

func TestModelDownloadJSONKeepsMeasuredBytes(t *testing.T) {
	raw, err := json.Marshal(DownloadTask{Kind: "model", DownloadedBytes: 42})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	json.Unmarshal(raw, &obj)
	if obj["downloaded_bytes"] != float64(42) || obj["progress_reason"] != nil {
		t.Fatalf("model JSON: %s", raw)
	}
}

func TestEngineDownloadJSONDropsCounters(t *testing.T) {
	rate := 1.5
	raw, err := json.Marshal(DownloadTask{Kind: "engine", DownloadedBytes: 99, BytesPerSecond: &rate})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	json.Unmarshal(raw, &obj)
	for _, key := range []string{"downloaded_bytes", "bytes_per_second"} {
		if value, exists := obj[key]; !exists || value != nil {
			t.Fatalf("%s = %v, JSON: %s", key, value, raw)
		}
	}
}

func TestFetchURLRejectsOversized(t *testing.T) {
	stubInstall(t)
	installHTTPDo = func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: maxFetchBytes + 1, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	if _, e := fetchURL(context.Background(), "https://example.test/big"); e == nil || e.Code != "download_too_large" {
		t.Fatalf("content-length: %v", e)
	}
	installHTTPDo = func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(bytes.NewReader(make([]byte, maxFetchBytes+1)))}, nil
	}
	if _, e := fetchURL(context.Background(), "https://example.test/big"); e == nil || e.Code != "download_too_large" {
		t.Fatalf("chunked body: %v", e)
	}
}

func TestEngineDownloadShutdownConverges(t *testing.T) {
	s := engineDownloadService(t)
	started := make(chan struct{})
	installHTTPDo = func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if _, e := s.CreateEngineDownload(engineSource, "shutdown"); e != nil {
		t.Fatal(e)
	}
	<-started
	s.Shutdown(context.Background())
	wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })
	task := s.Downloads()[0]
	assertEngineNullProgress(t, task)
	if task.EnginePath != nil || task.FinishedAt == nil {
		t.Fatalf("shutdown: %+v", task)
	}
}
