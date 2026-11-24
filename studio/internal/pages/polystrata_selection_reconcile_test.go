package pages

import (
	"context"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/core"
)

func TestPendingPolystrataSelectionSurvivesDisambiguatedRefresh(t *testing.T) {
	svc, _, _, _, _ := newModelsPolystrataDeleteService(t)
	waitUntil(t, func() bool { return !svc.Registry().Refreshing })

	oldDir := filepath.Join(t.TempDir(), "ws-old", "engines", "polystrata")
	newDir := filepath.Join(t.TempDir(), "ws-new", "engines", "polystrata")
	oldInst := filepath.Join(oldDir, "installation.json")
	newInst := filepath.Join(newDir, "installation.json")
	oldCfg := filepath.Join(oldDir, "strata-q2_0.json")
	newCfg := filepath.Join(newDir, "strata-q2_0.json")

	s := testState()
	s.ctx = context.Background()
	s.service = svc
	s.discoveryLoaded = true
	s.settings = svc.Settings()
	s.registry = core.Registry{
		Engines: []core.EngineAsset{
			{Name: "PolyStrata · v0.1.45", Family: "polystrata", Kind: "local_installation", Path: oldInst},
		},
		Models: []core.ModelAsset{
			{Name: "qwen3.8-flash-next-q2_0", Format: "strata_pack", Path: oldCfg, Available: true},
		},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			"PolyStrata · v0.1.45": {State: core.EngineCheckEligible, Family: "polystrata"},
		},
	}
	s.engine = "PolyStrata · v0.1.45"
	s.model = "qwen3.8-flash-next-q2_0"

	// Simulate clicking "Start Benchmark" before s.registry includes newInst / newCfg.
	engineMatched, modelMatched := s.applyPolystrataSelection(cleanMatchPath(newInst), cleanMatchPath(newCfg))
	if engineMatched || modelMatched {
		t.Fatalf("expected initial match to miss before refresh: eng=%v mod=%v", engineMatched, modelMatched)
	}
	s.pendingPolystrataSelection = &polystrataSelectionTarget{
		installationPath: cleanMatchPath(newInst),
		modelConfigPath:  cleanMatchPath(newCfg),
		initialEngine:    s.engine,
		initialModel:     s.model,
	}

	// Full refresh arrives with disambiguated names for both engines and models.
	newEngName := "PolyStrata · v0.1.45 · 本地引擎声明 · newhash"
	oldEngName := "PolyStrata · v0.1.45 · 本地引擎声明 · oldhash"
	newModName := "qwen3.8-flash-next-q2_0 · PolyStrata 配置 · newhash"
	oldModName := "qwen3.8-flash-next-q2_0 · PolyStrata 配置 · oldhash"

	batch := refreshBatch{
		epoch:    s.settingsSequence(),
		full:     true,
		settings: s.settings,
		registry: core.Registry{
			Engines: []core.EngineAsset{
				{Name: oldEngName, Family: "polystrata", Kind: "local_installation", Path: oldInst},
				{Name: newEngName, Family: "polystrata", Kind: "local_installation", Path: newInst},
			},
			Models: []core.ModelAsset{
				{Name: oldModName, Format: "strata_pack", Path: oldCfg, Available: true},
				{Name: newModName, Format: "strata_pack", Path: newCfg, Available: true},
			},
		},
	}
	if !s.applyRefreshBatch(batch) {
		t.Fatal("expected refresh batch to apply")
	}
	if s.engine != newEngName || s.model != newModName {
		t.Fatalf("expected pending selection to land on new pair (%q, %q), got (%q, %q)", newEngName, newModName, s.engine, s.model)
	}
	if s.pendingPolystrataSelection != nil {
		t.Fatalf("expected pendingPolystrataSelection cleared after full match")
	}
}

func TestAdoptPairsLatestPolystrataDownloadAndColocatedConfig(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "ws-old", "engines", "polystrata")
	newDir := filepath.Join(t.TempDir(), "ws-new", "engines", "polystrata")
	oldInst := filepath.Join(oldDir, "installation.json")
	newInst := filepath.Join(newDir, "installation.json")
	oldCfg := filepath.Join(oldDir, "strata-q2_0.json")
	newCfg := filepath.Join(newDir, "strata-q2_0.json")

	oldEngName := "PolyStrata · old"
	newEngName := "PolyStrata · new"
	oldModName := "qwen3.8 · old"
	newModName := "qwen3.8 · new"

	s := testState()
	s.registry = core.Registry{
		DefaultEngine: oldEngName,
		Engines: []core.EngineAsset{
			{Name: oldEngName, Family: "polystrata", Kind: "local_installation", Path: oldInst},
			{Name: newEngName, Family: "polystrata", Kind: "local_installation", Path: newInst},
		},
		Models: []core.ModelAsset{
			{Name: oldModName, Format: "strata_pack", Path: oldCfg, Available: true},
			{Name: newModName, Format: "strata_pack", Path: newCfg, Available: true},
		},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			oldEngName: {State: core.EngineCheckEligible, Family: "polystrata"},
			newEngName: {State: core.EngineCheckEligible, Family: "polystrata"},
		},
	}
	// Service.Downloads() orders tasks newest-first.
	s.downloads = []core.DownloadTask{
		{Kind: "polystrata", State: "done", InstallationPath: ptr(newInst), ModelConfigPath: ptr(newCfg)},
		{Kind: "polystrata", State: "done", InstallationPath: ptr(oldInst), ModelConfigPath: ptr(oldCfg)},
	}

	s.adoptDownloadedEngine()
	s.adoptOnlyModel()
	if s.engine != newEngName || s.model != newModName {
		t.Fatalf("expected newest completed polystrata pair (%q, %q), got (%q, %q)", newEngName, newModName, s.engine, s.model)
	}

	// After restart (empty downloads), co-located strata-*.json in the selected engine's dir wins.
	s.downloads = nil
	s.engine = ""
	s.model = ""
	s.adoptDownloadedEngine()
	s.adoptOnlyModel()
	if s.engine != oldEngName || s.model != oldModName {
		t.Fatalf("expected co-located pair (%q, %q) when downloads empty, got (%q, %q)", oldEngName, oldModName, s.engine, s.model)
	}
}
