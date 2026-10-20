package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/evidence"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testService(t *testing.T, execute Executor) *Service {
	t.Helper()
	ctx, c := context.WithCancel(context.Background())
	t.Cleanup(c)
	s, e := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json"), Execute: execute, HostStatePath: filepath.Join(t.TempDir(), "host.json")})
	if e != nil {
		t.Fatal(e)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	return s
}
func version(args []string) (inferyard.Execution, bool) {
	if args[0] == "--version" {
		return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, true
	}
	return inferyard.Execution{}, false
}
func result(code int) inferyard.Execution {
	return inferyard.Execution{ExitCode: code, Stdout: []byte(`{"command":"run","status":"complete","completeness":"complete","limitations":[]}`)}
}
func config(t *testing.T, root, bundle string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	bundleRaw, _ := json.Marshal(map[string]string{"bundle_id": bundle})
	if e := os.WriteFile(filepath.Join(filepath.Dir(p), bundle+".json"), bundleRaw, 0600); e != nil {
		t.Fatal(e)
	}
	e := os.WriteFile(p, []byte(fmt.Sprintf("[output]\nroot = %q\n[bundle]\npath = %q\n[endpoint]\nserver_pid = 1\nprocess_start_ticks = 1\n", root, bundle+".json")), 0600)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func wait(t *testing.T, f func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}
func TestQueueCancellationAndExitMapping(t *testing.T) {
	started := make(chan struct{}, 1)
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		started <- struct{}{}
		<-ctx.Done()
		return result(130), ctx.Err()
	})
	root := t.TempDir()
	s.settings.ResultsRoot = &root
	tasks, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{config(t, root, "a"), config(t, root, "b")}})
	if e != nil {
		t.Fatal(e)
	}
	<-started
	queued, e := s.CancelRun(tasks[1].TaskId)
	if e != nil || queued.State != "cancelled" || queued.Exit != nil || queued.FinishedAt == nil {
		t.Fatalf("queued cancellation: %+v %v", queued, e)
	}
	_, e = s.CancelRun(tasks[0].TaskId)
	if e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { v, _ := s.RunDetail(tasks[0].TaskId); return v.Task.State == "cancelled" })
	v, _ := s.RunDetail(tasks[0].TaskId)
	if v.Task.Exit.ExitCode != 130 {
		t.Fatal(v)
	}
	if _, e = s.CancelRun(tasks[0].TaskId); e == nil || e.Details["reason"] != "invalid_state" {
		t.Fatal(e)
	}
	for _, code := range []int{0, 2, 3, 4, 130} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				if v, ok := version(args); ok {
					return v, nil
				}
				return result(code), nil
			})
			root := t.TempDir()
			s.settings.ResultsRoot = &root
			ts, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{config(t, root, "a")}, AllowSingleBundle: ptr(true)})
			if e != nil {
				t.Fatal(e)
			}
			wait(t, func() bool { v, _ := s.RunDetail(ts[0].TaskId); return terminal(v.Task.State) })
			v, _ := s.RunDetail(ts[0].TaskId)
			want := map[int]string{0: "done", 2: "failed", 3: "done", 4: "failed", 130: "cancelled"}[code]
			if v.Task.State != want {
				t.Fatal(v)
			}
			if code == 3 && str(v.Task.Exit.Completeness) != "incomplete" {
				t.Fatal(v)
			}
		})
	}
}
func TestSettingsAndDeviceRefresh(t *testing.T) {
	gate := make(chan struct{})
	var calls atomic.Int64
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		calls.Add(1)
		select {
		case <-ctx.Done():
			return result(130), ctx.Err()
		case <-gate:
		}
		return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"device-check","status":"blocked","completeness":"incomplete","details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{},"extra":true}}`)}, nil
	})
	root := t.TempDir()
	patch := map[string]json.RawMessage{"models_root": json.RawMessage(fmt.Sprintf("%q", root)), "results_root": json.RawMessage(fmt.Sprintf("%q", filepath.Join(root, "results")))}
	got, e := s.UpdateSettings(patch)
	if e != nil || str(got.Effective.ReportsRoot) != filepath.Join(root, "reports") {
		t.Fatal(got, e)
	}
	for _, bad := range []string{"null", `""`, `"relative"`} {
		if _, e = s.UpdateSettings(map[string]json.RawMessage{"results_root": json.RawMessage(bad)}); e == nil {
			t.Fatal(bad)
		}
	}
	if e = s.RefreshDevice(); e != nil {
		t.Fatal(e)
	}
	if e = s.RefreshDevice(); e == nil || e.Details["reason"] != "refresh_in_progress" {
		t.Fatal(e)
	}
	close(gate)
	wait(t, func() bool { return !s.Device().Refreshing })
	if s.Device().LastError != nil || s.Device().Snapshot == nil || calls.Load() != 1 {
		t.Fatal(s.Device())
	}
	raw, e1 := os.ReadFile(s.settingsPath)
	if e1 != nil || len(raw) == 0 {
		t.Fatal(e1)
	}
}
func TestEventReplayLoad(t *testing.T) {
	b := NewEventBus()
	_, unsub, seed := b.Subscribe()
	defer unsub()
	start := time.Now()
	for i := 0; i < 10000; i++ {
		if e := b.Publish("run.progress", map[string]int{"done": i}); e != nil {
			t.Fatal(e)
		}
	}
	elapsed := time.Since(start)
	if _, gap, _ := b.Since(seed); !gap {
		t.Fatal("missing reset")
	}
	events, gap, last := b.Since(seed + 10000 - 2048 + 1)
	if gap || len(events) != 2047 {
		t.Fatal(len(events), gap)
	}
	if _, gap, _ = b.Since(last + 1); !gap {
		t.Fatal("future cursor")
	}
	t.Logf("10000 fixture events in %s; retained=%d; replay=%d; heartbeat=%s; progress interval=%s", elapsed, len(b.ring), len(events), HeartbeatInterval, ProgressInterval)
}
func TestDirtyAndAggregation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "host.json")
	for _, tc := range []struct {
		raw   string
		dirty bool
	}{{`{"schema_version":1,"dirty":true,"run_id":"r","kind":"run"}`, true}, {`{"schema_version":1,"dirty":false,"run_id":"r","kind":"run"}`, false}, {`{"schema_version":1,"dirty":true,"run_id":"other","kind":"run"}`, false}, {`{"schema_version":1,"dirty":true,"run_id":"r","kind":"engine-fit"}`, false}} {
		os.WriteFile(p, []byte(tc.raw), 0600)
		v, e := evidence.DirtyRun(p, "r")
		if e != nil || v != tc.dirty {
			t.Fatal(v, e)
		}
	}
	a := newAggregate([]byte(`{"documents":{"selection":{"payload":{"case_ids":["c"]}}}}`))
	rows := []evidence.Record{{Seq: 1, Phase: "formal", EventType: "request_started", Raw: json.RawMessage(`{"utc":"start","request_id":"q","data":{"case_id":"c","plan_index":0}}`)}, {Seq: 2, Phase: "formal", EventType: "request_finished", Raw: json.RawMessage(`{"utc":"end","request_id":"q","data":{"execution_state":"completed","t_send_ns":1000000,"t_terminal_ns":4000000}}`)}}
	a.events(rows)
	if a.Progress.Done != 1 || *a.Progress.Total != 1 || *a.Progress.Cases[0].DurationMs != 3 {
		t.Fatal(a.Progress)
	}
}
func TestContainmentAndSourceValidation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, e := Resolve(root, "../escape"); e == nil {
		t.Fatal("escape")
	}
	if e := os.Symlink(outside, filepath.Join(root, "link")); e == nil {
		if _, e = Resolve(root, "link/new"); e == nil {
			t.Fatal("symlink escape")
		}
	}
	good := "https://huggingface.co/org/repo/blob/0123456789012345678901234567890123456789/m.gguf"
	if !validSource(good) {
		t.Fatal("valid source")
	}
	for _, v := range []string{"https://example.com/a.gguf", "https://huggingface.co/org/repo/blob/main/m.gguf", good + "?token=secret"} {
		if validSource(v) {
			t.Fatal(v)
		}
	}
}
func TestParallelDownloadsAndProgress(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan string, 2)
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		dir := args[len(args)-1]
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, "model.gguf.part"), []byte("1234"), 0600)
		started <- dir
		select {
		case <-ctx.Done():
			return result(130), ctx.Err()
		case <-gate:
		}
		return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"model","status":"complete","completeness":"complete","details":{"bytes":4,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)}, nil
	})
	root := t.TempDir()
	s.settings.ModelsRoot = &root
	source := "https://huggingface.co/o/r/blob/0123456789012345678901234567890123456789/model.gguf"
	a, e := s.CreateModelDownload(CreateDownloadRequest{source, "a"}, "")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateModelDownload(CreateDownloadRequest{source, "b"}, "")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("downloads serialized")
		}
	}
	wait(t, func() bool {
		tasks := s.Downloads()
		return tasks[0].DownloadedBytes == 4 && tasks[1].DownloadedBytes == 4
	})
	if _, e = s.CreateModelDownload(CreateDownloadRequest{source, "a"}, ""); e == nil || e.Details["reason"] != "output_exists" {
		t.Fatal(e)
	}
	s.CancelDownload(a.TaskId)
	wait(t, func() bool {
		for _, d := range s.Downloads() {
			if d.TaskId == a.TaskId {
				return d.State == "cancelled"
			}
		}
		return false
	})
	close(gate)
	wait(t, func() bool {
		for _, d := range s.Downloads() {
			if d.TaskId == b.TaskId {
				return d.State == "done" && d.Result.Bytes == 4
			}
		}
		return false
	})
}
func TestReportCreationAndResumeValidation(t *testing.T) {
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		dir := args[len(args)-1]
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, "report.html"), []byte("report"), 0600)
		raw, _ := json.Marshal(map[string]any{"runs": []any{map[string]any{"source": map[string]any{"run_id": "fixture-run", "path": args[2]}}}})
		os.WriteFile(filepath.Join(dir, "index.json"), raw, 0600)
		return result(0), nil
	})
	root := t.TempDir()
	run := filepath.Join(root, "run")
	os.MkdirAll(run, 0700)
	for _, name := range []string{"inputs.json", "events.jsonl"} {
		raw, e := os.ReadFile(filepath.Join("../evidence/testdata/run", name))
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(run, name), raw, 0600)
	}
	s.settings.ResultsRoot = &root
	if _, e := s.Resume(ResumeRunRequest{FromRun: run}); e == nil || e.Details["reason"] != "not_resumable" {
		t.Fatal(e)
	}
	if _, e := s.CreateReport(CreateReportRequest{[]string{run, run}}); e == nil || e.Code != "invalid_request" {
		t.Fatal(e)
	}
	r, e := s.CreateReport(CreateReportRequest{[]string{run}})
	if e != nil {
		t.Fatal(e)
	}
	if r.RunIds[0] != "fixture-run" || str(r.RunPaths[0]) != mustResolve(t, root, run) {
		t.Fatal(r)
	}
	r2, e := s.CreateReport(CreateReportRequest{[]string{run}})
	if e != nil || r2.ReportId == r.ReportId {
		t.Fatal(r2, e)
	}
	os.RemoveAll(run)
	_, rs, e := s.Reports()
	if e != nil || len(rs) != 2 || rs[0].RunPaths[0] != nil {
		t.Fatal(rs, e)
	}
}

