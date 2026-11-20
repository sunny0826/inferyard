package core

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestLocalQueueStartsFreshProcessPerBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	for _, family := range []string{"llama-cpp", "mlx-lm"} {
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

func TestProbeReusesVersionForSameExecutable(t *testing.T) {
	var calls int
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			calls++
			return v, nil
		}
		return inferyard.Execution{}, fmt.Errorf("unexpected fixture command %v", args)
	})
	s.settings.InferyardPath = ptr(os.Args[0])
	u := s.userSettings()
	for i := 0; i < 3; i++ {
		p := s.probe(u)
		if p.Status != "ok" || str(p.Path) != os.Args[0] || str(p.Version) != "1.0" {
			t.Fatal(p)
		}
	}
	if calls != 1 {
		t.Fatalf("version probe ran %d times, want 1", calls)
	}
	p := s.probe(u)
	*p.Version = "tampered"
	if again := s.probe(u); str(again.Version) != "1.0" {
		t.Fatalf("cache aliased caller pointer: %+v", again)
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
