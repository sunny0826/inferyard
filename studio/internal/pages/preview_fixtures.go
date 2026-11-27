//go:build ui_debug && !release

package pages

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

//go:embed testdata/studio.json
var previewReportJSON []byte

var previewScenes = []struct {
	labelKey string
	apply    func(*viewState)
}{
	{"preview.scene.task_preparation", previewPreparation},
	{"preview.scene.cli_outdated", previewCLIOutdatedScene},
	{"preview.scene.task_ready", func(s *viewState) {
		s.history, s.downloads, s.reports = core.RunHistoryResponse{}, nil, nil
	}},
	{"preview.scene.task_running", previewRunning},
	{"preview.scene.speed_running", previewSpeedRunning},
	{"preview.scene.task_completed", previewCompleted},
	{"preview.scene.task_failed", func(s *viewState) {
		s.task = benchmarkTask{stage: "bind", start: startChainResult{candidate: "/preview/candidate"}, failure: core.Err("engine_start_failed")}
		s.engineState = core.EngineState{
			State:       "failed",
			Family:      "llama-cpp",
			Executable:  "llama-server",
			Model:       s.model,
			ModelKind:   "gguf",
			ContextSize: 32768,
			Candidate:   "/preview/candidate",
			Error:       core.Err("engine_start_failed"),
		}
	}},
	{"preview.scene.first_use_empty", func(s *viewState) {
		previewPreparation(s)
		s.registry = core.Registry{}
		s.model, s.engine = "", ""
		s.checks = core.EngineCheckOverview{Supported: true}
	}},
	{"preview.scene.engines_local_and_dir", previewLocalEngines},
	{"preview.scene.engines_lmstudio_bind", func(s *viewState) {
		previewLocalEngines(s)
		s.selected = "benchmarks"
		s.engine, s.model = l10n.TRf("preview.engine.local_name", "family", "lmstudio"), "local.gguf"
		s.localPID, s.localEndpoint = "4001", "http://127.0.0.1:48857"
		s.discoveryLoaded = true
		s.discovery = inferyard.ServiceDiscovery{Status: "complete", Services: []inferyard.DiscoveredService{
			{PID: 4001, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:48857", Models: []string{"local.gguf", "local-weights"}}}},
		}}
	}},
	{"preview.scene.library_edge_states", previewLibraryEdgeStatesScene},
	{"preview.scene.resources_dense_missing", previewDenseResources},
	{"preview.scene.resources_cpu_sensors", previewMultiSensorResources},
	{"preview.scene.report_compare_multi", func(s *viewState) { s.openReport("compare-report") }},
	{"preview.scene.report_long_missing", func(s *viewState) { s.openReport("long-report") }},
	{"preview.scene.report_svg_pelican", func(s *viewState) { s.openReport("pelican-report") }},
	{"preview.scene.report_read_failed", func(s *viewState) { s.openReport("broken-report") }},
	{"preview.scene.speed_history_results", previewSpeedHistoryScene},
	{"preview.scene.downloads_active_failed", previewDownloadsActiveAndFailed},
	{"preview.scene.downloads_polystrata_setup", previewDownloadsPolystrata},
	{"preview.scene.downloads_polystrata_model", previewDownloadsPolystrataModel},
	{"preview.scene.rec_mac48_first_use", previewRecMac48FirstUse},
	{"preview.scene.rec_rtx5090_tiers", previewRecRTX5090FirstUse},
	{"preview.scene.rec_rtx4070_tight", previewRecRTX4070Tight},
	{"preview.scene.rec_existing_collapsed", previewRecExistingAssetsCollapsed},
	{"preview.scene.rec_cpu_fallback", previewRecCPUFallback},
	{"preview.scene.rec_blocked_disk", previewRecBlockedDisk},
	{"preview.scene.service_exited", previewServiceExitedScene},
	{"preview.scene.services_multi", previewMultiServicesScene},
	{"preview.scene.service_orphaned", previewOrphanedServiceScene},
	{"preview.scene.notifications_center", previewNotificationsScene},
	{"preview.scene.update_available", previewUpdateAvailable},
	{"preview.scene.update_downloading", previewUpdateDownloading},
	{"preview.scene.update_ready", previewUpdateReadyToApply},
	{"preview.scene.update_error", previewUpdateError},
	{"preview.scene.design_system", previewDesignSystemScene},
}

func previewDenseResources(s *viewState) {
	s.detailID = "dense-resources"
	s.detail = previewDetailFixture(s.detailID)
	s.detail.Task.State = "done"
	s.detail.Resources = nil
	for i := range 158 {
		utc := time.Date(2026, 10, 20, 9, 21, i, 570000000, time.UTC).Format(time.RFC3339Nano)
		rows := previewResourceSamples(int64(i*5+1), utc, int64(3<<30)+int64(i%18)*(1<<24), 1<<28, 0)
		s.detail.Resources = append(s.detail.Resources, rows[:2]...)
	}
	s.downloads = nil
}

func previewPreparation(s *viewState) {
	s.progress = nil
	s.history, s.downloads, s.reports = core.RunHistoryResponse{}, nil, nil
	s.ready.CLI.Status = "not_found"
	s.serveSupported = false
	s.ready.Workspace.Initialized, s.ready.Results.Exists, s.ready.Models.Exists = false, false, false
}

func previewValue[T any](value T) *T { return &value }

func previewBase(ctx context.Context) *viewState {
	root := filepath.Join(os.TempDir(), "inferyard-ui-preview")
	model, engine := "Preview-Model-Q4.gguf", "preview-engine · llama-server"
	settings := core.UserSettings{WorkspaceRoot: previewValue(root)}
	resultsRoot := previewValue(filepath.Join(root, "results"))
	modelsRoot := previewValue(filepath.Join(root, "models"))
	reportsRoot := previewValue(filepath.Join(root, "reports"))
	s := &viewState{
		windowTitle: l10n.TR("app.title.preview"),
		speed:       &speedWorkspace{context: inferyard.ContextSpeed64K},
		settingsViewState: settingsViewState{
			downloadSource: core.DownloadSourceModelScope,
			buildInfo: func() core.BuildInfo {
				return core.BuildInfo{
					Version:   previewAppVersion,
					BuildTime: "2026-11-26T08:00:00Z",
					GitCommit: "0123456789abcdef0123456789abcdef01234567",
				}
			},
		},
		ctx: ctx, selected: "benchmarks", benchFlowOpen: true, loaded: true, settingsLoaded: true, serveSupported: true,
		denied: map[string]bool{}, model: model, engine: engine, discoveryLoaded: true,
		engineState: core.EngineState{State: "stopped"},
		ready: core.Readiness{
			CLI:       core.InferyardProbe{Status: "ok", Version: previewValue(previewAppVersion)},
			Workspace: core.DirectoryReadiness{Path: settings.WorkspaceRoot, Exists: true, Initialized: true},
			Results:   core.DirectoryReadiness{Path: resultsRoot, Exists: true},
			Models:    core.DirectoryReadiness{Path: modelsRoot, Exists: true},
		},
		settings: core.SettingsResponse{Settings: settings, Effective: core.EffectiveSettings{
			WorkspaceRoot: settings.WorkspaceRoot, ResultsRoot: resultsRoot, ModelsRoot: modelsRoot, ReportsRoot: reportsRoot,
			DownloadSource: core.DownloadSourceModelScope, Theme: core.ThemeSystem, KeepServicesOnQuit: true,
		}},
		keepServicesOnQuit: true,
		recentServices:     seedPreviewRecentServices(model, engine),
		registry: core.Registry{
			Models: []core.ModelAsset{
				{
					Name: model, Format: "gguf", Available: true, Architecture: "llama",
					SizeBytes: previewValue(int64(4 << 30)), Path: filepath.Join(root, "models", model), Deletable: true,
				},
				{
					Name: "Preview-Small.gguf", Format: "gguf", Available: true, Architecture: "qwen3",
					SizeBytes: previewValue(int64(2 << 30)), Path: filepath.Join(root, "models", "Preview-Small.gguf"), Deletable: true,
				},
			},
			Engines: []core.EngineAsset{{
				Name: engine, Family: "llama-cpp", Source: l10n.TR("preview.source.engine_dir"),
				Path: filepath.Join(root, "engines", "llama-server"), Deletable: true,
			}},
			DefaultEngine:   engine,
			TotalModelBytes: 6 << 30,
			DiskFreeBytes:   previewValue(int64(160 << 30)),
			ModelRoots:      []string{filepath.Join(root, "models")},
		},
		checks: core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{
			engine: {State: core.EngineCheckEligible, Family: "llama-cpp", Version: "prism-b10743-adfffbe"},
		}},
		progress: &inferyard.WorkspaceProgress{Workspace: root, NextAction: "prepare_candidate",
			DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: previewValue(filepath.Join(root, "preflight"))},
			Bundles: []inferyard.BundleProgress{
				{Bundle: inferyard.DefaultBundleCore, Phase: "candidate_missing", NextAction: "prepare_candidate"},
				{Bundle: inferyard.DefaultBundlePelican, Phase: "candidate_missing", NextAction: "prepare_candidate"},
			}},
		reports:             []core.ReportSummary{previewReportSummary("preview-report")},
		modelEvidence:       seedPreviewModelEvidence(model),
		engineUpdates:       seedPreviewEngineUpdates(engine),
		reportExclusivities: seedPreviewReportExclusivities(),
	}
	s.loadSettings(s.settings)
	seeded := core.RunSummary{
		RunId: "preview-run", Path: "/preview/results/preview-run",
		ModelName: previewValue(model), Engine: previewValue(engine),
		Bundle: previewValue(inferyard.DefaultBundleCore), Status: "complete",
		Sealed: true, Completeness: previewValue("complete"),
		StartedAt: previewValue("2026-10-16T23:55:00Z"), FinishedAt: previewValue("2026-10-16T23:58:12Z"),
	}
	seeded.Score = &core.RunScore{Passed: 7, Valid: 13, Rate: previewValue(7.0 / 13)}
	s.history = core.RunHistoryResponse{
		ResultsRoot:    resultsRoot,
		ScannedAt:      "2026-10-16T23:59:00Z",
		StandaloneRuns: []core.RunSummary{seeded},
	}
	s.device.Snapshot = json.RawMessage(previewSnapshotMac48)
	return s
}

