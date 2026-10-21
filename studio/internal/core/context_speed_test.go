package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

type speedFixture struct {
	mu        sync.Mutex
	commands  [][]string
	result    []byte
	blockInit bool
	hold      chan struct{}
	started   chan struct{}
	reads     int
}

func speedGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, e := os.ReadFile("../inferyard/testdata/context_speed_contract.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Name     string          `json:"name"`
		Response json.RawMessage `json:"response"`
	}
	if e = json.Unmarshal(raw, &rows); e != nil {
		t.Fatal(e)
	}
	for _, r := range rows {
		if r.Name == name {
			return r.Response
		}
	}
	t.Fatal("unknown golden", name)
	return nil
}

func speedService(t *testing.T, f *speedFixture) *Service {
	t.Helper()
	t.Setenv("ENGINE_HELPER", "1")
	workspace, _ := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	s.settings.InferyardPath = ptr(os.Args[0])
	s.settings.ModelsRoot = ptr(t.TempDir())
	model := filepath.Join(*s.settings.ModelsRoot, "model.gguf")
	if e := os.WriteFile(model, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	s.device.Snapshot, _ = json.Marshal(map[string]any{"models": []any{map[string]any{"path": model, "status": "available", "size_bytes": 9, "architecture": "llama", "context_length": 262144}}})
	s.registryRoots = func(string) []string { return []string{} }
	engine, _ := os.Executable()
	engineDir := filepath.Join(workspace, "engines")
	writeEngineBinary(t, engineDir, 0755, time.Now())
	s.registryRoots = func(string) []string { return []string{engineDir} }
	port := freePort(t)
	s.exec = func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if out, ok := engineFixtureDescription(args); ok {
			return out, nil
		}
		f.mu.Lock()
		f.commands = append(f.commands, append([]string{}, args...))
		f.mu.Unlock()
		flag := func(key string) string {
			for i, a := range args {
				if a == key && i+1 < len(args) {
					return args[i+1]
				}
			}
			return ""
		}
		ok := inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"fixture","status":"finished","completeness":"complete"}`)}
		switch args[0] {
		case "--schema":
			ok.Stdout, _ = os.ReadFile("../../../cli/schemas/context_speed_result.schema.json")
		case "init":
			if f.blockInit {
				return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"init","status":"blocked","completeness":"incomplete","limitations":["bundle_review_required"]}`)}, nil
			}
			if e := os.MkdirAll(flag("--out"), 0700); e != nil {
				return ok, e
			}
		case "device-check":
		case "config":
			out := flag("--out")
			if e := os.MkdirAll(out, 0700); e != nil {
				return ok, e
			}
			if args[1] == "create" {
				body := fmt.Sprintf("[engine]\nbinary_path = %q\nstartup_args = [\"--host\", \"127.0.0.1\", \"--port\", \"%d\"]\n", engine, port)
				if e := os.WriteFile(filepath.Join(out, "candidate.toml"), []byte(body), 0600); e != nil {
					return ok, e
				}
			}
		case "plan":
			dir := flag("--out")
			if e := os.MkdirAll(dir, 0700); e != nil {
				return ok, e
			}
			trials := []map[string]any{}
			for i := range 5 {
				trials = append(trials, map[string]any{"trial_id": fmt.Sprintf("trial-%d", i), "repeat_index": i})
			}
			raw, _ := json.Marshal(map[string]any{"schema_version": 3, "plan_sha256": strings.Repeat("a", 64), "experiment": map[string]any{"profile": "context-speed.v1", "budget": map[string]any{"max_requests": 5, "max_wall_seconds": 3725}}, "trials": trials})
			if e := os.WriteFile(filepath.Join(dir, "plan.json"), raw, 0600); e != nil {
				return ok, e
			}
		case "run":
			if len(args) != 5 || args[1] != "--plan" || args[3] != "--output-root" {
				return ok, fmt.Errorf("wrong argv: %v", args)
			}
			root := flag("--output-root")
			if e := os.MkdirAll(root, 0700); e != nil {
				return ok, e
			}
			if e := os.WriteFile(filepath.Join(root, "batch.json"), []byte(`{}`), 0600); e != nil {
				return ok, e
			}
			if f.started != nil {
				close(f.started)
			}
			if f.hold != nil {
				select {
				case <-ctx.Done():
					return inferyard.Execution{ExitCode: 130}, ctx.Err()
				case <-f.hold:
				}
			}
		case "speed-results":
			if ctx.Err() != nil {
				return inferyard.Execution{}, ctx.Err()
			}
			f.mu.Lock()
			f.reads++
			raw := append([]byte{}, f.result...)
			f.mu.Unlock()
			var response map[string]any
			if json.Unmarshal(raw, &response) != nil {
				return inferyard.Execution{ExitCode: 4, Stdout: raw}, nil
			}
			response["evidence_dir"] = flag("--batch")
			ok.Stdout, _ = json.Marshal(response)
			if response["status"] == "partial" {
				ok.ExitCode = 3
			}
		default:
			return ok, fmt.Errorf("unexpected command %v", args)
		}
		return ok, nil
	}
	return s
}

