package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartStandaloneConcurrentDifferentServicesNotBlocked(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
		"model-b.gguf": 4,
	}, nil)

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			for i := 2; i+1 < len(args); i++ {
				if args[i] == "--model" && strings.HasSuffix(args[i+1], "model-a.gguf") {
					close(startedA)
					<-releaseA
					break
				}
			}
		}
		return origExec(ctx, path, args...)
	}

	type res struct {
		st  EngineState
		err *ApiError
	}
	doneA := make(chan res, 1)
	go func() {
		st, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
		})
		doneA <- res{st, err}
	}()

	select {
	case <-startedA:
	case <-time.After(3 * time.Second):
		t.Fatal("model-a did not enter serve prepare")
	}

	// While model-a is blocked in serve prepare, model-b must start without blocking on settingsMu.
	doneB := make(chan res, 1)
	go func() {
		st, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-b.gguf", Engine: engine, ContextSize: 4096,
		})
		doneB <- res{st, err}
	}()

	var resB res
	select {
	case resB = <-doneB:
	case <-time.After(3 * time.Second):
		close(releaseA)
		t.Fatal("model-b was blocked while model-a was in serve prepare")
	}
	if resB.err != nil {
		close(releaseA)
		t.Fatalf("model-b StartStandalone failed: %+v", resB.err)
	}
	stB := waitManagedServiceState(t, s, resB.st.ServiceID, "running")

	close(releaseA)
	resA := <-doneA
	if resA.err != nil {
		t.Fatalf("model-a StartStandalone failed: %+v", resA.err)
	}
	stA := waitManagedServiceState(t, s, resA.st.ServiceID, "running")
	if stA.Port == stB.Port {
		t.Fatalf("expected distinct ports for concurrent starts, both got %d", stA.Port)
	}
}

func TestStartStandaloneConcurrentSameServiceIDBlocked(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
	}, nil)

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	var prepareCalls atomic.Int32
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			if prepareCalls.Add(1) == 1 {
				close(startedA)
				<-releaseA
			}
		}
		return origExec(ctx, path, args...)
	}

	type res struct {
		st  EngineState
		err *ApiError
	}
	done1 := make(chan res, 1)
	go func() {
		st, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
		})
		done1 <- res{st, err}
	}()

	select {
	case <-startedA:
	case <-time.After(3 * time.Second):
		t.Fatal("1st start did not enter serve prepare")
	}

	// Second concurrent start for the exact same (model, engine) must fail immediately without duplicate prepare.
	_, err2 := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
	})
	if err2 == nil || err2.Code != "engine_candidate_mismatch" {
		close(releaseA)
		t.Fatalf("expected engine_candidate_mismatch on concurrent same serviceID, got %+v", err2)
	}

	close(releaseA)
	res1 := <-done1
	if res1.err != nil {
		t.Fatalf("1st start failed: %+v", res1.err)
	}
	waitManagedServiceState(t, s, res1.st.ServiceID, "running")
	if prepareCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 serve prepare call, got %d", prepareCalls.Load())
	}
}

func TestStartStandaloneRechecksRunActiveBeforeSpawn(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
	}, nil)

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			close(startedA)
			<-releaseA
		}
		return origExec(ctx, path, args...)
	}

	done := make(chan *ApiError, 1)
	go func() {
		_, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-a.gguf", Engine: engine, ContextSize: 4096,
		})
		done <- err
	}()

	select {
	case <-startedA:
	case <-time.After(3 * time.Second):
		t.Fatal("start did not enter serve prepare")
	}

	s.mu.Lock()
	s.runs = append(s.runs, &runEntry{Task: RunTask{State: "running"}})
	s.mu.Unlock()
	close(releaseA)

	apiErr := <-done
	if apiErr == nil || apiErr.Code != "run_active" {
		t.Fatalf("expected run_active when run starts during serve prepare, got %+v", apiErr)
	}
	if svcs := s.ManagedServices(); len(svcs) != 0 {
		t.Fatalf("expected 0 managed services spawned, got %+v", svcs)
	}
}

func TestStartStandaloneConcurrentMemoryBudgetPreventsOvercommit(t *testing.T) {
	const giB = int64(1 << 30)
	// 16 GiB unified memory -> 12 GiB cap.
	snap := json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":17179869184}}`)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4 * giB,
		"model-b.gguf": 8 * giB,
	}, snap)

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			close(startedA)
			<-releaseA
		}
		return origExec(ctx, path, args...)
	}

	doneA := make(chan *ApiError, 1)
	go func() {
		_, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-a.gguf", Engine: engine, ContextSize: 8192,
		})
		doneA <- err
	}()

	select {
	case <-startedA:
	case <-time.After(3 * time.Second):
		t.Fatal("model-a did not enter serve prepare")
	}

	// model-a reserves ~4.9 GiB during prepare; model-b (~9.8 GiB) must fail with insufficient_memory.
	_, errB := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 8192,
	})
	close(releaseA)
	if errA := <-doneA; errA != nil {
		t.Fatalf("model-a failed: %+v", errA)
	}
	if errB == nil || errB.Code != "insufficient_memory" {
		t.Fatalf("expected insufficient_memory for concurrent model-b, got %+v", errB)
	}
}

