//go:build ui_debug && !release

package pages

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	{"preview.scene.task_ready", func(s *viewState) {
		s.history, s.downloads, s.reports = core.RunHistoryResponse{}, nil, nil
	}},
	{"preview.scene.task_running", previewRunning},
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
		s.engine, s.model = l10n.TRf("preview.engine.local_name", "family", "lmstudio"), "local.gguf"
		s.localPID, s.localEndpoint = "4001", "http://127.0.0.1:48857"
		s.discoveryLoaded = true
		s.discovery = inferyard.ServiceDiscovery{Status: "complete", Services: []inferyard.DiscoveredService{
			{PID: 4001, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:48857", Models: []string{"local.gguf", "local-weights"}}}},
		}}
	}},
	{"preview.scene.resources_dense_missing", previewDenseResources},
	{"preview.scene.resources_cpu_sensors", previewMultiSensorResources},
	{"preview.scene.report_long_missing", func(s *viewState) { s.openReport("long-report") }},
	{"preview.scene.report_svg_pelican", func(s *viewState) { s.openReport("pelican-report") }},
	{"preview.scene.report_read_failed", func(s *viewState) { s.openReport("broken-report") }},
	{"preview.scene.downloads_active_failed", previewDownloadsActiveAndFailed},
	{"preview.scene.downloads_polystrata_setup", previewDownloadsPolystrata},
	{"preview.scene.downloads_polystrata_model", previewDownloadsPolystrataModel},
	{"preview.scene.rec_mac48_first_use", previewRecMac48FirstUse},
	{"preview.scene.rec_rtx5090_tiers", previewRecRTX5090FirstUse},
	{"preview.scene.rec_rtx4070_tight", previewRecRTX4070Tight},
	{"preview.scene.rec_existing_collapsed", previewRecExistingAssetsCollapsed},
	{"preview.scene.rec_cpu_fallback", previewRecCPUFallback},
	{"preview.scene.rec_blocked_disk", previewRecBlockedDisk},
	{"preview.scene.update_available", previewUpdateAvailable},
	{"preview.scene.update_downloading", previewUpdateDownloading},
	{"preview.scene.update_ready", previewUpdateReadyToApply},
	{"preview.scene.update_error", previewUpdateError},
}

func previewDownloadsPolystrata(s *viewState) {
	s.selected = "downloads"
	s.registry.Engines = nil
	s.registry.Models = nil
	blockerErr := core.Err("polystrata_preflight_blocked")
	blockerErr.Details = map[string]any{
		"blockers": []map[string]any{{
			"code":        "cmake_missing",
			"message":     l10n.TR("preview.polystrata.blocker.cmake"),
			"fix_command": "brew install cmake",
		}},
	}
	s.downloads = []core.DownloadTask{
		{
			TaskId:      "preview-polystrata-active",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
			State:       "downloading",
			Stage:       "setup_engine",
			Step:        previewValue(3),
			StepTotal:   previewValue(4),
			Percent:     previewValue(62.0),
			Detail:      previewValue(l10n.TR("preview.polystrata.stage.build_metal_inference")),
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/3e1642161198c50ff1abed4046f4e4f476099dbb.tar.gz",
			OutName:     "polystrata-preview",
			OutDir:      "/preview/engines/polystrata-preview",
			LogPath:     previewValue("/preview/engines/polystrata-preview/.inferyard-setup.log"),
			StartedAt:   "2026-10-17T00:00:00Z",
		},
		{
			TaskId:      "preview-polystrata-blocked",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen · Q2_0",
			State:       "failed",
			Stage:       "preflight",
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/3e1642161198c50ff1abed4046f4e4f476099dbb.tar.gz",
			OutName:     "polystrata-blocked",
			OutDir:      "/preview/engines/polystrata-blocked",
			Error:       blockerErr,
			StartedAt:   "2026-10-16T23:55:00Z",
		},
	}
}

