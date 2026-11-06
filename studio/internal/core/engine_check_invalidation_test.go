package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

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