func mustResolve(t *testing.T, root, path string) string {
	t.Helper()
	p, e := Resolve(root, path)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestResumePlanOrderAndParentChain(t *testing.T) {
	root := t.TempDir()
	makeRun := func(id, trial, parent string) evidence.Run {
		dir := filepath.Join(root, id)
		os.MkdirAll(dir, 0700)
		relation := "initial"
		if parent != "" {
			relation = "resume"
		}
		input := map[string]any{"documents": map[string]any{"run": map[string]any{"payload": map[string]any{"trial_id": trial, "relation": relation, "parent_run_id": parent}}, "selection": map[string]any{"payload": map[string]any{"case_ids": []string{"c"}}}, "plan": map[string]any{"payload": map[string]any{"trials": []any{map[string]any{"trial_id": "first"}, map[string]any{"trial_id": "second"}}}}}}
		raw, _ := json.Marshal(input)
		return evidence.Run{ID: id, Path: dir, Inputs: raw}
	}
	runs := []evidence.Run{makeRun("z-root", "first", ""), makeRun("a-child", "first", "z-root"), makeRun("latest-time", "second", "")}
	summaries := make([]RunSummary, 3)
	markResumable(summaries, runs)
	if summaries[0].Resumable || !summaries[1].Resumable || summaries[2].Resumable {
		t.Fatal(summaries)
	}
}
func TestTwoConfigsExecuteInArrayOrder(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		started <- args[2]
		select {
		case <-ctx.Done():
			return result(130), ctx.Err()
		case <-release:
		}
		return result(0), nil
	})
	root := t.TempDir()
	s.settings.ResultsRoot = &root
	a, b := config(t, root, "a"), config(t, root, "b")
	tasks, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{a, b}})
	if e != nil {
		t.Fatal(e)
	}
	canonicalA, _ := filepath.EvalSymlinks(a)
	if got := <-started; got != canonicalA {
		t.Fatal(got)
	}
	if _, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{a}, AllowSingleBundle: ptr(true)}); e == nil || e.Details["reason"] != "queue_duplicate" {
		t.Fatal(e)
	}
	detail, _ := s.RunDetail(tasks[1].TaskId)
	if !detail.Task.Queued {
		t.Fatal("second task started while first was active")
	}
	release <- struct{}{}
	canonicalB, _ := filepath.EvalSymlinks(b)
	select {
	case got := <-started:
		if got != canonicalB {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("second did not start")
	}
	close(release)
	wait(t, func() bool { d, _ := s.RunDetail(tasks[1].TaskId); return d.Task.State == "done" })
}
func TestUnclosedDurationRemainsCLIResumeDecision(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte("{\"seq\":1,\"event_type\":\"duration_started\",\"data\":{}}\n"), 0600)
	raw := []byte(`{"documents":{"run":{"payload":{"trial_id":"t","relation":"initial"}},"plan":{"payload":{"trials":[{"trial_id":"t"}]}},"selection":{"payload":{"case_ids":[]}}}}`)
	summaries := []RunSummary{{}}
	markResumable(summaries, []evidence.Run{{ID: "r", Path: dir, Inputs: raw}})
	if !summaries[0].Resumable {
		t.Fatal("unfinished duration must reach CLI for exit-3 decision")
	}
}
