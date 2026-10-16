package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func TestVerifyEnginesLinkRemovalAndReceiptUpdate(t *testing.T) {
	var checks atomic.Int64
	s, path := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		checks.Add(1)
		return inferyard.Execution{Stdout: []byte(ineligibleFixture("invalid_library_manifest"))}, nil
	})
	ext := ".dylib"
	if runtime.GOOS == "linux" {
		ext = ".so"
	}
	if runtime.GOOS == "windows" {
		ext = ".dll"
	}
	link := filepath.Join(filepath.Dir(path), "broken"+ext)
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(filepath.Dir(path), "runtime-receipt.json")
	if err := os.WriteFile(receipt, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	verify := func(want int64) {
		t.Helper()
		s.VerifyEngines()
		wait(t, func() bool { return s.EngineChecks().Checking == 0 && checks.Load() == want })
	}
	verify(1)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	verify(2)
	if err := os.WriteFile(receipt, []byte(`{"updated":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	verify(3)
	s.VerifyEngines()
	if checks.Load() != 3 {
		t.Fatal("unchanged inputs rechecked")
	}
}

func TestVerifyEnginesDiscardsOldCLIAfterNewResult(t *testing.T) {
	oldStarted, oldRelease, oldFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cliVersion atomic.Int64
	cliVersion.Store(1)
	s, _ := checkableRegistry(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		v := cliVersion.Load()
		if args[0] == "--version" {
			if v == 1 {
				return inferyard.Execution{Stdout: []byte("inferyard old source=release")}, nil
			}
			return inferyard.Execution{Stdout: []byte("inferyard new source=release")}, nil
		}
		if v == 1 {
			close(oldStarted)
			<-oldRelease
			defer close(oldFinished)
			return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"command":"unknown"}`)}, nil
		}
		return inferyard.Execution{Stdout: []byte(engineCheckFixture)}, nil
	})
	s.VerifyEngines()
	select {
	case <-oldStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("old check did not start")
	}
	cliVersion.Store(2)
	s.VerifyEngines()
	wait(t, func() bool { return s.EngineChecks().Checking == 0 })
	close(oldRelease)
	<-oldFinished
	// Wait for both background jobs to leave the semaphore, including writeback.
	wait(t, func() bool { return len(s.checkSem) == 0 })
	overview := s.EngineChecks()
	if !overview.Supported {
		t.Fatal("old CLI disabled new CLI")
	}
	for _, e := range overview.Engines {
		if e.State != EngineCheckEligible {
			t.Fatalf("%+v", overview)
		}
	}
	s.VerifyEngines()
	if !s.EngineChecks().Supported {
		t.Fatal("state remained poisoned")
	}
}