func previewDownloadsPolystrataModel(s *viewState) {
	s.selected = "downloads"
	s.registry.Engines = nil
	s.registry.Models = nil
	totalBytes := int64(37623912858) // 35.04 GiB
	s.downloads = []core.DownloadTask{
		{
			TaskId:          "preview-polystrata-model-live",
			Kind:            "polystrata",
			DisplayName:     "PolyStrata · qwen · Q2_0",
			State:           "downloading",
			Stage:           "downloading_model",
			Step:            previewValue(4),
			StepTotal:       previewValue(4),
			DownloadedBytes: 4456028569, // 4.15 GiB
			TotalBytes:      previewValue(totalBytes),
			BytesPerSecond:  previewValue(22502441.0), // 21.46 MiB/s
			Detail:          previewValue("Qwen3.8-Flash-Next-GSQ-RCO-Q2_0-00001-of-00002.gguf"),
			SourceUrl:       "https://github.com/simplexai-labs/PolyStrata/archive/3e1642161198c50ff1abed4046f4e4f476099dbb.tar.gz",
			OutName:         "polystrata-qwen-q2",
			OutDir:          "/preview/engines/polystrata-qwen-q2",
			LogPath:         previewValue("/preview/engines/polystrata-qwen-q2/.inferyard-setup.log"),
			StartedAt:       "2026-10-17T00:00:00Z",
		},
	}
}

func previewDownloadsActiveAndFailed(s *viewState) {
	s.selected = "downloads"
	engineTotal := int64(11517506)
	s.downloads = []core.DownloadTask{
		{
			TaskId:          "preview-engine",
			Kind:            "engine",
			DisplayName:     "llama.cpp · prism-b10743-adfffbe",
			State:           "downloading",
			Stage:           "downloading",
			SourceUrl:       core.DefaultEngineURL(),
			OutName:         "preview-engine",
			OutDir:          "/preview/engines/preview-engine",
			DownloadedBytes: engineTotal * 3 / 5,
			TotalBytes:      previewValue(engineTotal),
			BytesPerSecond:  previewValue(18.5 * 1024 * 1024),
			StartedAt:       "2026-10-17T00:00:00Z",
		},
		{
			TaskId:    "preview-model",
			Kind:      "model",
			State:     "failed",
			OutName:   "preview-model",
			OutDir:    "/preview/models/preview-model",
			SourceUrl: "https://example.invalid/preview.gguf",
			Error:     core.Err("download_failed"),
		},
	}
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
	s.ready.Workspace.Initialized, s.ready.Results.Exists, s.ready.Models.Exists = false, false, false
}

func previewValue[T any](value T) *T { return &value }

