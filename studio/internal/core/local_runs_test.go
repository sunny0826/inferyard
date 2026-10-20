package core

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"testing"
)

func TestLocalQueueStartsFreshProcessPerBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	for _, family := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang"} {
		t.Run(family, func(t *testing.T) {
			f := newLocalQueueFixture(t, family)
			tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates})
			if failure != nil {
				t.Fatal(failure)
			}
			for _, task := range f.done(t, tasks) {
				if task.State != "done" || task.Exit.ExitCode != 0 {
					t.Fatalf("%+v", task)
				}
			}
			f.mu.Lock()
			pids := append([]int64{}, f.bindPIDs...)
			bundles := append([]string{}, f.runBundles...)
			f.mu.Unlock()
			if len(pids) != 2 || pids[0] == pids[1] || !reflect.DeepEqual(bundles, []string{"zh-core", "zh-svg-pelican"}) {
				t.Fatal(pids, bundles)
			}
			if _, err := processStartTicks(pids[0]); !os.IsNotExist(err) {
				t.Fatal("first service survived handoff", err)
			}
			if f.s.Engine().Pid != pids[1] {
				t.Fatal(f.s.Engine())
			}
		})
	}
}

func TestLocalQueueFailureBlocksRemainingAndCleansService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	f := newLocalQueueFixture(t, "mlx-lm")
	f.failBundle = "zh-core"
	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates})
	if failure != nil {
		t.Fatal(failure)
	}
	got := f.done(t, tasks)
	if got[0].Error == nil || got[0].Error.Code != "cli_blocked" || got[1].Error == nil || got[1].Error.Code != "previous_scope_incomplete" {
		t.Fatalf("%+v", got)
	}
	f.mu.Lock()
	runs, binds := len(f.runBundles), len(f.bindPIDs)
	f.mu.Unlock()
	if runs != 0 || binds != 1 || f.s.Engine().State != "stopped" {
		t.Fatal(runs, binds, f.s.Engine())
	}
}

func TestLocalQueueCancelDrainsBeforeServiceStops(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	f := newLocalQueueFixture(t, "mlx-lm")
	f.blockRun = true
	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates})
	if failure != nil {
		t.Fatal(failure)
	}
	<-f.runStarted
	if _, failure := f.s.CancelRun(tasks[0].TaskId); failure != nil {
		t.Fatal(failure)
	}
	got := f.done(t, tasks)
	if got[0].State != "cancelled" || got[1].Error == nil || got[1].Error.Code != "previous_scope_incomplete" {
		t.Fatalf("%+v", got)
	}
	wait(t, func() bool { return f.s.Engine().State == "stopped" })
}

func TestLocalQueueLMStudioRequiresFreshExternalIdentity(t *testing.T) {
	f := newLocalQueueFixture(t, "lmstudio")
	initial := &EngineState{Pid: 4001, Endpoint: f.endpoint}
	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates, ExistingService: initial})
	if failure != nil {
		t.Fatal(failure)
	}
	wait(t, func() bool { v, _ := f.s.RunDetail(tasks[1].TaskId); return v.Task.AwaitingService })
	if failure := f.s.BindCandidateService(tasks[1].TaskId, 4001, f.endpoint); failure == nil || failure.Code != "local_engine_service_reused" {
		t.Fatal(failure)
	}
	v, _ := f.s.RunDetail(tasks[1].TaskId)
	if !v.Task.AwaitingService {
		t.Fatal("invalid binding consumed waiting state")
	}
	if failure := f.s.BindCandidateService(tasks[1].TaskId, 4002, f.endpoint); failure != nil {
		t.Fatal(failure)
	}
	for _, task := range f.done(t, tasks) {
		if task.State != "done" {
			t.Fatalf("%+v", task)
		}
	}
	f.mu.Lock()
	pids := append([]int64{}, f.bindPIDs...)
	f.mu.Unlock()
	if !reflect.DeepEqual(pids, []int64{4001, 4002}) {
		t.Fatal(pids)
	}
	if f.s.engine != nil || f.s.orphan != nil {
		t.Fatal("external service became owned")
	}
	if failure := f.s.stopEngineForHandoff(context.Background()); failure != nil {
		t.Fatal(failure)
	}
}

func TestLocalQueueCancelWhileAwaitingDoesNotBind(t *testing.T) {
	f := newLocalQueueFixture(t, "lmstudio")
	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates})
	if failure != nil {
		t.Fatal(failure)
	}
	wait(t, func() bool { v, _ := f.s.RunDetail(tasks[0].TaskId); return v.Task.AwaitingService })
	if _, failure := f.s.CancelRun(tasks[0].TaskId); failure != nil {
		t.Fatal(failure)
	}
	got := f.done(t, tasks)
	if got[0].State != "cancelled" || got[1].Error == nil || got[1].Error.Code != "previous_scope_incomplete" {
		t.Fatalf("%+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bindPIDs) != 0 {
		t.Fatal(f.bindPIDs)
	}
}

func TestLocalQueueRejectsLMIdentityChangeDuringBinding(t *testing.T) {
	f := newLocalQueueFixture(t, "lmstudio")
	f.s.identityReader = func(pid int64) (int64, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return pid*10 + int64(len(f.bindPIDs)), nil
	}
	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates, ExistingService: &EngineState{Pid: 4001, Endpoint: f.endpoint}})
	if failure != nil {
		t.Fatal(failure)
	}
	wait(t, func() bool {
		first, _ := f.s.RunDetail(tasks[0].TaskId)
		return terminal(first.Task.State)
	})
	first, _ := f.s.RunDetail(tasks[0].TaskId)
	if first.Task.Error == nil || first.Task.Error.Code != "candidate_binding_invalid" {
		t.Fatalf("changed process identity was accepted: %+v", first.Task)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runBundles) != 0 {
		t.Fatal("run was sent to replaced process", f.runBundles)
	}
}
