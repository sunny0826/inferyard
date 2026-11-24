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
	for _, family := range []string{"llama-cpp", "mlx-lm", "polystrata"} {
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

func TestStartCandidateRunsRejectsMismatchedBundleID(t *testing.T) {
	f := newLocalQueueFixture(t, "polystrata")
	mismatched := []CandidateRun{
		{CandidatePath: f.candidates[1].CandidatePath, Bundle: "zh-core", Family: "polystrata"},
	}
	if _, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: mismatched, AllowSingleBundle: true}); failure == nil || failure.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for mismatched bundle, got %#v", failure)
	}
}
