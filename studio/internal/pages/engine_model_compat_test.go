package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestMlxLMStandalonePairingAndContextReuse(t *testing.T) {
	dirModel := core.ModelAsset{Name: "Qwen3-4B-MLX", Path: "/models/Qwen3-4B-MLX", Format: "directory", Available: true}
	ggufModel := core.ModelAsset{Name: "Qwen3-4B.gguf", Path: "/models/Qwen3-4B.gguf", Format: "gguf", Available: true}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx-lm-0.32.2/installation.json"}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama-prism/llama-server"}

	s := readyState("library")
	s.serveSupported = true
	s.engine = mlxEng.Name
	s.registry = core.Registry{
		Models:  []core.ModelAsset{dirModel, ggufModel},
		Engines: []core.EngineAsset{llamaEng, mlxEng},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			llamaEng.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			mlxEng.Name:   {State: core.EngineCheckEligible, Family: "mlx-lm"},
		},
	}

	opts := s.standaloneEngineOptions(dirModel)
	if len(opts) != 1 || opts[0].Value != mlxEng.Name {
		t.Fatalf("expected mlx-lm standalone option for directory model, got %+v", opts)
	}
	s.openStandaloneDrawer(dirModel, false)
	if !s.standalone.drawerOpen || s.standalone.engineName != mlxEng.Name {
		t.Fatalf("expected standalone drawer to select %q, got %+v", mlxEng.Name, s.standalone)
	}

	// Upstream mlx_lm.server standalone service is not directly reused for formal benchmarks; it triggers handoff instead.
	s.managedServices = []core.EngineState{{
		ServiceID:   core.ServiceIDFor(dirModel.Name, mlxEng.Name),
		State:       "running",
		Origin:      core.EngineOriginStandalone,
		Family:      "mlx-lm",
		EngineName:  mlxEng.Name,
		ModelName:   dirModel.Name,
		Model:       dirModel.Path,
		Pid:         4242,
		Port:        18080,
		ContextSize: 4096,
	}}
	if _, ok := s.reusableStandaloneService(dirModel.Name, mlxEng.Name); ok {
		t.Fatal("expected mlx-lm standalone service to require handoff rather than direct benchmark reuse")
	}
	if !s.standaloneRunningForBenchmarkConflict() {
		t.Fatal("expected active mlx-lm standalone service to trigger benchmark conflict handoff")
	}

	// Without a compatible engine for strata_pack, model row Start button disables cleanly with no_engine tooltip.
	strataModel := core.ModelAsset{Name: "pack-only", Format: "strata_pack", Available: true}
	tt := ui.NewTester(func(c *ui.Context) {
		s.renderModelStandalonePrimaryAction(c, strataModel)
	}, 800, 200)
	tt.Frame()
	startLabel := l10n.TR("models.serve.action.start") + " · " + strataModel.Name
	btn, found := tt.AccessByRole(ui.RoleButton, startLabel)
	if !found || !btn.Disabled || btn.Description != l10n.TR("models.serve.drawer.no_engine") {
		t.Fatalf("expected incompatible model start button disabled with no_engine reason, got found=%v btn=%+v", found, btn)
	}
}

