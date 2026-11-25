package core

import (
	"context"
	"testing"
	"time"
)

func TestStopManagedServiceAndWaitEventDrivenAndIdempotent(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
		"model-b.gguf": 4,
	}, nil)

	stA, errA := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
	})
	if errA != nil {
		t.Fatalf("start model-a failed: %+v", errA)
	}
	waitManagedServiceState(t, s, stA.ServiceID, "running")

	stB, errB := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 4096,
	})
	if errB != nil {
		t.Fatalf("start model-b failed: %+v", errB)
	}
	waitManagedServiceState(t, s, stB.ServiceID, "running")

	// Stop A and wait via entry.done: A stops while B keeps running.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if apiErr := s.StopManagedServiceAndWait(ctx, stA.ServiceID); apiErr != nil {
		t.Fatalf("StopManagedServiceAndWait(A) failed: %+v", apiErr)
	}
	if curA, ok := s.ManagedService(stA.ServiceID); !ok || curA.State != "stopped" {
		t.Fatalf("expected A stopped, got ok=%v state=%+v", ok, curA)
	}
	if curB, ok := s.ManagedService(stB.ServiceID); !ok || curB.State != "running" {
		t.Fatalf("expected B still running, got ok=%v state=%+v", ok, curB)
	}

	// Second call on already stopped or unknown serviceID is idempotent (returns nil),
	// even when a benchmark run is active.
	s.mu.Lock()
	s.runs = append(s.runs, &runEntry{Task: RunTask{State: "running"}})
	s.mu.Unlock()
	if apiErr := s.StopManagedServiceAndWait(ctx, stA.ServiceID); apiErr != nil {
		t.Fatalf("expected nil on already-stopped serviceID during active run, got %+v", apiErr)
	}
	if apiErr := s.StopManagedServiceAndWait(ctx, "unknown-service-id"); apiErr != nil {
		t.Fatalf("expected nil on unknown serviceID during active run, got %+v", apiErr)
	}
	s.mu.Lock()
	s.runs = nil
	s.mu.Unlock()

	// Pre-cancelled context must NOT stop a running service.
	cancelledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if apiErr := s.StopManagedServiceAndWait(cancelledCtx, stB.ServiceID); apiErr == nil || apiErr.Code != "cancelled" {
		t.Fatalf("expected cancelled for pre-cancelled ctx, got %+v", apiErr)
	}
	if curB, ok := s.ManagedService(stB.ServiceID); !ok || curB.State != "running" {
		t.Fatalf("expected B still running after pre-cancelled stop, got ok=%v state=%+v", ok, curB)
	}

	// StopActiveServicesAndWait stops remaining B without polling.
	if apiErr := s.StopActiveServicesAndWait(ctx); apiErr != nil {
		t.Fatalf("StopActiveServicesAndWait failed: %+v", apiErr)
	}
	if active := s.ManagedServices(); len(active) != 0 {
		t.Fatalf("expected 0 active services after StopActiveServicesAndWait, got %+v", active)
	}
}

func TestStopWaitEdgeCasesOrphanIsolationStoppedPrimaryAndDetachedLoop(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
	}, nil)

	stA, errA := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
	})
	if errA != nil {
		t.Fatalf("start model-a failed: %+v", errA)
	}
	waitManagedServiceState(t, s, stA.ServiceID, "running")

	// 1. Unrelated global s.orphan must not break idempotency of StopManagedServiceAndWait for unknown ID.
	s.mu.Lock()
	s.orphan = &orphanEngine{state: EngineState{State: "orphaned"}}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if apiErr := s.StopManagedServiceAndWait(ctx, "unknown-id"); apiErr != nil {
		t.Fatalf("expected nil for unknown serviceID despite global s.orphan, got %+v", apiErr)
	}
	s.mu.Lock()
	s.orphan = nil
	// 2. When s.engine is non-nil and already stopped, StopPrimaryEngineAndWait must NOT stop managed service A.
	s.engine = &engineEntry{state: EngineState{State: "stopped"}}
	s.mu.Unlock()
	if apiErr := s.StopPrimaryEngineAndWait(ctx); apiErr != nil {
		t.Fatalf("expected nil when s.engine is already stopped, got %+v", apiErr)
	}
	if curA, ok := s.ManagedService(stA.ServiceID); !ok || curA.State != "running" {
		t.Fatalf("expected managed service A still running, got ok=%v state=%+v", ok, curA)
	}
	s.mu.Lock()
	s.engine = nil
	s.mu.Unlock()

	// 3. Detached entry with closed done channel and active state must fail fast (engine_stop_failed), not spin.
	closedDone := make(chan struct{})
	close(closedDone)
	s.mu.Lock()
	s.services["detached-svc"] = &engineEntry{
		state: EngineState{ServiceID: "detached-svc", State: "running"},
		stop:  make(chan struct{}),
		done:  closedDone,
	}
	s.mu.Unlock()
	start := time.Now()
	apiErr := s.StopActiveServicesAndWait(ctx)
	if apiErr == nil || apiErr.Code != "engine_stop_failed" {
		t.Fatalf("expected engine_stop_failed for detached active entry, got %+v", apiErr)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("expected fast failure on detached entry, took %v", time.Since(start))
	}
	s.mu.Lock()
	delete(s.services, "detached-svc")
	s.mu.Unlock()
}
