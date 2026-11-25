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

	// Start small-4g (~4.92 GB estimated).
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

	// Attempting to start large-8g (~9.84 GB estimated) with only ~7.08 GB remaining must fail with insufficient_memory.
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
	info := parseHardwareMemoryBudget(hw)
	if !info.Known || info.TotalGB != 24 || info.AvailableGB != 21.6 {
		t.Fatalf("unexpected discrete GPU budget: %+v", info)
	}
}
