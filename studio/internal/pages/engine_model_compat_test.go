package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
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

func TestModelRowBenchmarkAndAdoptReconcilesEngine(t *testing.T) {
	dirModel := core.ModelAsset{Name: "Qwen3-MLX-Dir", Path: "/models/Qwen3-MLX-Dir", Format: "directory", Available: true}
	ggufModel := core.ModelAsset{Name: "Qwen3.gguf", Path: "/models/Qwen3.gguf", Format: "gguf", Available: true}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama/llama-server"}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx/installation.json"}

	s := readyState("library")
	s.engine = llamaEng.Name
	s.registry = core.Registry{
		Models:        []core.ModelAsset{ggufModel, dirModel},
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
	}, 800, 200)
	tt.Frame()
	benchLabel := l10n.TR("models.action.benchmark_with_model") + " · " + dirModel.Name
	click(t, tt, benchLabel)
	if s.model != dirModel.Name || s.engine != mlxEng.Name {
		t.Fatalf("clicking benchmark on directory model must switch engine to %q, got model=%q engine=%q", mlxEng.Name, s.model, s.engine)
	}

	// adoptDownloadedEngine prefers engine compatible with s.model when s.engine is empty.
	s.engine = ""
	s.model = dirModel.Name
	s.adoptDownloadedEngine()
	if s.engine != mlxEng.Name {
		t.Fatalf("adoptDownloadedEngine with directory s.model got %q, want %q", s.engine, mlxEng.Name)
	}
}

func TestCandidateFieldsUnlocksModelDropdownAndSyncsEngine(t *testing.T) {
	st := testState()
	s := &st
	ggufModel := core.ModelAsset{Name: "qwen.gguf", Path: "/models/qwen.gguf", Format: "gguf", Available: true}
	dirModel := core.ModelAsset{Name: "qwen-mlx", Path: "/models/qwen-mlx", Format: "directory", Available: true}
	strataModel := core.ModelAsset{Name: "unmatched-pack", Path: "/models/pack", Format: "strata_pack", Available: true}
	llamaEng := core.EngineAsset{Name: "llama-prism", Family: "llama-cpp", Path: "/ws/engines/llama/llama-server"}
	mlxEng := core.EngineAsset{Name: "mlx-lm · 0.32.2", Family: "mlx-lm", Kind: "local_installation", Path: "/ws/engines/mlx/installation.json"}

	s.engine = llamaEng.Name
	s.model = ggufModel.Name
	s.registry = core.Registry{
		Models:  []core.ModelAsset{ggufModel, dirModel, strataModel},
		Engines: []core.EngineAsset{llamaEng, mlxEng},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			llamaEng.Name: {State: core.EngineCheckEligible, Family: "llama-cpp"},
			mlxEng.Name:   {State: core.EngineCheckEligible, Family: "mlx-lm"},
		},
	}

	if got := s.candidateModelSuffix(dirModel); got != "" {
		t.Fatalf("directory model with eligible mlx-lm should have no incompatible suffix, got %q", got)
	}
	if got := s.candidateModelSuffix(strataModel); got == "" {
		t.Fatal("strata_pack model without eligible polystrata should retain incompatible suffix")
	}

	tt := ui.NewTester(func(c *ui.Context) {
		s.candidateFields(c)
	}, 1024, 900)
	tt.Frame()

	// Switching model to directory model automatically reconciles engine to mlx-lm without format_mismatch error.
	s.model = dirModel.Name
	tt.Frame()
	if s.engine != mlxEng.Name {
		t.Fatalf("switching candidate model to %q should reconcile engine to %q, got %q", dirModel.Name, mlxEng.Name, s.engine)
	}
	if errMsg := s.localSelectionError(); errMsg != "" {
		t.Fatalf("expected empty localSelectionError after engine auto-reconcile, got %q", errMsg)
	}

	// Manually switching engine back to llama-cpp when only 1 compatible GGUF model exists auto-switches model.
	s.engine = llamaEng.Name
	tt.Frame()
	if s.model != ggufModel.Name {
		t.Fatalf("switching engine to %q with single compatible GGUF model should select %q, got %q", llamaEng.Name, ggufModel.Name, s.model)
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