func previewBase(ctx context.Context) *viewState {
	root := filepath.Join(os.TempDir(), "inferyard-ui-preview")
	model, engine := "Preview-Model-Q4.gguf", "preview-engine · llama-server"
	settings := core.UserSettings{WorkspaceRoot: previewValue(root), ResultsRoot: previewValue(filepath.Join(root, "results")), ModelsRoot: previewValue(filepath.Join(root, "models"))}
	s := &viewState{
		speed: &speedWorkspace{context: inferyard.ContextSpeed64K},
		ctx:   ctx, selected: "dashboard", loaded: true, settingsLoaded: true,
		denied: map[string]bool{}, model: model, engine: engine, discoveryLoaded: true,
		engineState: core.EngineState{State: "stopped"},
		ready: core.Readiness{
			CLI:       core.InferyardProbe{Status: "ok", Version: previewValue("preview")},
			Workspace: core.DirectoryReadiness{Path: settings.WorkspaceRoot, Exists: true, Initialized: true},
			Results:   core.DirectoryReadiness{Path: settings.ResultsRoot, Exists: true},
			Models:    core.DirectoryReadiness{Path: settings.ModelsRoot, Exists: true},
		},
		settings: core.SettingsResponse{Settings: settings, Effective: core.EffectiveSettings{
			WorkspaceRoot: settings.WorkspaceRoot, ResultsRoot: settings.ResultsRoot, ModelsRoot: settings.ModelsRoot,
			Theme: core.ThemeSystem,
		}},
		registry: core.Registry{
			Models: []core.ModelAsset{
				{
					Name: model, Format: "gguf", Available: true, Architecture: "llama",
					SizeBytes: previewValue(int64(4 << 30)), Path: filepath.Join(root, "models", model), Deletable: true,
				},
				{
					Name: "Preview-Small.gguf", Format: "gguf", Available: true,
					Path: filepath.Join(root, "models", "Preview-Small.gguf"), Deletable: true,
				},
			},
			Engines: []core.EngineAsset{{
				Name: engine, Family: "llama-cpp", Source: l10n.TR("preview.source.engine_dir"),
				Path: filepath.Join(root, "engines", "llama-server"), Deletable: true,
			}},
			DefaultEngine:   engine,
			TotalModelBytes: 4 << 30,
			DiskFreeBytes:   previewValue(int64(100 << 30)),
			ModelRoots:      []string{filepath.Join(root, "models")},
		},
		checks: core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{engine: {State: core.EngineCheckEligible, Version: "preview"}}},
		progress: &inferyard.WorkspaceProgress{Workspace: root, NextAction: "prepare_candidate",
			DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: previewValue(filepath.Join(root, "preflight"))},
			Bundles: []inferyard.BundleProgress{
				{Bundle: inferyard.DefaultBundleCore, Phase: "candidate_missing", NextAction: "prepare_candidate"},
				{Bundle: inferyard.DefaultBundlePelican, Phase: "candidate_missing", NextAction: "prepare_candidate"},
			}},
		reports: []core.ReportSummary{previewReportSummary("preview-report")},
	}
	s.loadSettings(s.settings)
	seeded := core.RunSummary{RunId: "preview-run", Path: "/preview/results/preview-run", ModelName: previewValue(model), Bundle: previewValue(inferyard.DefaultBundleCore), Status: "complete", Sealed: true, Completeness: previewValue("complete")}
	seeded.Score = &core.RunScore{Passed: 7, Valid: 13, Rate: previewValue(7.0 / 13)}
	s.history.StandaloneRuns = []core.RunSummary{seeded}
	s.device.Snapshot = json.RawMessage(`{"hardware":{"cpu_model":"Preview CPU","logical_cpus":14,"architecture":"arm64","apple_silicon":true,"cpu_features":{"avx2":false,"avx512f":false},"memory_total_bytes":51539607552,"memory_available_bytes":32212254720,"disk_free_bytes":161061273600,"ac_online":true,"gpu":{"devices":[{"name":"Preview GPU","vendor":"apple","arch_family":"Apple Silicon","cores":20,"metal_supported":true,"missing":{"memory_total_bytes":"shared_host_memory","memory_free_bytes":"not_measured"}}]},"missing":{"temperature":"unsupported"}},"starter_recommendation":{"definition":"starter-recommendation.v1","status":"recommended","engine":"polystrata","backend":"metal","model_id":"polystrata-qwen36-ud-q4-k-m","model_title":"Qwen3.6 35B-A3B (UD-Q4_K_M)","family":"qwen36","size":"UD-Q4_K_M","engine_core":"llama","model_format":"gguf","fit_level":"fits","recommended_context":32768,"estimated_download_bytes":23085449216,"estimated_ram_bytes":25769803776,"gpu":{"name":"Preview GPU","vendor":"apple","arch_family":"Apple Silicon","unified_memory_bytes":51539607552},"alternatives":[{"engine":"llama-cpp","backend":"metal","model_id":"model-qwen3-0.6b-q8","model_title":"Qwen3-0.6B-Q8_0 (GGUF)","model_format":"gguf","fit_level":"fits","recommended_context":8192,"estimated_download_bytes":639446688,"estimated_ram_bytes":2147483648}]}}`)
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
	s.tasks = []core.RunTask{{TaskId: "preview-run", State: "running", Bundle: previewValue(inferyard.DefaultBundleCore), RunPath: previewValue("/preview/results/preview-run")}}
	s.runProgress = &core.RunProgress{Done: 42, Total: previewValue(int64(120)), CurrentCaseId: previewValue("preview-case-043")}
}

func previewCompleted(s *viewState) {
	s.task.stage, s.task.report = "done", "preview-report"
	s.progress.NextAction, s.progress.DefaultScopeComplete = "done", true
	s.progress.Report = inferyard.ProgressReport{Status: "present", Path: previewValue("preview-report"), Runs: []string{"preview-run"}}
	for i := range s.progress.Bundles {
		s.progress.Bundles[i].Phase, s.progress.Bundles[i].NextAction = "run_complete", "none"
	}
}

