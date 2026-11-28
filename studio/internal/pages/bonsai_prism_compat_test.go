package pages

import (
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestPrismOnlyGGUFRejectsUpstreamAndLMStudioAndReconcilesToPrism(t *testing.T) {
	ptq1 := core.ModelAsset{Name: "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf", Path: "/models/Ternary-Bonsai-2.5-27B-PTQ1_0.gguf", Format: "gguf", Available: true}
	pq2 := core.ModelAsset{Name: "Ternary-Bonsai-2.5-27B-PQ2_0.gguf", Path: "/models/Ternary-Bonsai-2.5-27B-PQ2_0.gguf", Format: "gguf", Architecture: "qwen35", Available: true}
	upstream := core.EngineAsset{Name: "llama-server · b11146", Family: "llama-cpp", Kind: "local_installation", Version: "b11146", Distribution: "ggml-org"}
	lmstudio := core.EngineAsset{Name: "LM Studio", Family: "lmstudio", Kind: "local_installation", Version: "0.4.0"}
	polystrata := core.EngineAsset{Name: "PolyStrata · v0.2.20-poly.5", Family: "polystrata", Kind: "local_installation", Version: "v0.2.20-poly.5"}
	prism := core.EngineAsset{Name: "llama-cpp-prism · llama-server", Family: "llama-cpp", Version: "prism-b12232-f702476", Distribution: "PrismML"}

	for _, m := range []core.ModelAsset{ptq1, pq2} {
		if compatibleStandaloneEngine(m, upstream) || compatibleBenchmarkEngine(m, upstream) {
			t.Fatalf("expected upstream ggml-org llama.cpp to reject %s", m.Name)
		}
		if compatibleStandaloneEngine(m, lmstudio) || compatibleBenchmarkEngine(m, lmstudio) {
			t.Fatalf("expected lmstudio to reject %s", m.Name)
		}
		if compatibleStandaloneEngine(m, polystrata) || compatibleBenchmarkEngine(m, polystrata) {
			t.Fatalf("expected polystrata to reject %s even when Architecture=%q", m.Name, m.Architecture)
		}
		if !compatibleStandaloneEngine(m, prism) || !compatibleBenchmarkEngine(m, prism) {
			t.Fatalf("expected PrismML llama.cpp to accept %s", m.Name)
		}
	}

	s := readyState("dashboard")
	s.engine = upstream.Name
	s.model = ptq1.Name
	s.registry = core.Registry{
		Models:  []core.ModelAsset{ptq1, pq2},
		Engines: []core.EngineAsset{upstream, prism},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			upstream.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			prism.Name:    {State: core.EngineCheckEligible, Family: "llama-cpp"},
		},
	}
	if got := s.resolveCompatibleEngineForModel(pq2.Name); got != prism.Name {
		t.Fatalf("expected resolveCompatibleEngineForModel(%s) = %q, got %q", pq2.Name, prism.Name, got)
	}
	if compat := s.compatibleModelsForSelectedEngine(); len(compat) != 0 {
		t.Fatalf("expected upstream engine to have 0 compatible Bonsai models, got %+v", compat)
	}
	s.reconcileModelOnEngineChange(prism.Name)
	if s.model != "" {
		t.Fatalf("expected switching to upstream engine to clear incompatible Bonsai model, got %q", s.model)
	}
	s.engine = prism.Name
	compat := s.compatibleModelsForSelectedEngine()
	if len(compat) != 2 || compat[0].Name != ptq1.Name || compat[1].Name != pq2.Name {
		t.Fatalf("expected PrismML engine to list [ptq1, pq2], got %+v", compat)
	}

	for _, id := range []string{"model-bonsai-2.5-27b-ptq1", "model-bonsai-2.5-27b-pq2"} {
		entry, ok := core.CatalogEntryByID(id)
		if !ok {
			t.Fatalf("missing catalog entry %s", id)
		}
		if got := modelCatalogEngineLabel(entry); got != "llama.cpp (PrismML)" {
			t.Fatalf("modelCatalogEngineLabel(%s) = %q, want llama.cpp (PrismML)", id, got)
		}
	}

	// Upstream-only installation must not satisfy starter engine check for PTQ1_0 / PQ2_0 options.
	s.registry.Engines = []core.EngineAsset{upstream}
	s.checks.Engines = map[string]core.EngineEligibility{
		upstream.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
	}
	if s.isStarterEngineInstalled(starterOption{Engine: "llama-cpp", Backend: "metal", ModelID: "model-bonsai-2.5-27b-ptq1"}) {
		t.Fatal("upstream ggml-org engine must not satisfy PTQ1_0 starter requirement")
	}
	s.registry.Engines = []core.EngineAsset{prism}
	s.checks.Engines = map[string]core.EngineEligibility{
		prism.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
	}
	if !s.isStarterEngineInstalled(starterOption{Engine: "llama-cpp", Backend: "metal", ModelID: "model-bonsai-2.5-27b-ptq1"}) {
		t.Fatal("PrismML engine should satisfy PTQ1_0 starter requirement")
	}
}

func TestPrismBinaryEngineNotDeadlockedByMissingPreflightWhenLocalEngineCoexists(t *testing.T) {
	pq2 := core.ModelAsset{Name: "Ternary-Bonsai-2.5-27B-PQ2_0.gguf", Path: "/models/Ternary-Bonsai-2.5-27B-PQ2_0.gguf", Format: "gguf", Available: true}
	polystrata := core.EngineAsset{Name: "PolyStrata · v0.2.20-poly.6", Family: "polystrata", Kind: "local_installation", Version: "v0.2.20-poly.6"}
	prism := core.EngineAsset{Name: "llama-cpp-prism-b12232-f702476 · llama-server", Family: "llama-cpp", Version: "prism-b12232-f702476", Distribution: "PrismML"}

	s := readyState("dashboard")
	s.engine = prism.Name
	s.model = pq2.Name
	s.registry = core.Registry{
		Models:  []core.ModelAsset{pq2},
		Engines: []core.EngineAsset{prism, polystrata},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			prism.Name:      {State: core.EngineCheckEligible, Family: "llama-cpp"},
			polystrata.Name: {State: core.EngineCheckEligible, Family: "polystrata"},
		},
	}
	s.progress = &inferyard.WorkspaceProgress{
		Workspace:       "/tmp/ws",
		NextAction:      "device_check",
		DevicePreflight: inferyard.ProgressPreflight{Status: "missing"},
	}

	if !s.preflightReady() {
		t.Fatal("expected preflightReady=true when local_installation engine skips device_check stage and preflight is missing")
	}
	if reason := s.speedStartBlockReason(); reason != "" {
		t.Fatalf("expected context speed to be unblocked for PrismML + Bonsai, got %q", reason)
	}

	// Blocked preflight must remain blocked.
	s.progress.DevicePreflight.Status = "blocked"
	if s.preflightReady() {
		t.Fatal("expected preflightReady=false when DevicePreflight.Status == blocked")
	}

	// Without any local_installation engine, stageAction enters device_check and preflightReady stays false.
	s.progress.DevicePreflight.Status = "missing"
	s.registry.Engines = []core.EngineAsset{prism}
	if s.preflightReady() {
		t.Fatal("expected preflightReady=false when no local_installation engine exists and preflight is missing")
	}
}