func previewRunning(s *viewState) {
	s.task.stage = "run"
	s.progress.NextAction = "run"
	s.progress.Bundles[0].Phase, s.progress.Bundles[0].NextAction = "run_incomplete", "run"
	s.progress.Bundles[0].BlockedReason = previewValue("single_run_resume_not_supported")
	s.engineState = core.EngineState{
		State:       "running",
		Family:      "llama-cpp",
		Executable:  "llama-server",
		Model:       s.model,
		ModelKind:   "gguf",
		ContextSize: 32768,
		Pid:         42,
		Endpoint:    "http://127.0.0.1:48857",
		Candidate:   "/preview/candidate",
	}
	s.tasks = []core.RunTask{{TaskId: "preview-run", State: "running", Bundle: previewValue(inferyard.DefaultBundleCore), RunPath: previewValue("/preview/results/preview-run"), StartedAt: previewValue("2026-10-16T23:59:08Z")}}
	s.runProgress = &core.RunProgress{Done: 42, Total: previewValue(int64(120)), CurrentCaseId: previewValue("preview-case-043")}
}

func previewSpeedRunning(s *viewState) {
	s.selected = "benchmarks"
	s.benchFlowOpen = true
	if s.speed == nil {
		s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	}
	s.speed.selected = true
	s.engineState = core.EngineState{
		State:       "running",
		Family:      "llama-cpp",
		Executable:  "llama-server",
		Model:       s.model,
		ModelKind:   "gguf",
		ContextSize: inferyard.ContextSpeed64K,
		Pid:         42,
		Endpoint:    "http://127.0.0.1:48857",
	}
	task := &speedTask{
		id:              "preview-speed-live",
		model:           s.model,
		engine:          s.engine,
		context:         inferyard.ContextSpeed64K,
		stage:           speedMeasuring,
		completed:       2,
		budget:          300,
		startedAt:       "2026-10-16T23:59:12Z",
		selectionModel:  s.model,
		selectionEngine: s.engine,
	}
	for i := range 2 {
		task.samples = append(task.samples, previewSpeedSample(inferyard.ContextSpeed64K, i))
	}
	s.speed.current = task
}