func hasTextSub(texts []string, sub string) bool {
	for _, t := range texts {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

func TestModelRowBenchmarkAndAdoptReconcilesEngine(t *testing.T) {
	dirModel := core.ModelAsset{Name: "Qwen3-MLX-Dir", Path: "/models/Qwen3-MLX-Dir", Format: "directory", Available: true}
	ggufModel := core.ModelAsset{Name: "Qwen3.gguf", Path: "/models/Qwen3.gguf", Format: "gguf", Available: true}
	strataModel := core.ModelAsset{Name: "unmatched-pack", Path: "/models/pack", Format: "strata_pack", Available: true}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama/llama-server"}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx/installation.json"}

	s := readyState("library")
	s.engine = llamaEng.Name
	s.registry = core.Registry{
		Models:        []core.ModelAsset{ggufModel, dirModel, strataModel},
		Engines:       []core.EngineAsset{llamaEng, mlxEng},
		DefaultEngine: llamaEng.Name,
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			llamaEng.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			mlxEng.Name:   {State: core.EngineCheckEligible, Family: "mlx-lm"},
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		modelTaskAction(c, s, dirModel)
		modelTaskAction(c, s, strataModel)
	}, 800, 200)
	tt.Frame()
	benchLabel := l10n.TR("models.action.benchmark_with_model") + " · " + dirModel.Name
	click(t, tt, benchLabel)
	if s.model != dirModel.Name || s.engine != mlxEng.Name {
		t.Fatalf("clicking benchmark on directory model must switch engine to %q, got model=%q engine=%q", mlxEng.Name, s.model, s.engine)
	}

	if got := s.resolveCompatibleEngineForModel(dirModel.Name); got != mlxEng.Name {
		t.Fatalf("resolveCompatibleEngineForModel(%q) = %q, want %q", dirModel.Name, got, mlxEng.Name)
	}
	if got := s.resolveCompatibleEngineForModel(strataModel.Name); got != "" {
		t.Fatalf("resolveCompatibleEngineForModel(%q) without polystrata = %q, want empty", strataModel.Name, got)
	}

	strataBtn := l10n.TR("models.action.benchmark_with_model") + " · " + strataModel.Name
	click(t, tt, strataBtn)
	if s.engine != "" || s.model != "" {
		t.Fatalf("clicking benchmark on incompatible strata_pack without matching engine must reset pair, got engine=%q model=%q", s.engine, s.model)
	}
}