func TestStartStandaloneBlocksWorkspaceSwitchAssetDeleteAndExtraArgsPortSteal(t *testing.T) {
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4,
		"model-b.gguf": 4,
	}, nil)

	portA := freePort(t)
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			for i := 2; i+1 < len(args); i++ {
				if args[i] == "--model" && strings.HasSuffix(args[i+1], "model-a.gguf") {
					close(startedA)
					<-releaseA
					break
				}
			}
		}
		return origExec(ctx, path, args...)
	}

	doneA := make(chan *ApiError, 1)
	go func() {
		_, err := s.StartStandalone(context.Background(), StandaloneStartRequest{
			Model: "model-a.gguf", Engine: engine, ContextSize: 4096, Port: portA,
		})
		doneA <- err
	}()

	select {
	case <-startedA:
	case <-time.After(3 * time.Second):
		t.Fatal("model-a did not enter serve prepare")
	}

	// Workspace switch must be blocked while model-a is preparing.
	otherWS := t.TempDir()
	rawOtherWS, _ := json.Marshal(otherWS)
	if _, wsErr := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawOtherWS}); wsErr == nil || wsErr.Code != "workspace_busy" {
		close(releaseA)
		t.Fatalf("expected workspace_busy on workspace switch during prepare, got %+v", wsErr)
	}

	// Asset deletion busy guard must treat preparing services as busy.
	s.mu.Lock()
	busy := s.assetWorkspaceBusyLocked()
	s.mu.Unlock()
	if !busy {
		close(releaseA)
		t.Fatal("expected assetWorkspaceBusyLocked() == true while service is preparing")
	}

	// model-b trying to hijack portA via ExtraArgs must fail with engine_port_in_use.
	_, errB := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-b.gguf", Engine: engine, ContextSize: 4096,
		ExtraArgs: []string{"--port", strconv.Itoa(portA)},
	})
	if errB == nil || errB.Code != "engine_port_in_use" {
		close(releaseA)
		t.Fatalf("expected engine_port_in_use when ExtraArgs targets reserved port %d, got %+v", portA, errB)
	}

	close(releaseA)
	if errA := <-doneA; errA != nil {
		t.Fatalf("model-a should succeed after release, got %+v", errA)
	}
}

func TestStartManagedServiceLocalExtraArgsPortMismatchAndNoDoubleCount(t *testing.T) {
	const giB = int64(1 << 30)
	snap := json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":17179869184}}`)
	s, workspace, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-a.gguf": 4 * giB,
		"model-b.gguf": 4 * giB,
	}, snap)

	// 1. Memory handoff: even if preparingServices still holds an entry alongside an active service,
	// activeServicesMemoryForPoolLocked deduplicates by serviceID and does not double-count.
	stA, errA := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: "model-a.gguf", Engine: engine, ContextSize: 8192,
	})
	if errA != nil {
		t.Fatalf("model-a start failed: %+v", errA)
	}
	s.mu.Lock()
	s.preparingServices[stA.ServiceID] = preparingService{estimatedMemoryGB: stA.EstimatedMemoryGB}
	s.mu.Unlock()
	budgetB := s.CheckServiceMemoryBudget("model-b.gguf", engine, 8192)
	s.mu.Lock()
	delete(s.preparingServices, stA.ServiceID)
	s.mu.Unlock()
	if !budgetB.Known || !budgetB.Enough || budgetB.UsedGB > stA.EstimatedMemoryGB+0.01 {
		t.Fatalf("expected deduplicated UsedGB=%.2f and Enough=true, got %+v", stA.EstimatedMemoryGB, budgetB)
	}

	// 2. Local=true startup whose Args --port disagrees with Endpoint port:
	//    - targeting another service's reserved port returns engine_port_in_use
	//    - targeting an unrelated free port returns invalid_input
	exePath := filepath.Join(workspace, "engines", "llama-multi", "llama-server")
	portReserved := freePort(t)
	portEndpoint := freePort(t)
	if portEndpoint == portReserved {
		portEndpoint = freePort(t)
	}
	s.mu.Lock()
	s.reservedPorts[portReserved] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.reservedPorts, portReserved)
		s.mu.Unlock()
	}()

	localStartup := Startup{
		Kind:             "direct_process",
		Path:             exePath,
		Args:             []string{"--host", "127.0.0.1", "--port", strconv.Itoa(portEndpoint), "--port", strconv.Itoa(portReserved)},
		Family:           "vllm",
		Local:            true,
		WorkingDirectory: filepath.Dir(exePath),
		Endpoint:         "http://127.0.0.1:" + strconv.Itoa(portEndpoint),
	}
	if _, err := s.startManagedServiceLocked(localStartup, "slug-local-conflict", "svc-local-conflict", portEndpoint, workspace); err == nil || err.Code != "engine_port_in_use" {
		t.Fatalf("expected engine_port_in_use for Local=true ExtraArgs targeting reserved port, got %+v", err)
	}

	portFree := freePort(t)
	localStartup.Args = []string{"--host", "127.0.0.1", "--port", strconv.Itoa(portEndpoint), "--port", strconv.Itoa(portFree)}
	if _, err := s.startManagedServiceLocked(localStartup, "slug-local-split", "svc-local-split", portEndpoint, workspace); err == nil || err.Code != "invalid_input" {
		t.Fatalf("expected invalid_input for Local=true Endpoint/Args port split, got %+v", err)
	}
	for _, bad := range [][]string{
		{"--host", "127.0.0.1", "--port", "0"},
		{"--host", "127.0.0.1", "--port", "not-a-port"},
	} {
		localStartup.Args = bad
		if _, err := s.startManagedServiceLocked(localStartup, "slug-local-bad", "svc-local-bad", portEndpoint, workspace); err == nil || err.Code != "invalid_input" {
			t.Fatalf("expected invalid_input for Local=true bad --port %v, got %+v", bad, err)
		}
	}
}
