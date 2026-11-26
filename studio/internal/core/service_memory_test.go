package core

import (
	"encoding/json"
	"testing"
)

func TestServiceMemoryBudgetAllowsWithinLimitAndBlocksOverflow(t *testing.T) {
	// 16 GiB Apple Silicon unified memory -> 12.0 GiB recommended inference ceiling.
	hw := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": true,
			"memory_total_bytes": 17179869184,
			"gpu": {
				"devices": [
					{"name": "Apple M4", "memory_kind": "shared_host", "memory_total_bytes": null}
				]
			}
		}
	}`)
	const giB = int64(1024 * 1024 * 1024)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"small-4g.gguf": 4 * giB,
		"large-8g.gguf": 8 * giB,
	}, hw)

	budgetSmall := s.CheckServiceMemoryBudget("small-4g.gguf", engine, 8192)
	if !budgetSmall.Known || !budgetSmall.Enough || budgetSmall.TotalGB != 16 || budgetSmall.AvailableGB != 12 {
		t.Fatalf("unexpected small budget: %+v", budgetSmall)
	}

	// Start small-4g (~4.92 GiB estimated).
	stSmall, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "small-4g.gguf", Engine: engine, ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("start small-4g failed: %+v", apiErr)
	}
	stSmall = waitManagedServiceState(t, s, stSmall.ServiceID, "running")
	if stSmall.EstimatedMemoryGB <= 0 {
		t.Fatalf("expected positive EstimatedMemoryGB on running service, got %+v", stSmall)
	}

	summary := s.CurrentServiceMemorySummary()
	if !summary.Known || summary.UsedGB <= 0 || summary.RemainingGB >= summary.AvailableGB {
		t.Fatalf("unexpected memory summary with small-4g running: %+v", summary)
	}

	// Attempting to start large-8g (~9.84 GiB estimated) with only ~7.08 GiB remaining must fail with insufficient_memory.
	budgetLarge := s.CheckServiceMemoryBudget("large-8g.gguf", engine, 8192)
	if !budgetLarge.Known || budgetLarge.Enough {
		t.Fatalf("expected large-8g budget to be insufficient, got %+v", budgetLarge)
	}
	if _, err := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "large-8g.gguf", Engine: engine, ContextSize: 8192,
	}); err == nil || err.Code != "insufficient_memory" {
		t.Fatalf("expected insufficient_memory error, got %+v", err)
	}

	// Already-running small-4g must remain running unaffected.
	if cur, ok := s.ManagedService(stSmall.ServiceID); !ok || cur.State != "running" {
		t.Fatalf("expected small-4g unaffected after insufficient_memory rejection, got %+v", cur)
	}
}

func TestServiceMemoryBudgetUnknownHardwareDoesNotBlock(t *testing.T) {
	const giB = int64(1024 * 1024 * 1024)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"huge-64g.gguf": 64 * giB,
	}, nil)

	budget := s.CheckServiceMemoryBudget("huge-64g.gguf", engine, 8192)
	if budget.Known || !budget.Enough {
		t.Fatalf("expected Known=false Enough=true when hardware snapshot missing, got %+v", budget)
	}
	st, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "huge-64g.gguf", Engine: engine, ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("expected start to proceed when hardware memory unknown, got %+v", apiErr)
	}
	waitManagedServiceState(t, s, st.ServiceID, "running")
}

func TestServiceMemoryBudgetDiscreteGPUVRAM(t *testing.T) {
	// 24 GiB discrete GPU VRAM -> 21.6 GiB recommended ceiling.
	hw := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{"name": "RTX 4090", "memory_kind": "dedicated", "memory_total_bytes": 25769803776}
				]
			}
		}
	}`)
	info := parseHardwareMemoryBudget(hw, false)
	if !info.Known || info.TotalGB != 24 || info.AvailableGB != 21.6 {
		t.Fatalf("unexpected discrete GPU budget: %+v", info)
	}
}

