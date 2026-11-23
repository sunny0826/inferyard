package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestPolystrataAutoSelectAndDoneTaskCTA(t *testing.T) {
	// 1. Fresh workspace with deduplicated strata_pack + eligible PolyStrata engine auto-selects both.
	sFresh := testState()
	sFresh.registry.Engines = []core.EngineAsset{{
		Name:   "PolyStrata · v0.1.45-poly.10",
		Family: "polystrata",
		Kind:   "local_installation",
		Path:   "/ws/engines/polystrata-1/installation.json",
	}}
	sFresh.checks.Engines = map[string]core.EngineEligibility{
		"PolyStrata · v0.1.45-poly.10": {State: core.EngineCheckEligible, Family: "polystrata"},
	}
	sFresh.registry.Models = []core.ModelAsset{{
		Name:      "Qwen3.6-35B-A3B (UD-Q4_K_M)",
		Format:    "strata_pack",
		Path:      "/ws/models/polystrata-data/models/qwen36/strata-qwen36.json",
		SizeBytes: ptr(int64(22663387424)),
		Available: true,
	}}
	sFresh.adoptDownloadedEngine()
	sFresh.adoptOnlyModel()
	if sFresh.engine != "PolyStrata · v0.1.45-poly.10" || sFresh.model != "Qwen3.6-35B-A3B (UD-Q4_K_M)" {
		t.Fatalf("expected fresh workspace auto-selection of PolyStrata pair, got engine=%q model=%q", sFresh.engine, sFresh.model)
	}

	// 2. Multi-model workspace selects newly installed compatible strata_pack matching done task.
	sMulti := testState()
	sMulti.registry.Engines = []core.EngineAsset{{
		Name:   "PolyStrata · v0.1.45-poly.10",
		Family: "polystrata",
		Kind:   "local_installation",
		Path:   "/ws/engines/polystrata-1/installation.json",
	}}
	sMulti.checks.Engines = map[string]core.EngineEligibility{
		"PolyStrata · v0.1.45-poly.10": {State: core.EngineCheckEligible, Family: "polystrata"},
	}
	sMulti.registry.Models = []core.ModelAsset{
		{Name: "other.gguf", Format: "gguf", Path: "/ws/models/other.gguf", Available: true},
		{Name: "Qwen3.6-35B-A3B (UD-Q4_K_M)", Format: "strata_pack", Path: "/ws/models/strata-qwen36.json", Available: true},
	}
	sMulti.downloads = []core.DownloadTask{{
		TaskId:           "ps-done",
		Kind:             "polystrata",
		State:            "done",
		InstallationPath: ptr("/ws/engines/polystrata-1/installation.json"),
		ModelConfigPath:  ptr("/ws/models/strata-qwen36.json"),
	}}
	sMulti.adoptDownloadedEngine()
	sMulti.adoptOnlyModel()
	if sMulti.engine != "PolyStrata · v0.1.45-poly.10" || sMulti.model != "Qwen3.6-35B-A3B (UD-Q4_K_M)" {
		t.Fatalf("expected multi-model auto-selection to pick installed PolyStrata config, got engine=%q model=%q", sMulti.engine, sMulti.model)
	}

	// 3. Completed PolyStrata task card renders "前往开始测评" and navigates to dashboard with pre-selected engine & model.
	sCard := testState()
	sCard.selected = "downloads"
	sCard.detailID = "old-detail"
	sCard.registry.Engines = []core.EngineAsset{
		{Name: "llama-cpp · local", Family: "llama-cpp", Kind: "local_installation", Path: "/ws/engines/llama/installation.json"},
		{Name: "PolyStrata · v0.1.45-poly.10", Family: "polystrata", Kind: "local_installation", Path: "/ws/engines/polystrata-1/installation.json"},
	}
	sCard.checks.Engines = map[string]core.EngineEligibility{
		"llama-cpp · local":             {State: core.EngineCheckEligible, Family: "llama-cpp"},
		"PolyStrata · v0.1.45-poly.10": {State: core.EngineCheckEligible, Family: "polystrata"},
	}
	sCard.registry.Models = []core.ModelAsset{
		{Name: "other.gguf", Format: "gguf", Path: "/ws/models/other.gguf", Available: true},
		{Name: "Qwen3.6-35B-A3B (UD-Q4_K_M)", Format: "strata_pack", Path: "/ws/models/strata-qwen36.json", Available: true},
	}
	sCard.downloads = []core.DownloadTask{{
		TaskId:           "ps-complete",
		Kind:             "polystrata",
		DisplayName:      "PolyStrata · qwen36 · UD-Q4_K_M",
		State:            "done",
		OutName:          "polystrata-1",
		InstallationPath: ptr("/ws/engines/polystrata-1/installation.json"),
		ModelConfigPath:  ptr("/ws/models/strata-qwen36.json"),
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { sCard.downloadsView(c) }) }, 1120, 2200)
	if !tt.HasText("前往开始测评") {
		t.Fatalf("expected '前往开始测评' button on completed PolyStrata card: %v", tt.Texts())
	}
	click(t, tt, "前往开始测评")
	if sCard.selected != "dashboard" || sCard.detailID != "" {
		t.Fatalf("expected navigation to dashboard with empty detailID, got selected=%q detailID=%q", sCard.selected, sCard.detailID)
	}
	if sCard.engine != "PolyStrata · v0.1.45-poly.10" || sCard.model != "Qwen3.6-35B-A3B (UD-Q4_K_M)" {
		t.Fatalf("expected installed engine and model pre-selected after CTA, got engine=%q model=%q", sCard.engine, sCard.model)
	}

	// 4. CTA preserves active benchmark state when a run is in progress.
	sBusy := testState()
	sBusy.selected = "downloads"
	sBusy.tasks = []core.RunTask{{TaskId: "run-active", State: "running"}}
	sBusy.task.stage = "running"
	sBusy.registry.Engines = sCard.registry.Engines
	sBusy.registry.Models = sCard.registry.Models
	sBusy.checks.Engines = sCard.checks.Engines
	sBusy.downloads = sCard.downloads
	ttBusy := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { sBusy.downloadsView(c) }) }, 1120, 2200)
	click(t, ttBusy, "前往开始测评")
	if sBusy.selected != "dashboard" || sBusy.task.stage != "running" {
		t.Fatalf("CTA must navigate without interrupting active benchmark: selected=%q stage=%q", sBusy.selected, sBusy.task.stage)
	}

	// 5. Missing-asset strata_pack renders localized unavailable reason.
	sMissing := testState()
	sMissing.registry.Models = []core.ModelAsset{{
		Name:      "Qwen3.6-35B-A3B",
		Format:    "strata_pack",
		Available: false,
		Reason:    "polystrata_config_model_missing",
	}}
	ttModels := ui.NewTester(sMissing.modelsView, 1120, 1800)
	if !ttModels.HasText("不可用：配置文件引用的模型文件缺失") {
		t.Fatalf("expected localized missing-asset reason on models page: %v", ttModels.Texts())
	}
}

