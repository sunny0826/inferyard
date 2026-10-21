package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContextSpeedOldCapabilityStopsBeforeInit(t *testing.T) {
	f := &speedFixture{}
	s := speedService(t, f)
	exec := s.exec
	s.exec = func(ctx context.Context, binary string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--schema" {
			return inferyard.Execution{Stdout: []byte(`{"oneOf":[{},{}]}`)}, nil
		}
		return exec(ctx, binary, args...)
	}
	task := startSpeedFixture(t, s)
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	d, _ := s.RunDetail(task.TaskId)
	if d.Task.Error == nil || d.Task.Error.Code != "cli_outdated" {
		t.Fatalf("%+v", d.Task)
	}
	if len(f.commands) != 0 {
		t.Fatal("old CLI received preparation", f.commands)
	}
}

func TestContextSpeedUnreadableExternalBatchRetained(t *testing.T) {
	s := speedService(t, &speedFixture{})
	batch := filepath.Join(*s.settings.ResultsRoot, "external-batch")
	if err := os.MkdirAll(batch, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(batch, "batch.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	rows, e := s.ContextSpeedHistory()
	if e != nil || len(rows) != 1 || filepath.Base(rows[0].ContextSpeed.Batch.Path) != filepath.Base(batch) || rows[0].Error == nil {
		t.Fatalf("unreadable batch lost: %+v %v", rows, e)
	}
}

func TestContextSpeedWaitingCancellationDoesNotExecute(t *testing.T) {
	s := speedService(t, &speedFixture{})
	root := filepath.Join(*s.settings.WorkspaceRoot, "logs", "context-speed-tasks")
	root, _ = canonical(root)
	task := &runEntry{Task: RunTask{TaskId: "waiting", Kind: "context_speed", State: "starting", Queued: true, ContextSpeed: &ContextSpeedTask{Stage: "queued"}}, recordRoot: root}
	s.mu.Lock()
	s.runs = append(s.runs, task)
	s.mu.Unlock()
	got, e := s.CancelRun("waiting")
	if e != nil || got.State != "cancelled" || got.Queued || got.Exit != nil {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestContextSpeedShutdownDrainsBeforeEngineStop(t *testing.T) {
	f := &speedFixture{result: speedGolden(t, "zero_runs"), hold: make(chan struct{}), started: make(chan struct{})}
	s := speedService(t, f)
	task := startSpeedFixture(t, s)
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		d, _ := s.RunDetail(task.TaskId)
		t.Fatalf("measurement did not start: %+v", d.Task)
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	d, _ := s.RunDetail(task.TaskId)
	if d.Task.State != "cancelled" || d.Task.Exit == nil || d.Task.Exit.ExitCode != 130 || s.Engine().State != "stopped" {
		t.Fatalf("%+v %+v", d.Task, s.Engine())
	}
	if f.reads != 1 {
		t.Fatal("shutdown lost final offline read", f.reads)
	}
	if _, e := s.StartContextSpeed(StartContextSpeedRequest{Model: "m", Engine: "e", Context: 65536}); e == nil {
		t.Fatal("shutdown admitted work")
	}
}

func TestContextSpeedReadRejectsEscapedBatch(t *testing.T) {
	s := speedService(t, &speedFixture{})
	outside := t.TempDir()
	link := filepath.Join(*s.settings.ResultsRoot, "escape")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	r, e := s.readContextSpeed(t.Context(), "unused", &ContextSpeedTask{Batch: &SpeedBatchRef{Path: link}})
	if e == nil || r.Samples != nil || r.Summary != nil {
		t.Fatal("escaped batch accepted")
	}
}

func TestContextSpeedBrokenPlanDoesNotHideBatch(t *testing.T) {
	s := speedService(t, &speedFixture{})
	batch := filepath.Join(*s.settings.ResultsRoot, "broken")
	if err := os.MkdirAll(filepath.Join(batch, "frozen-plan"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"batch.json", "frozen-plan/plan.json"} {
		if err := os.WriteFile(filepath.Join(batch, path), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rows, e := s.ContextSpeedHistory()
	if e != nil || len(rows) != 1 || rows[0].Error == nil || rows[0].State != "failed" {
		t.Fatalf("broken plan lost: %+v %v", rows, e)
	}
}

func TestContextSpeedExternalFrozenIdentity(t *testing.T) {
	f := &speedFixture{result: speedGolden(t, "five_complete")}
	s := speedService(t, f)
	original := startSpeedFixture(t, s)
	wait(t, func() bool { d, _ := s.RunDetail(original.TaskId); return terminal(d.Task.State) })
	d, _ := s.RunDetail(original.TaskId)
	model, e := s.resourcePath(d.Task.ContextSpeed.Model, false)
	if e != nil {
		t.Fatal(e)
	}
	engine, e := s.resourcePath(d.Task.ContextSpeed.Engine, true)
	if e != nil {
		t.Fatal(e)
	}
	batch := filepath.Join(*s.settings.ResultsRoot, "external")
	if err := os.MkdirAll(filepath.Join(batch, "frozen-plan"), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(d.Task.ContextSpeed.Plan.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(batch, "frozen-plan", "plan.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"model": map[string]any{"display_name": "frozen model", "local_path": model}, "engine": map[string]any{"release": "frozen engine", "binary_path": engine}, "conditions": map[string]any{"context_size": 131072}}
	raw, _ = json.Marshal(config)
	if err = os.WriteFile(filepath.Join(batch, "frozen-plan", "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(batch, "batch.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	rows, e := s.ContextSpeedHistory()
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if filepath.Base(row.ContextSpeed.Batch.Path) == "external" {
			got := row.ContextSpeed
			if got.DisplayModel != "frozen model" || got.DisplayEngine != "frozen engine" || got.Model != d.Task.ContextSpeed.Model || got.Engine != d.Task.ContextSpeed.Engine || got.Context != 131072 {
				t.Fatalf("lost frozen identity %+v", got)
			}
			return
		}
	}
	t.Fatal("external batch missing")
}

func TestContextSpeedZeroBatchExcludedFromOrdinaryHistory(t *testing.T) {
	s := speedService(t, &speedFixture{})
	batch := filepath.Join(*s.settings.ResultsRoot, "zero-speed")
	if err := os.MkdirAll(filepath.Join(batch, "frozen-plan"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string]string{"batch.json": `{}`, "frozen-plan/plan.json": `{"experiment":{"profile":"context-speed.v1"}}`} {
		if err := os.WriteFile(filepath.Join(batch, path), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	history, e := s.scan(s.userSettings())
	if e != nil || len(history.Batches) != 0 || len(history.StandaloneRuns) != 0 {
		t.Fatalf("specialist leaked into ordinary history %+v %v", history, e)
	}
}

func TestContextSpeedCancelCapabilityPreservesReason(t *testing.T) {
	s := speedService(t, &speedFixture{})
	exec := s.exec
	started := make(chan struct{})
	s.exec = func(ctx context.Context, binary string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--schema" {
			close(started)
			<-ctx.Done()
			return inferyard.Execution{ExitCode: -1}, ctx.Err()
		}
		return exec(ctx, binary, args...)
	}
	task := startSpeedFixture(t, s)
	<-started
	if _, e := s.CancelRun(task.TaskId); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { d, _ := s.RunDetail(task.TaskId); return terminal(d.Task.State) })
	d, _ := s.RunDetail(task.TaskId)
	if d.Task.State != "cancelled" || d.Task.Error == nil || d.Task.Error.Code != "cancelled" {
		t.Fatalf("cancellation presented as capability error: %+v", d.Task.Error)
	}
}