func startSpeedFixture(t *testing.T, s *Service) RunTask {
	t.Helper()
	registry := s.Registry()
	if len(registry.Models) == 0 || len(registry.Engines) == 0 {
		t.Fatalf("registry %+v", registry)
	}
	task, e := s.StartContextSpeed(StartContextSpeedRequest{Model: registry.Models[0].Name, Engine: registry.Engines[0].Name, Context: 65536})
	if e != nil {
		t.Fatal(e)
	}
	return task
}

func TestContextSpeedFiveAndHistoryReadOnly(t *testing.T) {
	f := &speedFixture{result: speedGolden(t, "five_complete")}
	s := speedService(t, f)
	task := startSpeedFixture(t, s)
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	d, _ := s.RunDetail(task.TaskId)
	if d.Task.State != "done" || d.Task.Error != nil || d.Task.RunPath != nil || d.Task.RunId != nil || len(d.Task.ContextSpeed.Batch.Runs) != 5 {
		t.Fatalf("%+v %+v", d.Task, d.Task.Error)
	}
	for range 2 {
		r, e := s.ReadContextSpeed(t.Context(), task.TaskId)
		if e != nil || len(r.Samples) != 5 || r.Summary == nil {
			t.Fatalf("%+v %v", r, e)
		}
	}
	f.mu.Lock()
	commands := append([][]string{}, f.commands...)
	reads := f.reads
	f.mu.Unlock()
	runs := 0
	for _, a := range commands {
		if a[0] == "run" {
			runs++
		}
		if a[0] == "report" || a[0] == "export" {
			t.Fatal(a)
		}
	}
	if runs != 1 || reads != 3 {
		t.Fatalf("runs %d reads %d", runs, reads)
	}
	record := filepath.Join(*s.settings.WorkspaceRoot, "logs", "context-speed-tasks", task.TaskId+".json")
	raw, e := os.ReadFile(record)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{`"samples"`, `"summary"`, `"prefill"`, `"decode"`} {
		if strings.Contains(string(raw), field) {
			t.Fatal("cached result", field)
		}
	}
	reopened := engineService(t, *s.settings.WorkspaceRoot)
	rows, apiErr := reopened.ContextSpeedHistory()
	if apiErr != nil || len(rows) != 1 || rows[0].TaskId != task.TaskId {
		t.Fatalf("want=%s rows=%+v error=%v", task.TaskId, rows, apiErr)
	}
	if e := os.RemoveAll(d.Task.ContextSpeed.Batch.Path); e != nil {
		t.Fatal(e)
	}
	rows, apiErr = reopened.ContextSpeedHistory()
	if apiErr != nil || len(rows) != 1 {
		t.Fatal("missing batch disappeared", apiErr)
	}
}

func TestContextSpeedCancelledFreshRead(t *testing.T) {
	f := &speedFixture{result: speedGolden(t, "five_complete"), hold: make(chan struct{}), started: make(chan struct{})}
	s := speedService(t, f)
	task := startSpeedFixture(t, s)
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not start")
	}
	if _, e := s.CancelRun(task.TaskId); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	d, _ := s.RunDetail(task.TaskId)
	if d.Task.State != "cancelled" || d.Task.Exit.ExitCode != 130 {
		t.Fatalf("%+v", d.Task)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads != 1 {
		t.Fatal("cancelled execution context leaked into read", f.reads)
	}
}

func TestContextSpeedPreparationFailurePersists(t *testing.T) {
	f := &speedFixture{blockInit: true}
	s := speedService(t, f)
	task := startSpeedFixture(t, s)
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	rows, e := s.ContextSpeedHistory()
	if e != nil || len(rows) != 1 || rows[0].State != "failed" || rows[0].ContextSpeed.Batch != nil || rows[0].Error == nil || len(rows[0].Error.Limitations) != 1 {
		t.Fatalf("want=%s rows=%+v error=%v", task.TaskId, rows, e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 2 || f.commands[1][0] != "init" {
		t.Fatal("continued after rejection", f.commands)
	}
}

func TestContextSpeedZeroAndReadFailureNeverReturnOldRows(t *testing.T) {
	f := &speedFixture{result: speedGolden(t, "zero_runs")}
	s := speedService(t, f)
	task := startSpeedFixture(t, s)
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	r, e := s.ReadContextSpeed(t.Context(), task.TaskId)
	if e != nil || len(r.Samples) != 5 || r.Execution.Counts.NotExecuted != 5 || r.Summary != nil {
		t.Fatalf("zero batch lost: %+v %v", r, e)
	}
	f.mu.Lock()
	f.result = []byte(`{"invalid":`)
	f.mu.Unlock()
	r, e = s.ReadContextSpeed(t.Context(), task.TaskId)
	if e == nil || r.Samples != nil || r.Summary != nil {
		t.Fatalf("stale results: %+v %v", r, e)
	}
	f.mu.Lock()
	f.result = speedGolden(t, "zero_runs")
	f.mu.Unlock()
	if _, e = s.ReadContextSpeed(t.Context(), task.TaskId); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	runs := 0
	for _, c := range f.commands {
		if c[0] == "run" {
			runs++
		}
	}
	if runs != 1 {
		t.Fatal("read retry reran measurements", runs)
	}
}
