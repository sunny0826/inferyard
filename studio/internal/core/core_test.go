package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"

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
	ws := t.TempDir()
	root := filepath.Join(ws, "results")
	s.settings.WorkspaceRoot = &ws
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
	select {
	case <-started:
		t.Fatal("cancelled queued run executed")
	default:
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
			ws := t.TempDir()
			root := filepath.Join(ws, "results")
			s.settings.WorkspaceRoot = &ws
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
	ws := t.TempDir()
	s.settings.WorkspaceRoot = &ws
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
		writeStudioReport(t, dir, "fixture-run", args[2])
		return result(0), nil
	})
	ws := t.TempDir()
	root := filepath.Join(ws, "results")
	run := filepath.Join(root, "run")
	os.MkdirAll(run, 0700)
	for _, name := range []string{"inputs.json", "events.jsonl"} {
		raw, e := os.ReadFile(filepath.Join("../evidence/testdata/run", name))
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(run, name), raw, 0600)
	}
	s.settings.WorkspaceRoot = &ws
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

func writeStudioReport(t *testing.T, dir, runID, runPath string) {
	t.Helper()
	raw, e := os.ReadFile("../inferyard/testdata/studio-report.json")
	if e != nil {
		t.Fatal(e)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	if e := decoder.Decode(&doc); e != nil {
		t.Fatal(e)
	}
	run := doc["runs"].([]any)[0].(map[string]any)
	source := run["source"].(map[string]any)
	source["run_id"], source["path"] = runID, runPath
	data, e := json.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "studio.json"), data, 0600); e != nil {
		t.Fatal(e)
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
	ws := t.TempDir()
	root := filepath.Join(ws, "results")
	s.settings.WorkspaceRoot = &ws
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