func previewCompleted(s *viewState) {
	s.benchFlowOpen = false
	s.task.stage, s.task.report = "done", "preview-report"
	s.progress.NextAction, s.progress.DefaultScopeComplete = "done", true
	s.progress.Report = inferyard.ProgressReport{Status: "present", Path: previewValue("preview-report"), Runs: []string{"preview-run"}}
	for i := range s.progress.Bundles {
		s.progress.Bundles[i].Phase, s.progress.Bundles[i].NextAction = "run_complete", "none"
	}
	batchID := "preview-batch-01"
	partialRun := core.RunSummary{
		RunId:        "preview-batch-run",
		Path:         "/preview/results/preview-batch-run",
		BatchId:      previewValue(batchID),
		ModelName:    previewValue("Preview-Small.gguf"),
		Engine:       previewValue(s.engine),
		Bundle:       previewValue(inferyard.DefaultBundlePelican),
		Status:       "partial",
		Sealed:       false,
		Completeness: previewValue("partial"),
		StopReason:   previewValue("interrupted_checkpoint"),
		Resumable:    true,
		StartedAt:    previewValue("2026-10-17T00:12:00Z"),
		Score:        &core.RunScore{Passed: 2, Valid: 5, Rate: previewValue(0.4)},
	}
	batchFailedRun := core.RunSummary{
		RunId:        "preview-batch-failed",
		Path:         "/preview/results/preview-batch-failed",
		BatchId:      previewValue(batchID),
		ModelName:    previewValue("Preview-Small.gguf"),
		Engine:       previewValue(s.engine),
		Bundle:       previewValue(inferyard.DefaultBundleCore),
		Status:       "failed",
		Sealed:       false,
		Completeness: previewValue("incomplete"),
		StopReason:   previewValue("engine_connection_lost"),
		Resumable:    true,
		StartedAt:    previewValue("2026-10-17T00:08:00Z"),
		FinishedAt:   previewValue("2026-10-17T00:09:45Z"),
		Score:        &core.RunScore{Passed: 4, Valid: 6, Failed: 2},
	}
	s.history.Batches = []core.BatchSummary{{
		BatchId: batchID,
		Path:    "/preview/results/" + batchID,
		Runs:    []core.RunSummary{partialRun, batchFailedRun},
	}}

	smallComplete := core.RunSummary{
		RunId:        "preview-run-small",
		Path:         "/preview/results/preview-run-small",
		ModelName:    previewValue("Preview-Small.gguf"),
		Engine:       previewValue("mlx-lm · 0.32.2"),
		Bundle:       previewValue(inferyard.DefaultBundleCore),
		Status:       "complete",
		Sealed:       true,
		Completeness: previewValue("complete"),
		StartedAt:    previewValue("2026-10-16T23:30:00Z"),
		FinishedAt:   previewValue("2026-10-16T23:32:18Z"),
		Score:        &core.RunScore{Passed: 10, Valid: 13, Rate: previewValue(10.0 / 13)},
	}
	failedStandalone := core.RunSummary{
		RunId:        "preview-failed-run",
		Path:         "/preview/results/preview-failed-run",
		ModelName:    previewValue(s.model),
		Engine:       previewValue(s.engine),
		Bundle:       previewValue(inferyard.DefaultBundleCore),
		Status:       "failed",
		Sealed:       false,
		Completeness: previewValue("incomplete"),
		StopReason:   previewValue("engine_connection_lost"),
		Resumable:    false,
		StartedAt:    previewValue("2026-10-16T22:15:00Z"),
		FinishedAt:   previewValue("2026-10-16T22:16:04Z"),
	}
	cancelledStandalone := core.RunSummary{
		RunId:        "preview-cancelled-run",
		Path:         "/preview/results/preview-cancelled-run",
		ModelName:    previewValue("Preview-Small.gguf"),
		Engine:       previewValue(s.engine),
		Bundle:       previewValue(inferyard.DefaultBundlePelican),
		Status:       "cancelled",
		Sealed:       false,
		Completeness: previewValue("incomplete"),
		StopReason:   previewValue("user_cancelled"),
		Resumable:    false,
		StartedAt:    previewValue("2026-10-16T21:40:00Z"),
		FinishedAt:   previewValue("2026-10-16T21:40:28Z"),
	}
	s.history.StandaloneRuns = append(s.history.StandaloneRuns[:1], smallComplete, failedStandalone, cancelledStandalone)
	s.reports = []core.ReportSummary{
		previewReportSummary("preview-report"),
		previewReportSummary("compare-report"),
	}
	seedPreviewSpeedHistory(s)
}

