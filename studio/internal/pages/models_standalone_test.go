package pages

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func int64Ptr(v int64) *int64 { return &v }

func TestModelRowActionReadiness(t *testing.T) {
	s := readyState("library")
	model := core.ModelAsset{Name: "m1.gguf", Format: "gguf", Available: true}
	s.registry.Engines = []core.EngineAsset{{Name: "llama-ok", Family: "llama-cpp"}}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines:   map[string]core.EngineEligibility{"llama-ok": {State: core.EngineCheckEligible}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		s.renderModelStandalonePrimaryAction(c, model)
		modelTaskAction(c, s, model)
	}, 1024, 600)
	check := func(action, reason string) {
		t.Helper()
		tt.Frame()
		name := l10n.TR(action) + " · " + model.Name
		b, ok := tt.AccessByRole(ui.RoleButton, name)
		var want string
		if reason != "" {
			want = l10n.TR(reason)
		}
		if !ok || b.Disabled != (reason != "") || b.Description != want {
			t.Fatalf("%s: found=%v disabled=%v description=%q, want %q", name, ok, b.Disabled, b.Description, want)
		}
	}
	const start, bench = "models.serve.action.start", "models.action.benchmark_with_model"
	s.ready.CLI.Status = "not_found"
	check(start, "models.serve.blocked.env_missing")
	check(bench, "shell.nav.new_task_hint.env_missing")
	s.ready.CLI.Status = "outdated"
	check(start, "models.serve.blocked.cli_outdated")
	s.ready.Workspace.Initialized = false
	check(start, "models.serve.blocked.env_missing")
	s.ready.CLI.Status = "ok"
	check(bench, "shell.nav.new_task_hint.env_missing")
	s.ready.Workspace.Initialized = true
	check(start, "models.serve.blocked.cli_outdated")
	s.serveProbing = true
	check(start, "models.serve.blocked.probing")
	check(bench, "")
	s.serveSupported, s.serveProbing, s.busy = true, false, true
	check(start, "models.action.blocked.busy")
	check(bench, "models.action.blocked.busy")
	s.busy = false
	check(start, "")
	check(bench, "")
	s.registry.Engines = nil
	check(start, "models.serve.drawer.no_engine")
	check(bench, "")
}

func TestStandaloneEngineFilteringAndDefaults(t *testing.T) {
	s := &viewState{
		engine: "llama-eligible",
		registry: core.Registry{
			Engines: []core.EngineAsset{
				{Name: "llama-eligible", Family: "llama-cpp"},
				{Name: "llama-ineligible", Family: "llama-cpp"},
				{Name: "lms", Family: "lmstudio"},
				{Name: "mlx", Family: "mlx-lm"},
				{Name: "vllm-ok", Family: "vllm"},
				{Name: "ps-ok", Family: "polystrata"},
			},
		},
		checks: core.EngineCheckOverview{
			Supported: true,
			Engines: map[string]core.EngineEligibility{
				"llama-eligible":   {State: core.EngineCheckEligible},
				"llama-ineligible": {State: core.EngineCheckIneligible},
				"lms":              {State: core.EngineCheckEligible},
				"mlx":              {State: core.EngineCheckEligible},
				"vllm-ok":          {State: core.EngineCheckEligible},
				"ps-ok":            {State: core.EngineCheckEligible},
			},
		},
	}

	gguf := core.ModelAsset{Name: "qwen.gguf", Format: "gguf", Architecture: "qwen35moe", Available: true}
	opts := s.standaloneEngineOptions(gguf)
	if len(opts) != 2 || opts[0].Value != "llama-eligible" || opts[1].Value != "ps-ok" {
		t.Fatalf("unexpected GGUF engine options: %+v", opts)
	}
	if def := s.defaultStandaloneEngine(gguf, opts); def != "llama-eligible" {
		t.Fatalf("default engine=%q want llama-eligible", def)
	}

	dirModel := core.ModelAsset{Name: "hf-dir", Format: "directory", Available: true}
	dirOpts := s.standaloneEngineOptions(dirModel)
	if len(dirOpts) != 2 || dirOpts[0].Value != "mlx" || dirOpts[1].Value != "vllm-ok" {
		t.Fatalf("expected [mlx, vllm-ok] for directory model, got %+v", dirOpts)
	}
}