func TestPolystrataReconcileIdleResetAndOneShotPendingSelection(t *testing.T) {
	// 1. Path-aware reconciliation: user selected raw B.gguf before B's helper weight existed;
	// after refresh deduplicates B.gguf into Config B alongside Config A (with A in done downloads),
	// s.model migrates cleanly onto Config B (never hijacking onto Config A) and keeps still-valid selections intact.
	s := settingsState()
	s.service = settingsTestService(t, t.TempDir())
	s.ctx = context.Background()
	s.discoveryLoaded = true
	s.engine = "PolyStrata · v0.1.45-poly.10"
	s.registry.Models = []core.ModelAsset{
		{Name: "A Config", Format: "strata_pack", Path: "/ws/models/ps/strata-a.json", CoveredPaths: []string{"/ws/models/ps/A.gguf"}, Available: true},
		{Name: "B.gguf", Format: "gguf", Path: "/ws/models/ps/B.gguf", Available: true},
	}
	s.model = "B.gguf"

	batch := refreshBatch{
		epoch:    s.settingsSequence(),
		full:     true,
		settings: s.settings,
		registry: core.Registry{
			Engines: []core.EngineAsset{{Name: "PolyStrata · v0.1.45-poly.10", Family: "polystrata", Kind: "local_installation", Path: "/ws/engines/ps/installation.json"}},
			Models: []core.ModelAsset{
				{Name: "A Config", Format: "strata_pack", Path: "/ws/models/ps/strata-a.json", CoveredPaths: []string{"/ws/models/ps/A.gguf"}, Available: true},
				{Name: "B Config", Format: "strata_pack", Path: "/ws/models/ps/strata-b.json", CoveredPaths: []string{"/ws/models/ps/B.gguf"}, Available: true},
				{Name: "valid-other.gguf", Format: "gguf", Path: "/ws/models/valid-other.gguf", Available: true},
			},
		},
		downloads: []core.DownloadTask{{
			TaskId:           "ps-done-a",
			Kind:             "polystrata",
			State:            "done",
			InstallationPath: ptr("/ws/engines/ps/installation.json"),
			ModelConfigPath:  ptr("/ws/models/ps/strata-a.json"),
		}},
	}
	if !s.applyRefreshBatch(batch) {
		t.Fatal("expected full refresh batch to apply")
	}
	if s.model != "B Config" {
		t.Fatalf("expected vanished raw B.gguf to migrate by CoveredPaths to B Config (not A Config), got %q", s.model)
	}

	// Ambiguous replacement (B1 and B2 both cover B.gguf) leaves model unselected instead of switching to A Config.
	sAmbig := settingsState()
	sAmbig.service = settingsTestService(t, t.TempDir())
	sAmbig.ctx = context.Background()
	sAmbig.discoveryLoaded = true
	sAmbig.engine = "PolyStrata · v0.1.45-poly.10"
	sAmbig.registry.Models = []core.ModelAsset{
		{Name: "B.gguf", Format: "gguf", Path: "/ws/models/ps/B.gguf", Available: true},
	}
	sAmbig.model = "B.gguf"
	ambigBatch := refreshBatch{
		epoch:    sAmbig.settingsSequence(),
		full:     true,
		settings: sAmbig.settings,
		registry: core.Registry{
			Engines: batch.registry.Engines,
			Models: []core.ModelAsset{
				batch.registry.Models[0],
				{Name: "B1 Config", Format: "strata_pack", Path: "/ws/models/ps/strata-b1.json", CoveredPaths: []string{"/ws/models/ps/B.gguf"}, Available: true},
				{Name: "B2 Config", Format: "strata_pack", Path: "/ws/models/ps/strata-b2.json", CoveredPaths: []string{"/ws/models/ps/B.gguf"}, Available: true},
			},
		},
		downloads: batch.downloads,
	}
	if !sAmbig.applyRefreshBatch(ambigBatch) {
		t.Fatal("expected ambiguous refresh batch to apply")
	}
	if sAmbig.model != "" {
		t.Fatalf("expected ambiguous replacement to clear selection rather than switching to A Config, got %q", sAmbig.model)
	}

	s.model = "valid-other.gguf"
	if !s.applyRefreshBatch(batch) {
		t.Fatal("expected second refresh batch to apply")
	}
	if s.model != "valid-other.gguf" {
		t.Fatalf("expected valid user selection to remain untouched, got %q", s.model)
	}

	// 2. Idle CTA resets completed speed workspace and completed workspace state to prepare_candidate (step 2).
	sIdle := testState()
	sIdle.selected = "downloads"
	sIdle.progress = &inferyard.WorkspaceProgress{DefaultScopeComplete: true}
	sIdle.speed = &speedWorkspace{
		selected: true,
		current:  &speedTask{id: "sp-done", stage: speedDone},
		opened:   &speedTask{id: "sp-done", stage: speedDone},
	}
	sIdle.registry.Engines = batch.registry.Engines
	sIdle.registry.Models = batch.registry.Models
	sIdle.checks.Engines = map[string]core.EngineEligibility{
		"PolyStrata · v0.1.45-poly.10": {State: core.EngineCheckEligible, Family: "polystrata"},
	}
	sIdle.startBenchmarkFromPolystrataTask(batch.downloads[0])
	if sIdle.selected != "dashboard" || sIdle.speed.current != nil || sIdle.speed.opened != nil || sIdle.speed.selected {
		t.Fatalf("expected CTA to reset completed speed workspace, got selected=%q speed=%+v", sIdle.selected, sIdle.speed)
	}
	if sIdle.task.stage != "prepare_candidate" || sIdle.step() != 2 {
		t.Fatalf("expected CTA to enter prepare_candidate (step 2), got stage=%q step=%d", sIdle.task.stage, sIdle.step())
	}

	// 3. One-shot pendingPolystrataSelection is cleared on first full refresh even when target model is missing,
	// and never overwrites subsequent manual engine choices.
	sPending := settingsState()
	sPending.service = settingsTestService(t, t.TempDir())
	sPending.ctx = context.Background()
	sPending.discoveryLoaded = true
	sPending.registry.Engines = []core.EngineAsset{
		{Name: "PolyStrata · v0.1.45-poly.10", Family: "polystrata", Kind: "local_installation", Path: "/ws/engines/ps/installation.json"},
		{Name: "llama-cpp · local", Family: "llama-cpp", Kind: "local_installation", Path: "/ws/engines/llama/installation.json"},
	}
	sPending.pendingPolystrataSelection = &polystrataSelectionTarget{
		installationPath: "/ws/engines/ps/installation.json",
		modelConfigPath:  "/ws/models/missing-strata.json",
		initialEngine:    "llama-cpp · local",
		initialModel:     "",
	}
	sPending.engine = "llama-cpp · local"

	missingBatch := refreshBatch{
		epoch:    sPending.settingsSequence(),
		full:     true,
		settings: sPending.settings,
		registry: sPending.registry,
	}
	if !sPending.applyRefreshBatch(missingBatch) {
		t.Fatal("expected missingBatch to apply")
	}
	if sPending.pendingPolystrataSelection != nil {
		t.Fatalf("expected pendingPolystrataSelection to be consumed in one shot: %+v", sPending.pendingPolystrataSelection)
	}
	// User manually picks llama-cpp; another full refresh must not switch engine back.
	sPending.engine = "llama-cpp · local"
	if !sPending.applyRefreshBatch(missingBatch) {
		t.Fatal("expected second missingBatch to apply")
	}
	if sPending.engine != "llama-cpp · local" {
		t.Fatalf("manual engine selection was overwritten after one-shot pending consumed: %q", sPending.engine)
	}

	// If user manually switches engine BEFORE the pending full refresh lands, pending must not override it either.
	sPending.pendingPolystrataSelection = &polystrataSelectionTarget{
		installationPath: "/ws/engines/ps/installation.json",
		modelConfigPath:  "/ws/models/strata-qwen36.json",
		initialEngine:    "",
		initialModel:     "",
	}
	sPending.engine = "llama-cpp · local"
	if !sPending.applyRefreshBatch(missingBatch) {
		t.Fatal("expected override-guard batch to apply")
	}
	if sPending.engine != "llama-cpp · local" || sPending.pendingPolystrataSelection != nil {
		t.Fatalf("pending selection overrode manual engine or remained pending: engine=%q pending=%+v", sPending.engine, sPending.pendingPolystrataSelection)
	}
}
