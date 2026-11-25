package pages

import (
	"encoding/json"
	"testing"

	"inferyard/studio/internal/core"
)

func TestModelsStandaloneMultiServiceRowAndBenchmarkHandoff(t *testing.T) {
	m1 := core.ModelAsset{Name: "m1.gguf", Path: "/models/m1.gguf", Format: "gguf", Available: true}
	m2 := core.ModelAsset{Name: "m2.gguf", Path: "/models/m2.gguf", Format: "gguf", Available: true}
	m3 := core.ModelAsset{Name: "m3.gguf", Path: "/models/m3.gguf", Format: "gguf", Available: true}
	svc1 := core.EngineState{
		ServiceID:  core.ServiceIDFor("m1.gguf", "llama-a"),
		State:      "running",
		Origin:     core.EngineOriginStandalone,
		ModelName:  "m1.gguf",
		EngineName: "llama-a",
		Model:      "/models/m1.gguf",
		Endpoint:   "http://127.0.0.1:48857",
		LogPath:    "/logs/m1.log",
	}
	svc2 := core.EngineState{
		ServiceID:  core.ServiceIDFor("m2.gguf", "llama-a"),
		State:      "running",
		Origin:     core.EngineOriginStandalone,
		ModelName:  "m2.gguf",
		EngineName: "llama-a",
		Model:      "/models/m2.gguf",
		Endpoint:   "http://0.0.0.0:48858",
		LANAccess:  true,
		LogPath:    "/logs/m2.log",
	}
	s := &viewState{
		serveSupported:  true,
		registry:        core.Registry{Models: []core.ModelAsset{m1, m2, m3}},
		engineState:     svc1,
		managedServices: []core.EngineState{svc1, svc2},
	}

	// Second running service must be recognized in the models table with its own endpoint/logPath.
	rowState, matchedSvc, ep, logPath, _ := s.standaloneRowState(m2)
	if rowState != "running" || matchedSvc.ServiceID != svc2.ServiceID || ep != "http://0.0.0.0:48858" || logPath != "/logs/m2.log" {
		t.Fatalf("unexpected rowState for m2: state=%q svc=%+v ep=%q log=%q", rowState, matchedSvc, ep, logPath)
	}

	// Idle m3 must not match running services, and requesting start opens drawer directly.
	rowState3, _, _, _, _ := s.standaloneRowState(m3)
	if rowState3 != "" {
		t.Fatalf("expected empty rowState for idle m3, got %q", rowState3)
	}
	s.requestStandaloneStart(m3, false)
	if !s.standalone.drawerOpen || s.standalone.modelName != "m3.gguf" || s.standalone.switchConfirm {
		t.Fatalf("expected parallel drawer open for m3, got %+v", s.standalone)
	}

	// Benchmark conflict detection must see active managed services even if engineState is stopped.
	s.engineState = core.EngineState{State: "stopped"}
	if !s.standaloneRunningForBenchmarkConflict() {
		t.Fatalf("expected benchmark conflict when managedServices has active standalone services")
	}
}

func TestUICheckMemoryBudgetCPUOnlyAndNoDiskScan(t *testing.T) {
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
	s := &viewState{
		device: core.DeviceResponse{Snapshot: hw},
		registry: core.Registry{
			Models: []core.ModelAsset{
				{Name: "mid-12g.gguf", SizeBytes: int64Ptr(12 * giB), Available: true},
			},
		},
		memorySummary: core.ServiceMemoryBudget{
			TotalGB:     8,
			AvailableGB: 7.2,
			RemainingGB: 7.2,
			Enough:      true,
			Known:       true,
		},
	}

	gpuBudget := s.checkMemoryBudget("mid-12g.gguf", "llama-a", 8192, nil)
	if !gpuBudget.Known || gpuBudget.Enough {
		t.Fatalf("expected GPU budget to block 12 GiB model on 8 GiB VRAM, got %+v", gpuBudget)
	}

	cpuLayers := 0
	cpuBudget := s.checkMemoryBudget("mid-12g.gguf", "llama-a", 8192, &cpuLayers)
	if !cpuBudget.Known || !cpuBudget.Enough || cpuBudget.TotalGB != 64 || cpuBudget.AvailableGB != 48 {
		t.Fatalf("expected CPU-only budget to use 64 GiB host pool, got %+v", cpuBudget)
	}

	// Running CPU-only 12 GiB service on a discrete GPU machine must not count against GPU VRAM pool.
	s.registry.Models = append(s.registry.Models, core.ModelAsset{
		Name: "gpu-4g.gguf", SizeBytes: int64Ptr(4 * giB), Available: true,
	})
	s.managedServices = []core.EngineState{
		{
			ServiceID:         core.ServiceIDFor("mid-12g.gguf", "llama-a"),
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			ModelName:         "mid-12g.gguf",
			EngineName:        "llama-a",
			GPULayers:         &cpuLayers,
			EstimatedMemoryGB: 14.76,
			StartedAt:         "2026-11-25T07:00:00Z",
		},
	}
	gpuSmallBudget := s.checkMemoryBudget("gpu-4g.gguf", "llama-a", 8192, nil)
	if !gpuSmallBudget.Known || !gpuSmallBudget.Enough || gpuSmallBudget.UsedGB != 0 {
		t.Fatalf("expected UI GPU budget to ignore CPU-only service, got %+v", gpuSmallBudget)
	}

	// Device refresh after a GPU service starts must not double-subtract its VRAM usage.
	capturedAt := "2026-11-25T07:05:00Z"
	s.device = core.DeviceResponse{
		CapturedAt: &capturedAt,
		Snapshot: json.RawMessage(`{
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
		}`),
	}
	s.managedServices = []core.EngineState{
		{
			ServiceID:         core.ServiceIDFor("big-10g.gguf", "llama-a"),
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			ModelName:         "big-10g.gguf",
			EngineName:        "llama-a",
			EstimatedMemoryGB: 12.3,
			StartedAt:         "2026-11-25T07:00:00Z",
		},
	}
	refreshedBudget := s.checkMemoryBudget("gpu-4g.gguf", "llama-a", 8192, nil)
	if !refreshedBudget.Known || !refreshedBudget.Enough || refreshedBudget.RemainingGB < 5 {
		t.Fatalf("expected UI GPU budget after refresh not to double-count running service, got %+v", refreshedBudget)
	}
}

func TestStandaloneHandoffCopyKeysSingleVsMulti(t *testing.T) {
	tk, mk, ck := standaloneHandoffCopyKeys(1)
	if tk != "models.serve.handoff.title" || mk != "models.serve.handoff.message" || ck != "models.serve.handoff.confirm" {
		t.Fatalf("unexpected single-service handoff keys: %q %q %q", tk, mk, ck)
	}
	tk2, mk2, ck2 := standaloneHandoffCopyKeys(2)
	if tk2 != "models.serve.handoff.multi_title" || mk2 != "models.serve.handoff.multi_message" || ck2 != "models.serve.handoff.multi_confirm" {
		t.Fatalf("unexpected multi-service handoff keys: %q %q %q", tk2, mk2, ck2)
	}
}
