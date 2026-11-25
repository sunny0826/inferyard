package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
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

	// When user selects oldEngName and clears model, adoptOnlyModel picks oldModName even though newTask is first.
	s.engine = oldEngName
	s.model = ""
	s.adoptOnlyModel()
	if s.model != oldModName {
		t.Fatalf("expected model matching selected engine %q (%q), got %q", oldEngName, oldModName, s.model)
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

func TestPolystrataBenchmarkVerifiesAndSelectsWithoutLibraryVisit(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(ws, "models")
	resultsRoot := filepath.Join(ws, "results")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	psDir := filepath.Join(ws, "engines", "polystrata")
	packDir := filepath.Join(dataDir, "packs", "qwen38")
	for _, dir := range []string{resultsRoot, filepath.Join(psDir, "engine-poly"), packDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(psDir, "engine-poly", "BUILD.json"), []byte(`{"version":"v0.1.45"}`), 0o600)
	_ = os.WriteFile(filepath.Join(packDir, "weights.bin"), []byte("weights"), 0o600)
	cfgPath := filepath.Join(psDir, "strata-q2_0.json")
	cfgRaw, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--pack", packDir},
		"model_name": "qwen3.8-flash-next-q2_0",
	})
	_ = os.WriteFile(cfgPath, cfgRaw, 0o600)

	instPath := filepath.Join(psDir, "installation.json")
	instRaw, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": psDir, "data_dir": dataDir,
	})
	_ = os.WriteFile(instPath, instRaw, 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	settingsPath := filepath.Join(base, "settings.json")
	rawSettings, _ := json.Marshal(core.UserSettings{
		InferyardPath:       ptr(os.Args[0]),
		WorkspaceRoot:       &ws,
		EngineInstallations: []string{instPath},
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	var checkCalls atomic.Int32
	svc, err := core.New(ctx, core.Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(base, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset_scan.v1","models":[],"engines":[],"total_model_bytes":0}}`)}, nil
			}
			if len(args) > 0 && args[0] == "device-check" {
				return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
			}
			if len(args) >= 3 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
				checkCalls.Add(1)
				return inferyard.Execution{Stdout: []byte(`{"command":"engine check","status":"checked","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"local-engine-check.v2","eligible":true,"reason":null,"family":"polystrata","declaration_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","verification_scope":"installation_runtime_closure_only","model_requests_sent":0,"ready_to_run":false}}`)}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return !svc.Registry().Refreshing })

	s := testState()
	s.ctx = ctx
	s.service = svc
	s.loaded = true
	s.ready = core.Readiness{
		CLI:       core.InferyardProbe{Status: "ok"},
		Workspace: core.DirectoryReadiness{Initialized: true, Exists: true},
		Results:   core.DirectoryReadiness{Exists: true},
		Models:    core.DirectoryReadiness{Exists: true},
	}
	s.registry = svc.Registry()
	s.checks = svc.EngineChecks()
	s.progress = &inferyard.WorkspaceProgress{
		Workspace:       ws,
		NextAction:      "device_check",
		DevicePreflight: inferyard.ProgressPreflight{Status: "missing"},
		Bundles: []inferyard.BundleProgress{
			{Bundle: inferyard.DefaultBundleCore, Phase: "device_missing", NextAction: "device_check"},
			{Bundle: inferyard.DefaultBundlePelican, Phase: "device_missing", NextAction: "device_check"},
		},
	}
	s.adoptOnlyModel()
	if s.model == "" {
		t.Fatalf("expected PolyStrata model auto-selected, got models=%+v", s.registry.Models)
	}
	// Before engine verification completes (s.engine == ""), must not report a false format mismatch.
	if gotErr := s.localSelectionError(); gotErr != "" {
		t.Fatalf("expected empty localSelectionError when no engine selected yet, got %q", gotErr)
	}
	if !s.benchmarkNeedsEngineVerify() {
		t.Fatal("expected benchmarkNeedsEngineVerify=true when NextAction=device_check and local engine exists")
	}
	if gotStep := s.step(); gotStep != 2 {
		t.Fatalf("expected step()=2 (configure) when local engine skips device_check, got %d", gotStep)
	}

	// Entering via openBenchmarksFlow (s.task.stage == "") even while refreshing=true must
	// neither flash no_engines on frame 1 nor skip VerifyEngines.
	uiCallbacks := make(chan func(), 32)
	s.updateWindow = func(fn func()) { uiCallbacks <- fn }
	wake, off, cursor := svc.Bus.Subscribe()
	defer off()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				events, _, next := svc.Bus.After(cursor)
				cursor = next
				for _, ev := range events {
					if ev.Type == "engine.check" {
						s.runOnUI(func() {
							s.checks = svc.EngineChecks()
							s.adoptDownloadedEngine()
							s.adoptOnlyModel()
						})
					}
				}
			}
		}
	}()

	s.refreshing = true
	s.openBenchmarksFlow()
	tt := ui.NewTester(s.benchmarksView, 960, 720)
	tt.Frame()

	noEnginesCopy := l10n.T(l10n.LanguageZhHans, "dashboard.candidate.no_engines")
	mismatchCopy := l10n.T(l10n.LanguageZhHans, "engine.local.error.format_mismatch")
	firstFrameTexts := strings.Join(tt.Texts(), "\n")
	if strings.Contains(firstFrameTexts, noEnginesCopy) || strings.Contains(firstFrameTexts, mismatchCopy) {
		t.Fatalf("frame 1 must not flash %q or %q while verification starts, got:\n%s", noEnginesCopy, mismatchCopy, firstFrameTexts)
	}

	deadline := time.After(5 * time.Second)
	for s.engine == "" {
		select {
		case fn := <-uiCallbacks:
			fn()
			tt.Frame()
		case <-deadline:
			t.Fatalf("timed out waiting for UI callback to adopt PolyStrata engine; checks=%+v", s.checks)
		}
	}

	if checkCalls.Load() == 0 {
		t.Fatal("expected VerifyEngines to invoke CLI engine check --installation")
	}
	if s.localEngine() == nil || s.localEngine().Family != "polystrata" {
		t.Fatalf("expected PolyStrata engine adopted after verification, got engine=%q checks=%+v", s.engine, s.checks)
	}
	texts := strings.Join(tt.Texts(), "\n")
	if strings.Contains(texts, noEnginesCopy) || strings.Contains(texts, mismatchCopy) {
		t.Fatalf("expected neither %q nor %q once PolyStrata verified, got:\n%s", noEnginesCopy, mismatchCopy, texts)
	}
}

