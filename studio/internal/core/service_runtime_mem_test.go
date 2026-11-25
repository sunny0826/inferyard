package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestSampleProcessTreeRSSGB(t *testing.T) {
	if got := sampleProcessTreeRSSGB(nil); got != nil {
		t.Fatalf("expected nil for empty pids, got %v", *got)
	}
	if got := sampleProcessTreeRSSGB([]int{0, -5}); got != nil {
		t.Fatalf("expected nil for non-positive pids, got %v", *got)
	}
	if got := sampleProcessTreeRSSGB([]int{99999999}); got != nil {
		t.Fatalf("expected nil for nonexistent pid, got %v", *got)
	}
	if runtime.GOOS != "windows" {
		got := sampleProcessTreeRSSGB([]int{os.Getpid()})
		if got == nil || *got <= 0 {
			t.Fatalf("expected positive non-nil RSS GB for current process on %s, got %v", runtime.GOOS, got)
		}
	}
}

func TestParsePSRSSOutputGB(t *testing.T) {
	if got := parsePSRSSOutputGB([]byte("")); got != nil {
		t.Fatalf("expected nil for empty output, got %v", *got)
	}
	if got := parsePSRSSOutputGB([]byte("   \n  0 \n")); got != nil {
		t.Fatalf("expected nil for zero output, got %v", *got)
	}
	// 2 * 1024 * 1024 KB = 2.00 GB, plus 524288 KB = 0.50 GB -> 2.50 GB
	got := parsePSRSSOutputGB([]byte("  2097152 \n 524288\n"))
	if got == nil || *got != 2.5 {
		t.Fatalf("expected 2.5 GB, got %v", got)
	}
}

func TestServiceMemoryUsageHelper(t *testing.T) {
	measured := 12.34
	gb, isMeasured, known := ServiceMemoryUsage(EngineState{
		RuntimeMemoryGB:   &measured,
		EstimatedMemoryGB: 15.0,
	})
	if !known || !isMeasured || gb != 12.34 {
		t.Fatalf("expected live measured 12.34, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}

	gb, isMeasured, known = ServiceMemoryUsage(EngineState{
		EstimatedMemoryGB: 15.25,
	})
	if !known || isMeasured || gb != 15.25 {
		t.Fatalf("expected static estimate 15.25, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}

	gb, isMeasured, known = ServiceMemoryUsage(EngineState{})
	if known || isMeasured || gb != 0 {
		t.Fatalf("expected unknown memory when both absent, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}
}

func TestEntryPIDSnapshotConcurrentRaceSafetyAndLightweightEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	entry := &engineEntry{
		owner: engineOwner{
			PID:     1001,
			Workers: []engineProcessIdentity{{PID: 1002, StartTicks: 10}},
		},
		pidSnapshot: []int{1001, 1002},
		stop:        make(chan struct{}),
		detach:      make(chan struct{}),
		done:        make(chan struct{}),
		state: EngineState{
			ServiceID: "svc-race",
			State:     "running",
			Pid:       1001,
			Port:      8080,
		},
	}
	close(entry.done)
	svc.mu.Lock()
	svc.services["svc-race"] = entry
	svc.mu.Unlock()
	defer func() {
		svc.mu.Lock()
		delete(svc.services, "svc-race")
		svc.mu.Unlock()
	}()

	origSample := sampleProcessTreeRSSFunc
	sampleProcessTreeRSSFunc = func(pids []int) *float64 {
		v := 14.5
		return &v
	}
	defer func() { sampleProcessTreeRSSFunc = origSample }()

	_, off, cursor := svc.Bus.Subscribe()
	defer off()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			entry.owner.Workers = []engineProcessIdentity{{PID: int64(2000 + i), StartTicks: 10}}
			svc.syncEntryPIDSnapshot(entry)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			svc.mu.Lock()
			pids := entryProcessPIDsLocked(entry)
			svc.mu.Unlock()
			_ = len(pids)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			svc.maybeSampleRuntimeMemory(entry, nil, true)
		}
	}()
	wg.Wait()

	events, _, _ := svc.Bus.After(cursor)
	sawMemory := false
	for _, ev := range events {
		if ev.Type == "services.memory" {
			sawMemory = true
		}
		if ev.Type == "engine.state" || ev.Type == "services.state" {
			t.Fatalf("RSS update must not emit heavy %q event", ev.Type)
		}
	}
	if !sawMemory {
		t.Fatalf("expected lightweight services.memory event after RSS change, got %+v", events)
	}
}
