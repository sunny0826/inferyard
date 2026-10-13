package server

import (
	"bytes"
	"context"
	"encoding/json"
	"inferyard/studio/internal/core"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHistorySkipsCorruptRuns(t *testing.T) {
	root := t.TempDir()
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := func(dir, id string) {
		t.Helper()
		for _, name := range []string{"inputs.json", "manifest.json", "events.jsonl"} {
			raw, err := os.ReadFile(filepath.Join("../evidence/testdata/run", name))
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(dir, name), []byte(strings.ReplaceAll(string(raw), "fixture-run", id)))
		}
	}
	for _, prefix := range []string{"standalone", "batch/runs"} {
		fixture(filepath.Join(root, prefix, "good"), prefix+"-good")
		fixture(filepath.Join(root, prefix, "bad-manifest"), prefix+"-bad-manifest")
		write(filepath.Join(root, prefix, "bad-manifest", "manifest.json"), []byte(`{`))
		fixture(filepath.Join(root, prefix, "bad-events"), prefix+"-bad-events")
		write(filepath.Join(root, prefix, "bad-events", "events.jsonl"), []byte("{broken}\n"))
	}
	write(filepath.Join(root, "batch", "batch.json"), []byte(`{"schema_version":3}`))
	fixture(filepath.Join(root, "cancelled"), "cancelled-run")
	write(filepath.Join(root, "cancelled", "events.jsonl"), []byte("{\"seq\":1,\"event_type\":\"run_stopped\",\"data\":{\"reason\":\"user_cancelled\"}}\n"))
	configDir := t.TempDir()
	settings := filepath.Join(configDir, "settings.json")
	raw, _ := json.Marshal(map[string]string{"results_root": root})
	write(settings, raw)
	host := filepath.Join(configDir, "host.json")
	write(host, []byte(`{"schema_version":1,"dirty":true,"run_id":"cancelled-run","kind":"run"}`))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := core.New(ctx, core.Options{SettingsPath: settings, HostStatePath: host})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewWithCore(ctx, fstest.MapFS{}, c)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	code, body := request(t, s, ts, "GET", "/api/runs", "")
	if code != 200 {
		t.Fatalf("%d: %s", code, body)
	}
	var history core.RunHistoryResponse
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.StandaloneRuns) != 2 || len(history.Batches) != 1 || len(history.Batches[0].Runs) != 1 {
		t.Fatalf("unexpected history: %s", body)
	}
	found := false
	for _, run := range history.StandaloneRuns {
		if run.RunId == "cancelled-run" {
			found = true
			if run.Status != "cancelled" {
				t.Fatal(run.Status)
			}
		}
	}
	if !found {
		t.Fatal("cancelled run missing")
	}
	if !strings.Contains(logs.String(), "skipped 4 entries") {
		t.Fatal(logs.String())
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(body, &shape); err != nil {
		t.Fatal(err)
	}
	if len(shape) != 4 || shape["results_root"] == nil || shape["scanned_at"] == nil || shape["batches"] == nil || shape["standalone_runs"] == nil {
		t.Fatalf("response shape changed: %s", body)
	}
}
