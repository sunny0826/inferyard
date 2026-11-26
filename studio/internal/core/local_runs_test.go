package core

import (
	"context"
	"os"
	"path/filepath"
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

func TestLocalQueueReusesExistingManagedServiceAcrossBundles(t *testing.T) {
	for _, family := range []string{"llama-cpp", "mlx-lm", "polystrata"} {
		t.Run(family, func(t *testing.T) {
			f := newLocalQueueFixture(t, family)
			f.s.identityReader = func(pid int64) (int64, error) { return pid * 100, nil }
			existing := &EngineState{State: "running", Pid: 9101, Endpoint: f.endpoint}
			tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{
				Candidates:      f.candidates,
				ExistingService: existing,
			})
			if failure != nil {
				t.Fatal(failure)
			}
			for _, task := range f.done(t, tasks) {
				if task.State != "done" || task.Exit.ExitCode != 0 {
					t.Fatalf("unexpected task result: %+v", task)
				}
			}
			f.mu.Lock()
			pids := append([]int64{}, f.bindPIDs...)
			bundles := append([]string{}, f.runBundles...)
			f.mu.Unlock()
			if !reflect.DeepEqual(pids, []int64{9101, 9101}) || !reflect.DeepEqual(bundles, []string{"zh-core", "zh-svg-pelican"}) {
				t.Fatalf("expected reused pid 9101 across both bundles, got pids=%v bundles=%v", pids, bundles)
			}
			if f.s.engine != nil || f.s.Engine().State != "stopped" {
				t.Fatalf("expected no dedicated benchmark engine spawned when reusing standalone service, got %+v", f.s.Engine())
			}
		})
	}
}

func TestDescribeCandidateCachesUnchangedCandidateFile(t *testing.T) {
	f := newLocalQueueFixture(t, "polystrata")
	if err := os.WriteFile(filepath.Join(f.workspace, "model"), []byte("gguf"), 0600); err != nil {
		t.Fatal(err)
	}
	path := f.candidates[0].CandidatePath
	for range 3 {
		if _, failure := f.s.describeCandidateCached(context.Background(), path); failure != nil {
			t.Fatal(failure)
		}
	}
	f.mu.Lock()
	calls := f.describeCalls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 config describe subprocess for unchanged candidate, got %d", calls)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, failure := f.s.describeCandidateCached(context.Background(), path); failure != nil {
		t.Fatal(failure)
	}
	f.mu.Lock()
	callsAfter := f.describeCalls
	f.mu.Unlock()
	if callsAfter != 2 {
		t.Fatalf("expected cache invalidation after file change to trigger 2nd call, got %d", callsAfter)
	}
}