func TestStandaloneContextOptionsValidationAndMemoryShortfall(t *testing.T) {
	snap, _ := json.Marshal(map[string]any{
		"hardware": map[string]any{
			"memory_available_bytes": float64(4 * (1 << 30)),
		},
	})
	s := &viewState{
		device: core.DeviceResponse{Snapshot: snap},
	}
	model := core.ModelAsset{
		Name:          "qwen-32k.gguf",
		Format:        "gguf",
		ContextLength: int64Ptr(32768),
		SizeBytes:     int64Ptr(8 * (1 << 30)),
		Available:     true,
	}

	ctxOpts := standaloneContextOptions(model)
	// Presets <= 32768: 4k, 8k, 16k, 32k + custom = 5 options.
	if len(ctxOpts) != 5 || ctxOpts[len(ctxOpts)-1].Value != standaloneCustomContext {
		t.Fatalf("unexpected context options for 32k model: %+v", ctxOpts)
	}

	s.standalone.contextPreset = standaloneCustomContext
	s.standalone.customContext = "12000"
	if val, errMsg := s.resolvedStandaloneContext(model); errMsg != "" || val != 12000 {
		t.Fatalf("expected 12000 valid, got val=%d err=%q", val, errMsg)
	}
	s.standalone.customContext = "32768"
	if val, errMsg := s.resolvedStandaloneContext(model); errMsg != "" || val != 32768 {
		t.Fatalf("expected 32768 valid, got val=%d err=%q", val, errMsg)
	}
	s.standalone.customContext = "65536"
	if _, errMsg := s.resolvedStandaloneContext(model); errMsg == "" {
		t.Fatalf("expected 65536 to be rejected for 32k model")
	}

	if shortfall := s.standaloneMemoryShortfall(model); shortfall == "" {
		t.Fatalf("expected memory shortfall notice when model 8 GiB > available 4 GiB")
	}

	// Recommendation context_size applies only when recommendation.selected.model_path matches.
	snapWithRec, _ := json.Marshal(map[string]any{
		"recommendation": map[string]any{
			"selected": map[string]any{
				"model_path":   "/models/model-a.gguf",
				"context_size": 4096,
			},
		},
	})
	s.device.Snapshot = snapWithRec
	modelA := core.ModelAsset{Name: "a.gguf", Path: "/models/model-a.gguf", ContextLength: int64Ptr(32768)}
	modelB := core.ModelAsset{Name: "b.gguf", Path: "/models/model-b.gguf", ContextLength: int64Ptr(32768)}
	if got := s.recommendedStandaloneContext(modelA); got != 4096 {
		t.Fatalf("modelA recommended context=%d want 4096", got)
	}
	if got := s.recommendedStandaloneContext(modelB); got != 8192 {
		t.Fatalf("modelB recommended context=%d want 8192 fallback", got)
	}

	// PolyStrata strata_pack with 64 GiB on-disk SizeBytes but 10 GiB ResidentBytes fits on 32 GiB available memory.
	snap32G, _ := json.Marshal(map[string]any{
		"hardware": map[string]any{
			"apple_silicon":          true,
			"memory_total_bytes":     float64(64 * (1 << 30)),
			"memory_available_bytes": float64(32 * (1 << 30)),
		},
	})
	s.device.Snapshot = snap32G
	s.hwMemoryCache = uiHardwareMemoryCache{}
	strataModel := core.ModelAsset{
		Name:          "qwen3.8-flash-next-q2_0",
		Format:        "strata_pack",
		SizeBytes:     int64Ptr(64 * (1 << 30)),
		ResidentBytes: int64Ptr(10 * (1 << 30)),
		ContextLength: int64Ptr(32768),
		Available:     true,
	}
	s.registry.Models = []core.ModelAsset{strataModel}
	s.registry.Engines = []core.EngineAsset{
		{Name: "PolyStrata · 0.1.45-metal", Family: "polystrata"},
		{Name: "llama-b8000", Family: "llama-cpp"},
	}
	if shortfall := s.standaloneMemoryShortfall(strataModel); shortfall != "" {
		t.Fatalf("unexpected shortfall for mmap strata_pack: %q", shortfall)
	}
	budget := s.checkMemoryBudget(strataModel.Name, "PolyStrata · 0.1.45-metal", 32768, nil)
	if !budget.Known || !budget.Enough || budget.EstimatedGB > 16 {
		t.Fatalf("expected strata_pack ResidentBytes budget to fit comfortably, got %+v", budget)
	}
	if s.engineSupportsLlamaFlags("PolyStrata · 0.1.45-metal") {
		t.Fatalf("expected polystrata engine to hide llama-specific flags")
	}
	if !s.engineSupportsLlamaFlags("llama-b8000") {
		t.Fatalf("expected llama-cpp engine to support llama-specific flags")
	}
}