func TestCandidateFieldsEngineFirstAndCompatibleModelFiltering(t *testing.T) {
	s := readyState("benchmarks")
	preflight := "/tmp/device-preflight.json"
	s.progress = &inferyard.WorkspaceProgress{
		Workspace:       "/tmp/ws",
		NextAction:      "prepare_candidate",
		DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: &preflight},
		Bundles:         []inferyard.BundleProgress{{Bundle: "zh-core"}},
	}
	ggufModel := core.ModelAsset{Name: "qwen.gguf", Path: "/models/qwen.gguf", Format: "gguf", Available: true}
	ggufModel2 := core.ModelAsset{Name: "llama.gguf", Path: "/models/llama.gguf", Format: "gguf", Available: true}
	dirModel := core.ModelAsset{Name: "qwen-mlx", Path: "/models/qwen-mlx", Format: "directory", Available: true}
	strataModel := core.ModelAsset{Name: "unmatched-pack", Path: "/models/pack", Format: "strata_pack", Available: true}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama/llama-server"}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx/installation.json"}
	vllmEng := core.EngineAsset{Name: "vllm-metal", Family: "vllm", Path: "/ws/engines/vllm/vllm"}

	s.registry = core.Registry{
		Models:  []core.ModelAsset{ggufModel, ggufModel2, dirModel, strataModel},
		Engines: []core.EngineAsset{llamaEng, mlxEng, vllmEng},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			llamaEng.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			mlxEng.Name:   {State: core.EngineCheckEligible, Family: "mlx-lm"},
			vllmEng.Name:  {State: core.EngineCheckEligible, Family: "vllm"},
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		s.candidateFields(c)
	}, 1024, 900)

	// 1. Blank engine clears dangling model and disables Model dropdown with select_engine_first.
	s.engine = ""
	s.model = dirModel.Name
	tt.Frame()
	if s.model != "" {
		t.Fatalf("blank engine must clear dangling model, got %q", s.model)
	}
	texts := tt.Texts()
	if !hasTextSub(texts, l10n.TR("dashboard.candidate.select_engine")) {
		t.Fatalf("expected %q placeholder when engine blank, got %v", l10n.TR("dashboard.candidate.select_engine"), texts)
	}
	if !hasTextSub(texts, l10n.TR("dashboard.candidate.select_engine_first")) {
		t.Fatalf("expected %q placeholder when engine blank, got %v", l10n.TR("dashboard.candidate.select_engine_first"), texts)
	}
	if got := s.speedStartBlockReason(); got != l10n.TR("speed.block.no_engine") {
		t.Fatalf("speedStartBlockReason() with blank engine = %q, want %q", got, l10n.TR("speed.block.no_engine"))
	}

	// 2. Selecting mlx-lm with 1 compatible directory model unlocks & auto-selects dirModel.
	s.engine = mlxEng.Name
	tt.Frame()
	if s.model != dirModel.Name {
		t.Fatalf("selecting %q with single compatible model should auto-select %q, got %q", mlxEng.Name, dirModel.Name, s.model)
	}
	if errMsg := s.localSelectionError(); errMsg != "" {
		t.Fatalf("expected empty localSelectionError after valid selection, got %q", errMsg)
	}

	// 3. Switching to llama-cpp (2 compatible GGUF models) resets incompatible model to "" ("请选择模型")
	// and Model menu lists only compatible GGUF models.
	s.engine = llamaEng.Name
	tt.Frame()
	if s.model != "" {
		t.Fatalf("switching to engine with 2 compatible models must reset model, got %q", s.model)
	}
	if !hasTextSub(tt.Texts(), l10n.TR("dashboard.candidate.select_model")) {
		t.Fatalf("expected %q placeholder with multiple compatible models, got %v", l10n.TR("dashboard.candidate.select_model"), tt.Texts())
	}
	compat := s.compatibleModelsForSelectedEngine()
	if len(compat) != 2 || compat[0].Name != ggufModel.Name || compat[1].Name != ggufModel2.Name {
		t.Fatalf("compatibleModelsForSelectedEngine() for llama-cpp = %+v, want [qwen.gguf llama.gguf]", compat)
	}
	tt.Click(l10n.TR("dashboard.candidate.model"))
	menuItems := tt.Menu()
	if !hasTextSub(menuItems, ggufModel.Name) || !hasTextSub(menuItems, ggufModel2.Name) ||
		hasTextSub(menuItems, dirModel.Name) || hasTextSub(menuItems, strataModel.Name) {
		t.Fatalf("Model menu under llama-cpp should contain only GGUF models, got %v", menuItems)
	}
	tt.ChooseMenuItem(modelLabel(ggufModel))
	tt.Frame()
	if s.model != ggufModel.Name {
		t.Fatalf("choosing %q from Model menu set s.model=%q", ggufModel.Name, s.model)
	}

	// Check format_mismatch detection on speedStartBlockReason before render reconciliation.
	s.model = dirModel.Name
	if got := s.speedStartBlockReason(); got != l10n.TR("engine.local.error.format_mismatch") {
		t.Fatalf("speedStartBlockReason() on format mismatch = %q, want %q", got, l10n.TR("engine.local.error.format_mismatch"))
	}

	// 4. Switching to engine with 0 compatible models clears s.model and disables Model dropdown with hint.
	s.registry.Models = []core.ModelAsset{ggufModel, ggufModel2, strataModel}
	s.model = ggufModel.Name
	s.engine = vllmEng.Name
	tt.Frame()
	if s.model != "" {
		t.Fatalf("switching to engine with 0 compatible models must clear model, got %q", s.model)
	}
	vllmTexts := tt.Texts()
	if !hasTextSub(vllmTexts, l10n.TR("dashboard.candidate.no_compatible_models")) ||
		!hasTextSub(vllmTexts, l10n.TR("dashboard.candidate.no_compatible_models_hint")) {
		t.Fatalf("expected no_compatible_models and hint for vllm, got %v", vllmTexts)
	}
	if got := s.speedStartBlockReason(); got != l10n.TR("dashboard.candidate.no_compatible_models_hint") {
		t.Fatalf("speedStartBlockReason() with 0 compatible models = %q, want %q", got, l10n.TR("dashboard.candidate.no_compatible_models_hint"))
	}
	s.registry.Models = []core.ModelAsset{ggufModel, ggufModel2, dirModel, strataModel}

	// 5. Switching Engine back to "" clears model and re-disables Model dropdown; newBenchmark clears both.
	s.engine = llamaEng.Name
	s.model = ggufModel.Name
	tt.Frame()
	s.engine = ""
	tt.Frame()
	if s.model != "" || !hasTextSub(tt.Texts(), l10n.TR("dashboard.candidate.select_engine_first")) {
		t.Fatalf("switching engine back to empty must clear model (%q) and disable model dropdown", s.model)
	}

	s.engine = mlxEng.Name
	s.model = dirModel.Name
	s.newBenchmark()
	if s.engine != "" || s.model != "" {
		t.Fatalf("newBenchmark() must clear engine & model, got engine=%q model=%q", s.engine, s.model)
	}

	// 6. startBenchmark rejects blank, ineligible, or model-incompatible engine.
	s.task = benchmarkTask{}
	s.selectedBundles = map[string]bool{"zh-core": true}
	s.engine = ""
	s.model = ggufModel.Name
	s.startBenchmark()
	if s.task.stage != "" || s.busy {
		t.Fatalf("startBenchmark() with blank engine must not start task, got stage=%q busy=%v", s.task.stage, s.busy)
	}
	s.engine = "missing-engine"
	s.startBenchmark()
	if s.task.stage != "" || s.busy {
		t.Fatalf("startBenchmark() with ineligible engine must not start task, got stage=%q busy=%v", s.task.stage, s.busy)
	}
	s.engine = llamaEng.Name
	s.model = dirModel.Name
	s.startBenchmark()
	if s.task.stage != "" || s.busy {
		t.Fatalf("startBenchmark() with incompatible (engine, model) must not start task, got stage=%q busy=%v", s.task.stage, s.busy)
	}
}