func previewDetailFixture(id string) *core.RunTaskDetail {
	return &core.RunTaskDetail{
		Task: core.RunTask{TaskId: id, Kind: "run", State: "running", Bundle: previewValue(inferyard.DefaultBundleCore), RunId: previewValue("preview-run"), RunPath: previewValue("/preview/results/preview-run"), StartedAt: previewValue("2026-10-16T23:59:08Z")},
		Progress: &core.RunProgress{Done: 42, Total: previewValue(int64(120)), Phase: previewValue("formal"), CurrentCaseId: previewValue("preview-case-043"), Cases: []core.CaseProgress{
			{
				CaseId:       "preview-case-001",
				State:        "completed",
				Category:     previewValue(l10n.TR("preview.fixture.category")),
				QualityState: previewValue("pass"),
				DurationMs:   previewValue(1240.0),
				Prompt:       previewValue("Compute 24 * 17 and output only the final integer."),
				Response:     previewValue("408"),
			},
			{
				CaseId:        "preview-case-002",
				State:         "failed",
				Category:      previewValue(l10n.TR("preview.fixture.category")),
				ErrorCategory: previewValue("preview_failure"),
				Prompt:        previewValue("Summarize the key trade-offs between KV cache quantization and attention throughput in two sentences."),
			},
		}},
		Resources: append(append(
			previewResourceSamples(1, "2026-10-17T00:00:00Z", 10<<30, 2<<30, 120),
			previewResourceSamples(6, "2026-10-17T00:00:01Z", 9<<30, 3<<30, 240)...),
			previewResourceSamples(11, "2026-10-17T00:00:02Z", 10<<30, 2<<30, 180)...),
	}
}