func TestStandaloneSwitchAndBenchmarkConflictGuards(t *testing.T) {
	m1 := core.ModelAsset{Name: "m1.gguf", Path: "/models/m1.gguf", Format: "gguf", Available: true}
	m2 := core.ModelAsset{Name: "m2.gguf", Path: "/models/m2.gguf", Format: "gguf", Available: true}
	s := &viewState{
		ready:          testState().ready,
		serveSupported: true,
		registry:       core.Registry{Models: []core.ModelAsset{m1, m2}},
		engineState: core.EngineState{
			State:    "running",
			Origin:   core.EngineOriginStandalone,
			Model:    "/models/m1.gguf",
			Endpoint: "http://127.0.0.1:48857",
		},
	}

	// Same model already running: idempotent, does not open drawer or switch dialog.
	s.requestStandaloneStart(m1, false)
	if s.standalone.drawerOpen || s.standalone.switchConfirm {
		t.Fatalf("expected same-model start to be idempotent, got %+v", s.standalone)
	}

	// Different model: P3 multi-service opens drawer directly for parallel start (no switch confirmation).
	s.requestStandaloneStart(m2, false)
	if !s.standalone.drawerOpen || s.standalone.modelName != "m2.gguf" || s.standalone.switchConfirm {
		t.Fatalf("expected parallel start drawer for m2.gguf without switchConfirm, got %+v", s.standalone)
	}

	// Standalone running blocks direct benchmark start and opens handoff confirm.
	called := false
	if s.guardStandaloneForBenchmark(func() { called = true }) {
		t.Fatalf("expected benchmark to be intercepted while standalone service is running")
	}
	if called || !s.standalone.benchmarkConfirm || s.standalone.benchmarkAction == nil {
		t.Fatalf("expected benchmarkConfirm to be armed, got %+v", s.standalone)
	}

	// Outdated CLI blocks standalone start.
	s.serveSupported = false
	if reason := s.standaloneStartBlockReason(); reason == "" {
		t.Fatalf("expected block reason when serveSupported=false")
	}

	// Engine card summary for standalone shows origin row and omits Config candidate row.
	rows := s.engineCardSummary()
	foundOrigin := false
	for _, r := range rows {
		if r.value == engineOriginLabel(core.EngineOriginStandalone) {
			foundOrigin = true
		}
	}
	if !foundOrigin {
		t.Fatalf("expected standalone origin row in engineCardSummary, got %+v", rows)
	}

	// Standalone starting does NOT count as benchmark roundWorkActive; parallel drawer opens directly.
	s.serveSupported = true
	s.standalone = standaloneServeState{}
	s.engineState.State = "starting"
	if s.roundWorkActive() {
		t.Fatalf("standalone starting must not block roundWorkActive")
	}
	s.requestStandaloneStart(m2, false)
	if !s.standalone.drawerOpen || s.standalone.modelName != "m2.gguf" || s.standalone.switchConfirm {
		t.Fatalf("expected parallel start drawer while another standalone is starting, got %+v", s.standalone)
	}

	// retryBenchmark is intercepted while standalone service is running/starting.
	s.standalone = standaloneServeState{}
	s.task = benchmarkTask{stage: "bind", failure: core.Err("engine_candidate_mismatch")}
	s.retryBenchmark()
	if !s.standalone.benchmarkConfirm || s.standalone.benchmarkAction == nil {
		t.Fatalf("expected retryBenchmark to trigger standalone handoff confirm")
	}

	// Header action prefers engineState.Model over lastModel, and preserves per-model retry params.
	s.selected = "dashboard"
	s.registry.Engines = []core.EngineAsset{
		{Name: "llama-a", Family: "llama-cpp"},
		{Name: "llama-b", Family: "llama-cpp"},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			"llama-a": {State: core.EngineCheckEligible},
			"llama-b": {State: core.EngineCheckEligible},
		},
	}
	s.engine = "llama-a"
	s.standalone = standaloneServeState{
		lastModel: "m2.gguf",
		lastParams: map[string]standaloneAttempt{
			"m1.gguf": {engine: "llama-b", context: 12000},
			"m2.gguf": {engine: "llama-a", context: 4096},
		},
	}
	s.engineState = core.EngineState{State: "failed", Origin: core.EngineOriginStandalone, Model: "/models/m1.gguf"}
	s.openStandaloneFromHeader(true)
	if s.selected != "library" || s.libraryTab != 0 || !s.standalone.drawerOpen || s.standalone.modelName != "m1.gguf" || s.standalone.engineName != "llama-b" || s.standalone.customContext != "12000" {
		t.Fatalf("expected header retry to restore m1.gguf params (llama-b, 12000), got selected=%q libraryTab=%d standalone=%+v", s.selected, s.libraryTab, s.standalone)
	}

	// Normal start (prefillLast=false) uses recommended default (8192) instead of previous 12000.
	s.standalone = standaloneServeState{
		lastModel: "m1.gguf",
		lastParams: map[string]standaloneAttempt{
			"m1.gguf": {engine: "llama-b", context: 12000},
		},
	}
	s.engineState = core.EngineState{State: "stopped", Origin: core.EngineOriginStandalone, Model: "/models/m1.gguf"}
	s.openStandaloneDrawer(m1, false)
	if s.standalone.contextPreset != "8192" || s.standalone.customContext != "8192" || s.standalone.engineName != "llama-a" {
		t.Fatalf("expected normal start defaults (llama-a, 8192), got engine=%q preset=%q custom=%q", s.standalone.engineName, s.standalone.contextPreset, s.standalone.customContext)
	}

	// Unsaved settings guard prevents setting drawerOpen before user confirms leaving settings.
	s.selected = "settings"
	s.settingsLoaded = true
	s.workspace = "/dirty-workspace"
	s.standalone = standaloneServeState{}
	s.settingsLeaveConfirm = false
	s.openStandaloneFromHeader(false)
	if !s.settingsLeaveConfirm || s.standalone.drawerOpen || s.selected != "settings" {
		t.Fatalf("expected unsaved settings guard to defer drawer open, got confirm=%v drawer=%v selected=%q", s.settingsLeaveConfirm, s.standalone.drawerOpen, s.selected)
	}
}