func TestDownloadsEnginesFiltersInstalledAndDrawerPreventsDuplicateInstall(t *testing.T) {
	st := testState()
	s := &st
	s.settings.Effective.WorkspaceRoot = ptr("/ws")
	uiState := s.ensureDownloadsUI()
	uiState.hostPlatform = "darwin/arm64"

	s.registry.Engines = []core.EngineAsset{
		{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Version: "0.32.2"},
		{Name: "llama-cpp-prism · llama-server", Family: "llama-cpp", Version: "prism-b10743-adfffbe"},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		s.downloadsEnginesTab(c)
		s.downloadsEngineDrawer(c)
	}, 1280, 1200)
	tt.Frame()

	// Installed mlx-lm and llama-cpp must be filtered out of Downloadable Engines.
	if _, ok := tt.AccessByRole(ui.RoleButton, "安装 · mlx-lm"); ok {
		t.Fatal("installed mlx-lm must not appear in downloadable engines list")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "安装 · llama-cpp"); ok {
		t.Fatal("installed llama-cpp must not appear in downloadable engines list")
	}
	// Uninstalled vllm and polystrata remain available.
	if _, ok := tt.AccessByRole(ui.RoleButton, "安装 · vllm"); !ok {
		t.Fatal("uninstalled vllm should remain in downloadable engines list")
	}

	// Managed engine install task with Kind=="engine" and RequestedSourceUrl=="vllm:..." is detected as downloading.
	vllmFam, ok := s.engineCatalogRegistry().FamilyByID("vllm")
	if !ok {
		t.Fatal("missing vllm family")
	}
	s.downloads = []core.DownloadTask{{
		TaskId:             "dl-vllm",
		Kind:               "engine",
		State:              "downloading",
		RequestedSourceUrl: "vllm:vllm-0.31.0:engine-vllm-macos-arm64",
		OutDir:             "/ws/engines/vllm-0.31.0",
	}}
	if !s.isEngineFamilyDownloading(vllmFam) {
		t.Fatal("expected isEngineFamilyDownloading to match Kind=engine managed install task")
	}
	s.downloads = nil

	// Opening detail drawer for installed mlx-lm disables duplicate install button.
	uiState.openEngineDrawer("mlx-lm", "mlx-lm-0.32.2", "darwin/arm64", "engine-mlx-lm-macos-arm64")
	tt.Frame()
	if btn, ok := tt.AccessByRole(ui.RoleButton, "安装此版本 · mlx-lm"); !ok || !btn.Disabled {
		t.Fatalf("expected drawer install button disabled for installed mlx-lm 0.32.2, got ok=%v btn=%+v", ok, btn)
	}

	// Opening detail drawer for installed llama-cpp prism-b10743-adfffbe disables duplicate download button.
	uiState.openEngineDrawer("llama-cpp", "llama-cpp-prism-b10743", "darwin/arm64", "engine-macos-arm64")
	tt.Frame()
	if btn, ok := tt.AccessByRole(ui.RoleButton, "安装此版本 · llama-cpp"); !ok || !btn.Disabled {
		t.Fatalf("expected drawer install button disabled for installed llama-cpp prism-b10743-adfffbe, got ok=%v btn=%+v", ok, btn)
	}
	uiState.engineDrawerOpen = false

	// When all supported managed catalog families on darwin/arm64 are installed, show all_installed message.
	s.registry.Engines = append(s.registry.Engines,
		core.EngineAsset{Name: "PolyStrata · v0.2.20-poly.5", Family: "polystrata", Version: "v0.2.20-poly.5"},
		core.EngineAsset{Name: "vLLM · v0.31.0", Family: "vllm", Version: "v0.31.0"},
	)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, l10n.TR("downloads.engines.all_installed")) {
		t.Fatalf("expected all_installed copy when all managed catalog families are installed, got:\n%s", texts)
	}

	// Linked older mlx-lm (0.31.0) must still allow installing catalog 0.32.2 in the drawer.
	s.registry.Engines[0] = core.EngineAsset{Name: "mlx-lm · 0.31.0", Family: "mlx-lm", Kind: "local_installation", Version: "0.31.0"}
	uiState.openEngineDrawer("mlx-lm", "mlx-lm-0.32.2", "darwin/arm64", "engine-mlx-lm-macos-arm64")
	tt.Frame()
	if btn, ok := tt.AccessByRole(ui.RoleButton, "安装此版本 · mlx-lm"); !ok || btn.Disabled {
		t.Fatalf("expected drawer install button enabled when installed mlx-lm is older 0.31.0, got ok=%v btn=%+v", ok, btn)
	}
}

