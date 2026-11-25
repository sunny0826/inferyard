package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteAssetBlockedByRunningManagedService(t *testing.T) {
	const giB = int64(1024 * 1024 * 1024)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"running-a.gguf": 2 * giB,
		"idle-b.gguf":    2 * giB,
	}, nil)

	st, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "running-a.gguf", Engine: engine, ContextSize: 4096,
	})
	if apiErr != nil {
		t.Fatalf("start running-a failed: %+v", apiErr)
	}
	waitManagedServiceState(t, s, st.ServiceID, "running")

	// Deleting any managed asset while a standalone managed service is active must be blocked.
	if err := s.DeleteManagedAsset("model", "running-a.gguf"); err == nil || err.Code != "conflict" {
		t.Fatalf("expected conflict(workspace_busy) when deleting model with active managed service, got %+v", err)
	}
	if err := s.DeleteManagedAsset("model", "idle-b.gguf"); err == nil || err.Code != "conflict" {
		t.Fatalf("expected conflict(workspace_busy) for other model while managed service is active, got %+v", err)
	}

	// Orphaned multi-service record must also block asset deletion.
	s.StopAllManagedServices()
	s.mu.Lock()
	s.orphanedServices["orphan-svc-1"] = &orphanEngine{
		state: EngineState{ServiceID: "orphan-svc-1", State: "orphaned"},
	}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", "running-a.gguf"); err == nil || err.Code != "conflict" {
		t.Fatalf("expected conflict(workspace_busy) when orphanedServices is non-empty, got %+v", err)
	}
	s.mu.Lock()
	delete(s.orphanedServices, "orphan-svc-1")
	s.mu.Unlock()
}

func TestLANAutoPortAndCachedPlanProbeWildcardHost(t *testing.T) {
	orig := reservePortOnHostFunc
	t.Cleanup(func() { reservePortOnHostFunc = orig })

	var probedHosts []string
	reservePortOnHostFunc = func(host string, port int) (int, error) {
		probedHosts = append(probedHosts, host)
		if host == "0.0.0.0" && port == DefaultBenchmarkPort {
			return 0, errors.New("address already in use on 0.0.0.0")
		}
		if port == 0 {
			return 49123, nil
		}
		return port, nil
	}

	gotPort, err := ChooseFreePortExcluding("0.0.0.0", DefaultBenchmarkPort, nil)
	if err != nil {
		t.Fatalf("ChooseFreePortExcluding failed: %v", err)
	}
	if gotPort != 49123 {
		t.Fatalf("expected fallback port 49123 when 0.0.0.0:%d busy, got %d", DefaultBenchmarkPort, gotPort)
	}
	if len(probedHosts) < 2 || probedHosts[0] != "0.0.0.0" || probedHosts[1] != "0.0.0.0" {
		t.Fatalf("expected wildcard host 0.0.0.0 probes, got %+v", probedHosts)
	}

	// Verify reusableStandalonePlan probes the supplied bindHost (0.0.0.0 for LAN).
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	exePath := filepath.Join(dir, "llama-server")
	if err := os.WriteFile(modelPath, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "serve-plan.json")
	planRaw, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"argv":              []string{exePath, "--host", "127.0.0.1", "--port", "48857"},
		"environment":       map[string]string{},
		"working_directory": dir,
		"endpoint":          "http://127.0.0.1:48857",
		"model_path":        modelPath,
		"model_kind":        "gguf",
		"context_size":      8192,
		"family":            "llama-cpp",
		"adapter":           "prism_llama_server_v1",
		"local":             false,
	})
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	eng := registryEntry{path: exePath, family: "llama-cpp"}

	// On 0.0.0.0, port 48857 is mocked as busy -> cached plan must NOT be reused.
	if _, ok := reusableStandalonePlan(planPath, modelPath, eng, 8192, "0.0.0.0"); ok {
		t.Fatalf("expected cached plan on port 48857 to be rejected when 0.0.0.0:48857 is occupied")
	}
	// On 127.0.0.1, port 48857 is mocked as free -> cached plan can be reused.
	if _, ok := reusableStandalonePlan(planPath, modelPath, eng, 8192, "127.0.0.1"); !ok {
		t.Fatalf("expected cached plan on port 48857 to be reusable on 127.0.0.1")
	}
}

func TestUnexpectedRuntimeExitKeepsMemoryNoticeAndOwnerOnWriteFailure(t *testing.T) {
	const giB = int64(1024 * 1024 * 1024)
	s, workspace, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"crash-model.gguf": 2 * giB,
	}, nil)

	st, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "crash-model.gguf", Engine: engine, ContextSize: 4096,
	})
	if apiErr != nil {
		t.Fatalf("start crash-model failed: %+v", apiErr)
	}
	st = waitManagedServiceState(t, s, st.ServiceID, "running")

	// Locate the owner file and force exit-notice persistence to fail by creating
	// a non-empty directory at service-exit-notices.json so os.Rename fails.
	s.mu.Lock()
	entry := s.services[st.ServiceID]
	ownerPath := entry.ownerPath
	proc := entry.cmd.Process
	s.mu.Unlock()
	if ownerPath == "" {
		t.Fatalf("expected ownerPath to be recorded")
	}
	noticePath := exitNoticePath(workspace)
	if err := os.MkdirAll(filepath.Join(noticePath, "blocker"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Kill the process unexpectedly.
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cur, ok := s.ManagedService(st.ServiceID)
		if ok && cur.State == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// In-memory exit notice must still be preserved even though disk write failed.
	notices := s.ExitedServiceNotices()
	if len(notices) == 0 || notices[0].ServiceID != st.ServiceID {
		t.Fatalf("expected in-memory exit notice for %s to be preserved, got %+v", st.ServiceID, notices)
	}

	// Owner file must NOT be removed when persisting the exit notice failed.
	if _, err := os.Stat(ownerPath); err != nil {
		t.Fatalf("expected owner file %s to be preserved on exit-notice write failure, stat err=%v", ownerPath, err)
	}
}
