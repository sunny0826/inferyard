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
)

//go:embed testdata/studio.json
var previewReportJSON []byte

var previewScenes = []struct {
	label string
	apply func(*viewState)
}{
	{"任务 · 准备", previewPreparation},
	{"任务 · 待运行", func(s *viewState) {
		s.history, s.downloads, s.reports = core.RunHistoryResponse{}, nil, nil
	}},
	{"任务 · 运行中", previewRunning},
	{"任务 · 已完成", previewCompleted},
	{"任务 · 失败", func(s *viewState) {
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
	{"首次使用 · 无资源", func(s *viewState) {
		previewPreparation(s)
		s.registry = core.Registry{}
		s.model, s.engine = "", ""
		s.checks = core.EngineCheckOverview{Supported: true}
	}},
	{"引擎 · 本地安装与目录模型", previewLocalEngines},
	{"引擎 · LM Studio 手动绑定", func(s *viewState) {
		previewLocalEngines(s)
		s.engine, s.model = "lmstudio · 模拟安装", "local.gguf"
		s.localPID, s.localEndpoint = "4001", "http://127.0.0.1:48857"
		s.discoveryLoaded = true
		s.discovery = inferyard.ServiceDiscovery{Status: "complete", Services: []inferyard.DiscoveredService{
			{PID: 4001, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:48857", Models: []string{"local.gguf", "local-weights"}}}},
		}}
	}},
	{"资源 · 密集采样与缺测", previewDenseResources},
	{"资源 · CPU 与多路温度", previewMultiSensorResources},
	{"报告 · 长表与缺测", func(s *viewState) { s.openReport("long-report") }},
	{"报告 · 读取失败", func(s *viewState) { s.openReport("broken-report") }},
	{"下载 · 进行中与失败", previewDownloadsActiveAndFailed},
	{"下载 · PolyStrata 引导安装", previewDownloadsPolystrata},
}

func previewDownloadsPolystrata(s *viewState) {
	s.selected = "downloads"
	s.registry.Engines = nil
	s.registry.Models = nil
	blockerErr := core.Err("polystrata_preflight_blocked")
	blockerErr.Details = map[string]any{
		"blockers": []map[string]any{{
			"code":        "cmake_missing",
			"message":     "需要安装 CMake 才能构建 PolyStrata。",
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
			Detail:      previewValue("编译 Metal 推理核心"),
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/v0.1.45-poly.10.tar.gz",
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
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/v0.1.45-poly.10.tar.gz",
			OutName:     "polystrata-blocked",
			OutDir:      "/preview/engines/polystrata-blocked",
			Error:       blockerErr,
			StartedAt:   "2026-10-16T23:55:00Z",
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
				Name: engine, Family: "llama-cpp", Source: "模拟引擎目录",
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
	if reference == "long-report" {
		run := &index.Runs[0]
		for i := range 40 {
			run.Requests = append(run.Requests, core.ReportRequest{CaseID: fmt.Sprintf("preview-case-%03d", i), CategoryLabel: "模拟分类", State: "completed"})
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