func TestManagedServiceBenchmarkHandoffAndIneligibleEngineSurfacing(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	psEng := core.EngineAsset{
		Name:   "PolyStrata · v0.1.45",
		Family: "polystrata",
		Kind:   "local_installation",
		Path:   "/tmp/polystrata/installation.json",
	}
	psMod := core.ModelAsset{
		Name:      "qwen3.8-flash-next-q2_0",
		Format:    "strata_pack",
		Path:      "/tmp/polystrata/strata-q2_0.json",
		Available: true,
	}
	s.registry.Engines = append(s.registry.Engines, psEng)
	s.registry.Models = append(s.registry.Models, psMod)
	s.checks.Engines[psEng.Name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "polystrata"}

	// Adopted PolyStrata service with blank EngineName falls back to unambiguous Family match,
	// clears stale failed round A state, and sets prepare_candidate.
	adoptedSvc := core.EngineState{
		ServiceID: "svc-adopted-ps",
		State:     "running",
		Origin:    core.EngineOriginStandalone,
		Family:    "polystrata",
		ModelName: psMod.Name,
		Port:      18080,
		Endpoint:  "http://127.0.0.1:18080",
	}
	s.engine = "llama-cpp-default"
	s.model = "Qwen3-8B-Q4.gguf"
	s.task = benchmarkTask{
		stage:   "bind",
		failure: core.Err("cli_error"),
		start:   startChainResult{localCandidates: []core.CandidateRun{{Bundle: "zh-core"}}},
	}
	s.probeServiceConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExclusive, Port: 18080}
	}
	s.startBenchmarkForManagedService(adoptedSvc)
	if s.selected != "benchmarks" || !s.benchFlowOpen {
		t.Fatalf("expected benchmarks flow open, got selected=%q open=%v", s.selected, s.benchFlowOpen)
	}
	if s.model != psMod.Name || s.engine != psEng.Name || s.task.stage != "prepare_candidate" {
		t.Fatalf("expected service target (%q, %q, prepare_candidate), got (%q, %q, %q)", psMod.Name, psEng.Name, s.model, s.engine, s.task.stage)
	}
	if s.task.failure != nil || len(s.task.start.localCandidates) != 0 {
		t.Fatalf("expected stale round A failure and candidates cleared, got task=%+v", s.task)
	}

	// Discovered service reporting unmatched /v1/models alias clears stale s.model so Model A cannot run by mistake.
	s.selected = "services"
	s.benchFlowOpen = false
	s.model = "Qwen3-8B-Q4.gguf"
	s.probeEndpointConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExternal, Port: 11434, Clients: []string{"Client (PID 7)"}}
	}
	s.startBenchmarkForDiscoveredService(
		inferyard.DiscoveredService{PID: 42, ProcessName: "ollama"},
		inferyard.DiscoveredEndpoint{URL: "http://127.0.0.1:11434", Models: []string{"served-alias-not-in-registry"}},
	)
	extTester := newServicesTester(s)
	extTester.Frame()
	click(t, extTester, l10n.T(l10n.LanguageZhHans, "services.ext_check.action.benchmark_anyway"))
	extTester.Frame()
	if s.model != "" {
		t.Fatalf("expected unmatched discovered service model to clear stale selection, got %q", s.model)
	}
	if s.pendingExclusivityModel != "served-alias-not-in-registry" {
		t.Fatalf("expected pendingExclusivityModel to record discovered service model id, got %q", s.pendingExclusivityModel)
	}

	// Empty model registry jumping to discovered service B, followed by full refresh discovering only model A,
	// must keep s.model empty rather than auto-adopting A.
	sEmpty, _ := servicesP2State(t)
	sEmpty.registry.Models = nil
	sEmpty.probeEndpointConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{Status: core.ExclusivityExclusive, Port: 11434}
	}
	sEmpty.startBenchmarkForDiscoveredService(
		inferyard.DiscoveredService{PID: 99, ProcessName: "ollama"},
		inferyard.DiscoveredEndpoint{URL: "http://127.0.0.1:11434", Models: []string{"model-B"}},
	)
	if !sEmpty.applyRefreshBatch(refreshBatch{
		epoch:    sEmpty.settingsSequence(),
		full:     true,
		settings: sEmpty.settings,
		registry: core.Registry{
			Engines: s.registry.Engines[:1],
			Models:  []core.ModelAsset{{Name: "Model-A.gguf", Format: "gguf", Path: "/models/Model-A.gguf", Available: true}},
		},
	}) {
		t.Fatal("expected full refresh batch to apply")
	}
	if sEmpty.model != "" {
		t.Fatalf("expected empty model selection after refresh discovered only Model-A, got %q", sEmpty.model)
	}

	// When s.engine is empty and all registered engines are ineligible, surface reason instead of generic no_engines.
	sInel := testState()
	sInel.loaded = true
	sInel.selected = "benchmarks"
	sInel.benchFlowOpen = true
	sInel.ready = core.Readiness{
		CLI:       core.InferyardProbe{Status: "ok"},
		Workspace: core.DirectoryReadiness{Initialized: true, Exists: true},
		Results:   core.DirectoryReadiness{Exists: true},
		Models:    core.DirectoryReadiness{Exists: true},
	}
	sInel.progress = &inferyard.WorkspaceProgress{
		Workspace:       "/tmp/ws",
		NextAction:      "prepare_candidate",
		DevicePreflight: inferyard.ProgressPreflight{Status: "missing"},
		Bundles:         []inferyard.BundleProgress{{Bundle: inferyard.DefaultBundlePelican, Phase: "candidate_missing", NextAction: "prepare_candidate"}},
	}
	sInel.registry = core.Registry{
		Engines: []core.EngineAsset{psEng},
		Models:  []core.ModelAsset{psMod},
	}
	sInel.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			psEng.Name: {State: core.EngineCheckIneligible, Family: "polystrata", Reason: "local_engine_polystrata_engine_missing"},
		},
	}
	sInel.model = psMod.Name
	tt := ui.NewTester(sInel.benchmarksView, 960, 720)
	tt.Frame()
	inelTexts := strings.Join(tt.Texts(), "\n")
	noEnginesCopy := l10n.T(l10n.LanguageZhHans, "dashboard.candidate.no_engines")
	viewEnginesBtn := l10n.T(l10n.LanguageZhHans, "dashboard.candidate.view_engines")
	if strings.Contains(inelTexts, noEnginesCopy) {
		t.Fatalf("expected ineligible engine reason instead of %q, got:\n%s", noEnginesCopy, inelTexts)
	}
	if !strings.Contains(inelTexts, psEng.Name) || !strings.Contains(inelTexts, viewEnginesBtn) {
		t.Fatalf("expected ineligible engine %q and %q button surfaced, got:\n%s", psEng.Name, viewEnginesBtn, inelTexts)
	}
}