func TestServiceMemoryBudgetCPUOnlyUsesHostMemory(t *testing.T) {
	// 8 GiB discrete GPU VRAM + 64 GiB host RAM:
	// Default GPU mode sees 7.2 GiB ceiling (blocks 12 GiB model),
	// whereas CPU-only mode (gpuLayers=0) sees 48.0 GiB host ceiling (allows 12 GiB model).
	hw := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{"name": "RTX 4060", "memory_kind": "dedicated", "memory_total_bytes": 8589934592}
				]
			}
		}
	}`)
	const giB = int64(1024 * 1024 * 1024)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"mid-12g.gguf": 12 * giB,
	}, hw)

	gpuBudget := s.CheckServiceMemoryBudget("mid-12g.gguf", engine, 8192)
	if !gpuBudget.Known || gpuBudget.Enough {
		t.Fatalf("expected GPU budget to reject 12 GiB model on 8 GiB VRAM, got %+v", gpuBudget)
	}

	cpuLayers := 0
	cpuBudget := s.CheckServiceMemoryBudgetForRequest(StandaloneStartRequest{
		Model:       "mid-12g.gguf",
		Engine:      engine,
		ContextSize: 8192,
		GPULayers:   &cpuLayers,
	})
	if !cpuBudget.Known || !cpuBudget.Enough || cpuBudget.TotalGB != 64 || cpuBudget.AvailableGB != 48 {
		t.Fatalf("expected CPU-only budget to use 64 GiB host pool, got %+v", cpuBudget)
	}

	st, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "mid-12g.gguf",
		Engine:      engine,
		ContextSize: 8192,
		GPULayers:   &cpuLayers,
	})
	if apiErr != nil {
		t.Fatalf("expected CPU-only start to succeed without VRAM block, got %+v", apiErr)
	}
	waitManagedServiceState(t, s, st.ServiceID, "running")
}

func TestServiceMemoryBudgetRespectsSnapshotAvailableCeiling(t *testing.T) {
	// 16 GiB total RAM, but only 2 GiB actually available in snapshot -> must cap AvailableGB at 2.0 GiB.
	hostSnap := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": true,
			"memory_total_bytes": 17179869184,
			"memory_available_bytes": 2147483648
		}
	}`)
	hostInfo := parseHardwareMemoryBudget(hostSnap, false)
	if !hostInfo.Known || hostInfo.TotalGB != 16 || hostInfo.AvailableGB != 2 {
		t.Fatalf("expected host AvailableGB capped at 2.0 GiB, got %+v", hostInfo)
	}

	// 24 GiB total discrete VRAM, but only 3 GiB free -> must cap AvailableGB at 3.0 GiB.
	gpuSnap := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{
						"name": "RTX 4090",
						"memory_kind": "dedicated",
						"memory_total_bytes": 25769803776,
						"memory_free_bytes": 3221225472
					}
				]
			}
		}
	}`)
	gpuInfo := parseHardwareMemoryBudget(gpuSnap, false)
	if !gpuInfo.Known || gpuInfo.TotalGB != 24 || gpuInfo.AvailableGB != 3 {
		t.Fatalf("expected GPU AvailableGB capped at 3.0 GiB, got %+v", gpuInfo)
	}
}

func TestServiceMemoryBudgetDiscreteGPUSeparatesCPUAndGPUPools(t *testing.T) {
	// 64 GiB host RAM + 8 GiB discrete GPU VRAM:
	// Starting a 12 GiB model with GPULayers=0 (host RAM pool) must NOT block
	// starting a 4 GiB model on the discrete GPU VRAM pool.
	hw := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{"name": "RTX 4060", "memory_kind": "dedicated", "memory_total_bytes": 8589934592}
				]
			}
		}
	}`)
	const giB = int64(1024 * 1024 * 1024)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"cpu-12g.gguf": 12 * giB,
		"gpu-4g.gguf":  4 * giB,
	}, hw)

	cpuLayers := 0
	stCPU, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "cpu-12g.gguf",
		Engine:      engine,
		ContextSize: 8192,
		GPULayers:   &cpuLayers,
	})
	if apiErr != nil {
		t.Fatalf("expected CPU-only 12 GiB start to succeed, got %+v", apiErr)
	}
	waitManagedServiceState(t, s, stCPU.ServiceID, "running")

	// GPU budget must ignore the running CPU-only service (UsedGB == 0) and allow gpu-4g.
	gpuBudget := s.CheckServiceMemoryBudget("gpu-4g.gguf", engine, 8192)
	if !gpuBudget.Known || !gpuBudget.Enough || gpuBudget.UsedGB != 0 {
		t.Fatalf("expected GPU budget to exclude CPU-only service and allow gpu-4g, got %+v", gpuBudget)
	}
	stGPU, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "gpu-4g.gguf",
		Engine:      engine,
		ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("expected GPU 4 GiB start to succeed alongside CPU-only 12 GiB service, got %+v", apiErr)
	}
	waitManagedServiceState(t, s, stGPU.ServiceID, "running")
}

func TestServiceMemoryBudgetDeviceRefreshDoesNotDoubleCountRunningService(t *testing.T) {
	// 24 GiB discrete GPU VRAM:
	// 1. Start a 10 GiB model (~12.3 GiB estimated).
	// 2. Refresh device snapshot reporting 12 GiB free VRAM (already reflecting the running service).
	// 3. Starting a 4 GiB model (~4.92 GiB estimated) must be allowed instead of double-subtracting 12.3 GiB.
	const giB = int64(1024 * 1024 * 1024)
	initialHW := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{"name": "RTX 4090", "memory_kind": "dedicated", "memory_total_bytes": 25769803776}
				]
			}
		}
	}`)
	s, _, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"big-10g.gguf":  10 * giB,
		"small-4g.gguf": 4 * giB,
	}, initialHW)

	stBig, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "big-10g.gguf",
		Engine:      engine,
		ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("start big-10g failed: %+v", apiErr)
	}
	waitManagedServiceState(t, s, stBig.ServiceID, "running")

	// Simulate a device refresh captured after big-10g started, reporting 12 GiB free VRAM.
	refreshedHW := json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {
			"apple_silicon": false,
			"memory_total_bytes": 68719476736,
			"gpu": {
				"devices": [
					{
						"name": "RTX 4090",
						"memory_kind": "dedicated",
						"memory_total_bytes": 25769803776,
						"memory_free_bytes": 12884901888
					}
				]
			}
		}
	}`)
	s.mu.Lock()
	s.device.Snapshot = refreshedHW
	s.device.CapturedAt = ptr(now())
	s.mu.Unlock()

	budgetSmall := s.CheckServiceMemoryBudget("small-4g.gguf", engine, 8192)
	if !budgetSmall.Known || !budgetSmall.Enough || budgetSmall.RemainingGB < 5 {
		t.Fatalf("expected small-4g allowed after device refresh (no double counting), got %+v", budgetSmall)
	}
	stSmall, apiErr := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "small-4g.gguf",
		Engine:      engine,
		ContextSize: 8192,
	})
	if apiErr != nil {
		t.Fatalf("expected small-4g start to succeed after device refresh, got %+v", apiErr)
	}
	waitManagedServiceState(t, s, stSmall.ServiceID, "running")
}