func TestMLXStandaloneServiceExcludedFromBenchmarkReuse(t *testing.T) {
	s := &viewState{
		engine: "mlx-lm · 0.32.2",
		registry: core.Registry{
			Models: []core.ModelAsset{
				{Name: "Qwen3-0.6B-4bit", Format: "directory", Path: "/models/Qwen3-0.6B-4bit", Available: true},
			},
			Engines: []core.EngineAsset{
				{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation"},
			},
		},
		managedServices: []core.EngineState{{
			Origin:      core.EngineOriginStandalone,
			State:       "running",
			Pid:         4242,
			Port:        48857,
			ContextSize: 8192,
			ModelName:   "Qwen3-0.6B-4bit",
			EngineName:  "mlx-lm · 0.32.2",
			Family:      "mlx-lm",
			Endpoint:    "http://127.0.0.1:48857",
		}},
	}
	if _, ok := s.reusableStandaloneService("Qwen3-0.6B-4bit", "mlx-lm · 0.32.2"); ok {
		t.Fatal("mlx-lm standalone server (mlx_lm.server) must not be reused directly for formal benchmark runs")
	}
}

func TestExplicitHandoffsAndHistoryRerunValidation(t *testing.T) {
	s := readyState("history")
	fooModel := core.ModelAsset{Name: "foo.gguf", Path: "/models/foo.gguf", Format: "gguf", Available: true}
	mlxModel := core.ModelAsset{Name: "qwen-mlx", Path: "/models/qwen-mlx", Format: "directory", Available: true}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama/llama-server"}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx/installation.json"}
	vllmInel := core.EngineAsset{Name: "vllm-bad", Family: "vllm", Path: "/ws/engines/vllm/vllm"}

	s.registry = core.Registry{
		Models:  []core.ModelAsset{fooModel, mlxModel},
		Engines: []core.EngineAsset{llamaEng, mlxEng, vllmInel},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			llamaEng.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			mlxEng.Name:   {State: core.EngineCheckEligible, Family: "mlx-lm"},
			vllmInel.Name: {State: core.EngineCheckIneligible, Family: "vllm", Reason: "missing_dep"},
		},
	}

	// 1. History rerun resolves ModelPath + CLI adapter, EngineFamily disambiguation, and PolyStrata packs.
	fooDisplay, fooPath, prismAdapter := "foo", "/models/foo.gguf", "prism_llama_server_v1"
	eng, mod := s.resolveHistoryRerunSelection(core.RunSummary{ModelName: &fooDisplay, ModelPath: &fooPath, Engine: &prismAdapter})
	if eng != llamaEng.Name || mod != fooModel.Name {
		t.Fatalf("history rerun (foo/prism) got (%q, %q), want (%q, %q)", eng, mod, llamaEng.Name, fooModel.Name)
	}
	mlxDisplay, mlxPath, localAdapter, mlxFam := "Qwen3 MLX", "/models/qwen-mlx", "local_openai_v1", "mlx-lm"
	vllmElig := core.EngineAsset{Name: "vllm-good", Family: "vllm", Path: "/ws/engines/vllm/vllm-good"}
	s.registry.Engines = append(s.registry.Engines, vllmElig)
	s.checks.Engines[vllmElig.Name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "vllm"}
	s.engine = vllmElig.Name // must NOT leak into mlx-lm or ambiguous legacy rerun
	eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ModelName: &mlxDisplay, ModelPath: &mlxPath, Engine: &localAdapter, EngineFamily: &mlxFam})
	if eng != mlxEng.Name || mod != mlxModel.Name {
		t.Fatalf("history rerun (mlx/EngineFamily) got (%q, %q), want (%q, %q)", eng, mod, mlxEng.Name, mlxModel.Name)
	}
	eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ModelName: &mlxDisplay, ModelPath: &mlxPath, Engine: &localAdapter})
	if eng != "" || mod != "" {
		t.Fatalf("legacy local_openai_v1 rerun with 2 compatible engines must stay empty, got (%q, %q)", eng, mod)
	}
	strataMod := core.ModelAsset{Name: "qwen3.8-flash-next-iq2_xs", Format: "strata_pack", Path: "/ws/data/strata-iq2_xs.json", CoveredPaths: []string{"/tmp/packs/iq2_xs/weights.bin"}, Available: true}
	polyEng := core.EngineAsset{Name: "PolyStrata · v0.2.20", Family: "polystrata", Kind: "local_installation", Path: "/ws/engines/poly/installation.json"}
	s.registry.Models = append(s.registry.Models, strataMod)
	s.registry.Engines = append(s.registry.Engines, polyEng)
	s.checks.Engines[polyEng.Name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "polystrata"}
	packDisp, packPath, polyFam, servedName := "iq2_xs", "/tmp/packs/iq2_xs", "polystrata", "qwen3.8-flash-next-iq2_xs"
	eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ModelName: &packDisp, ModelPath: &packPath, Engine: &localAdapter, EngineFamily: &polyFam})
	if eng != polyEng.Name || mod != strataMod.Name {
		t.Fatalf("polystrata pack rerun via CoveredPaths got (%q, %q), want (%q, %q)", eng, mod, polyEng.Name, strataMod.Name)
	}
	eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ServedModel: &servedName, EngineFamily: &polyFam})
	if eng != polyEng.Name || mod != strataMod.Name {
		t.Fatalf("polystrata pack rerun via ServedModel got (%q, %q), want (%q, %q)", eng, mod, polyEng.Name, strataMod.Name)
	}
	polyEng2 := core.EngineAsset{Name: "PolyStrata · v0.2.21", Family: "polystrata", Kind: "local_installation", Path: "/ws/engines/poly2/installation.json"}
	s.registry.Engines = append(s.registry.Engines, polyEng2)
	s.checks.Engines[polyEng2.Name] = core.EngineEligibility{State: core.EngineCheckEligible, Family: "polystrata"}
	if eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ServedModel: &servedName, EngineFamily: &polyFam}); eng != "" || mod != "" {
		t.Fatalf("EngineFamily with 2 usable polystrata installs must stay empty, got (%q, %q)", eng, mod)
	}
	s.registry.Engines = []core.EngineAsset{llamaEng, vllmElig}
	if eng, mod = s.resolveHistoryRerunSelection(core.RunSummary{ModelPath: &mlxPath, Engine: &localAdapter, EngineFamily: &mlxFam}); eng != "" || mod != "" {
		t.Fatalf("missing EngineFamily must not fall back to vllm, got (%q, %q)", eng, mod)
	}
	s.registry.Models = []core.ModelAsset{fooModel, mlxModel}
	s.registry.Engines = []core.EngineAsset{llamaEng, mlxEng, vllmInel}

	// 2. applyPolystrataSelection atomically clears stale pair when no PolyStrata engine exists.
	s.engine, s.model = llamaEng.Name, fooModel.Name
	engOK, modOK := s.applyPolystrataSelection("/missing/installation.json", "/missing/strata.json")
	if engOK || modOK || s.engine != "" || s.model != "" {
		t.Fatalf("applyPolystrataSelection without PolyStrata must clear stale selection, got engOK=%v modOK=%v (%q, %q)", engOK, modOK, s.engine, s.model)
	}

	// 3. applyServiceBenchmarkTarget rejects ineligible engine and leaves pair empty.
	s.engine, s.model = llamaEng.Name, fooModel.Name
	s.applyServiceBenchmarkTarget(mlxModel.Name, vllmInel.Name)
	if s.engine != "" || s.model != "" {
		t.Fatalf("applyServiceBenchmarkTarget with ineligible engine must clear pair, got (%q, %q)", s.engine, s.model)
	}

	// 4. newSpeedTask rejects ineligible engine or incompatible model without mutating s.engine/s.model.
	s.engine, s.model, s.failure = llamaEng.Name, fooModel.Name, nil
	s.newSpeedTask(&speedTask{selectionEngine: vllmInel.Name, selectionModel: mlxModel.Name, context: inferyard.ContextSpeed64K})
	if s.failure == nil || s.failure.Code != "context_speed_assets_unavailable" || s.engine != llamaEng.Name || s.model != fooModel.Name {
		t.Fatalf("newSpeedTask with ineligible engine failed expectation: err=%+v (%q, %q)", s.failure, s.engine, s.model)
	}
	s.failure = nil
	s.newSpeedTask(&speedTask{selectionEngine: llamaEng.Name, selectionModel: mlxModel.Name, context: inferyard.ContextSpeed64K})
	if s.failure == nil || s.failure.Code != "context_speed_assets_unavailable" || s.engine != llamaEng.Name || s.model != fooModel.Name {
		t.Fatalf("newSpeedTask with incompatible model failed expectation: err=%+v (%q, %q)", s.failure, s.engine, s.model)
	}
}