func previewReportSummary(id string) core.ReportSummary {
	index := previewReportFixture("")
	summary := core.ReportSummary{
		ReportId: id, Path: "/preview/reports/" + id,
		ModifiedAt: previewValue("2026-01-01T00:00:00Z"),
		RunIds:     []string{"preview-run"}, RunPaths: []*string{previewValue("/preview/results/preview-run")},
	}
	for _, run := range index.Runs {
		summary.RunSummaries = append(summary.RunSummaries, core.ReportListRun{
			RunID: run.Source.RunID, Model: run.Model, BundleLabel: run.Facets.BundleLabel,
			Title: run.Profile.Title, Quality: run.Dashboard.Quality,
		})
	}
	return summary
}

func previewReportFixture(reference string) *core.ReportIndex {
	index, err := inferyard.ParseStudioReport(previewReportJSON)
	if err != nil {
		panic("invalid bundled UI preview report: " + err.Error())
	}
	if reference == "pelican-report" {
		run := &index.Runs[0]
		run.Source.RunID = "pelican-preview-run"
		run.Facets.BundleLabel = inferyard.DefaultBundlePelican + " · 1.0.0"
		zero := core.ReportCounts{Numerator: previewValue(int64(0)), Denominator: previewValue(int64(0)), Excluded: previewValue(int64(0))}
		run.Dashboard.Quality = core.ReportQuality{StudioCounts: zero, Complete: false, Reason: previewValue("quality_incomplete_or_not_applicable")}
		run.Dashboard.Categories = []core.ReportCategory{}
		latency := 2480.0
		run.Requests = []core.ReportRequest{{
			CaseID:        "svg-pelican-01",
			CategoryLabel: "svg",
			State:         "completed",
			Quality:       previewValue("not_applicable"),
			LatencyMS:     &latency,
		}}
		run.Summary.CompletionRate = core.ReportCounts{Numerator: previewValue(int64(1)), Denominator: previewValue(int64(1)), Excluded: previewValue(int64(0))}
		run.Dashboard.Latency = core.ReportLatency{
			SampleCount: previewValue(int64(1)),
			Excluded:    previewValue(int64(0)),
			Min:         &latency,
			P50:         &latency,
			Max:         &latency,
		}
		run.SVGOutputs = previewPelicanOutputs()
		return &index
	}
	if reference == "long-report" {
		run := &index.Runs[0]
		for i := range 40 {
			run.Requests = append(run.Requests, core.ReportRequest{CaseID: fmt.Sprintf("preview-case-%03d", i), CategoryLabel: l10n.TR("preview.fixture.category"), State: "completed"})
		}
		run.Dashboard.Latency.Excluded = previewValue(int64(len(run.Requests)) - *run.Dashboard.Latency.SampleCount)
	}
	return &index
}

func previewDetailFixture(id string) *core.RunTaskDetail {
	return &core.RunTaskDetail{
		Task: core.RunTask{TaskId: id, Kind: "run", State: "running", Bundle: previewValue(inferyard.DefaultBundleCore), RunId: previewValue("preview-run"), RunPath: previewValue("/preview/results/preview-run")},
		Progress: &core.RunProgress{Done: 42, Total: previewValue(int64(120)), Phase: previewValue("formal"), CurrentCaseId: previewValue("preview-case-043"), Cases: []core.CaseProgress{
			{CaseId: "preview-case-001", State: "completed", QualityState: previewValue("pass"), DurationMs: previewValue(1240.0)},
			{CaseId: "preview-case-002", State: "failed", ErrorCategory: previewValue("preview_failure")},
		}},
		Resources: append(append(
			previewResourceSamples(1, "2026-10-17T00:00:00Z", 10<<30, 2<<30, 120),
			previewResourceSamples(6, "2026-10-17T00:00:01Z", 9<<30, 3<<30, 240)...),
			previewResourceSamples(11, "2026-10-17T00:00:02Z", 10<<30, 2<<30, 180)...),
	}
}
