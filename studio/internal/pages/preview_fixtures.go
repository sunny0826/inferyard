//go:build ui_debug && !release

package pages

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

//go:embed testdata/report-index.json
var previewReportJSON []byte

var previewScenes = []struct {
	label string
	apply func(*viewState)
}{
	{"任务 · 准备", func(s *viewState) {
		s.progress = nil
		s.history, s.downloads, s.reports = core.RunHistoryResponse{}, nil, nil
		s.ready.CLI.Status = "not_found"
		s.ready.Workspace.Initialized, s.ready.Results.Exists, s.ready.Models.Exists = false, false, false
	}},
	{"任务 · 运行中", previewRunning},
	{"任务 · 已完成", previewCompleted},
	{"任务 · 失败", func(s *viewState) {
		s.task = benchmarkTask{stage: "bind", start: startChainResult{candidate: "/preview/candidate"}, failure: core.Err("engine_start_failed")}
		s.engineState.State = "failed"
	}},
	{"运行 · 实时详情", func(s *viewState) { previewRunning(s); s.selectTask("preview-run") }},
	{"报告 · 列表", func(s *viewState) { s.selected = "reports" }},
	{"报告 · 详情", func(s *viewState) { s.openReport("preview-report") }},
	{"报告 · 长表与缺测", func(s *viewState) { s.openReport("long-report") }},
	{"报告 · 读取失败", func(s *viewState) { s.openReport("broken-report") }},
	{"空数据", previewEmpty},
	{"模型 · 可用组合", func(s *viewState) { s.selected = "models" }},
	{"下载 · 进行中与失败", func(s *viewState) { s.selected = "downloads" }},
	{"设备 · 含缺测", func(s *viewState) { s.selected = "device" }},
	{"设置 · 路径与目录名", func(s *viewState) { s.selected = "settings" }},
	{"专项 · 历史恢复示例", previewSpeedHistory},
}

func previewValue[T any](value T) *T { return &value }

func previewBase(ctx context.Context) *viewState {
	root := filepath.Join(os.TempDir(), "inferyard-ui-preview")
	model, engine := "Preview-Model-Q4.gguf", "preview-engine · llama-server"
	settings := core.UserSettings{WorkspaceRoot: previewValue(root), ResultsRoot: previewValue(filepath.Join(root, "results")), ModelsRoot: previewValue(filepath.Join(root, "models"))}
	s := &viewState{
		speed: &speedWorkspace{context: 65536},
		ctx:   ctx, selected: "dashboard", loaded: true, settingsLoaded: true,
		denied: map[string]bool{}, chartIndex: -1, model: model, engine: engine, discoveryLoaded: true,
		candidateMatches: func(string, string, string) (bool, bool) { return true, true },
		engineState:      core.EngineState{State: "stopped"},
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
			Models:  []core.ModelAsset{{Name: model, Available: true, Architecture: "llama", SizeBytes: previewValue(int64(4 << 30))}, {Name: "Preview-Small.gguf", Available: true}},
			Engines: []core.EngineAsset{{Name: engine, Source: "模拟引擎目录"}}, DefaultEngine: engine,
		},
		checks: core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{engine: {State: core.EngineCheckEligible, Version: "preview"}}},
		progress: &inferyard.WorkspaceProgress{Workspace: root, NextAction: "prepare_candidate",
			DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: previewValue(filepath.Join(root, "preflight"))},
			Bundles: []inferyard.BundleProgress{
				{Bundle: "zh-core", Phase: "candidate_missing", NextAction: "prepare_candidate"},
				{Bundle: "zh-svg-pelican", Phase: "candidate_missing", NextAction: "prepare_candidate"},
			}},
		reports: []core.ReportSummary{{ReportId: "preview-report", Path: "/preview/reports/preview-report", RunIds: []string{"preview-run"}}},
	}
	s.loadSettings(s.settings)
	s.history.StandaloneRuns = []core.RunSummary{{RunId: "preview-run", Path: "/preview/results/preview-run", ModelName: previewValue(model), Bundle: previewValue("zh-core"), Status: "complete", Sealed: true}}
	s.downloads = []core.DownloadTask{
		{TaskId: "preview-engine", Kind: "engine", State: "downloading", OutName: "preview-engine", OutDir: "/preview/engines/preview-engine"},
		{TaskId: "preview-model", Kind: "model", State: "failed", OutName: "preview-model", OutDir: "/preview/models/preview-model", SourceUrl: "https://example.invalid/preview.gguf", Error: core.Err("download_failed")},
	}
	s.device.Snapshot = json.RawMessage(`{"hardware":{"cpu_model":"Preview CPU","logical_cpus":14,"architecture":"arm64","memory_total_bytes":25769803776,"memory_available_bytes":10737418240,"disk_free_bytes":107374182400,"ac_online":true,"gpu":{"devices":[{"name":"Preview GPU","cores":16,"metal_supported":true,"missing":{"memory_total_bytes":"shared_host_memory","memory_free_bytes":"not_measured"}}]},"missing":{"temperature":"unsupported"}}}`)
	return s
}

func previewRunning(s *viewState) {
	s.task.stage = "run"
	s.progress.NextAction = "run"
	s.progress.Bundles[0].Phase, s.progress.Bundles[0].NextAction = "run_incomplete", "run"
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857", Candidate: "/preview/candidate"}
	s.tasks = []core.RunTask{{TaskId: "preview-run", State: "running", Bundle: previewValue("zh-core"), RunPath: previewValue("/preview/results/preview-run")}}
	s.runProgress = &core.RunProgress{Done: 42, Total: previewValue(int64(120)), CurrentCaseId: previewValue("preview-case-043")}
}

func previewCompleted(s *viewState) {
	s.task.stage, s.task.report = "done", "preview-report"
	s.progress.NextAction, s.progress.DefaultScopeComplete = "done", true
	for i := range s.progress.Bundles {
		s.progress.Bundles[i].Phase, s.progress.Bundles[i].NextAction = "run_complete", "none"
	}
}

func previewEmpty(s *viewState) {
	s.selected, s.progress = "reports", nil
	s.history, s.registry = core.RunHistoryResponse{}, core.Registry{}
	s.reports, s.downloads, s.device.Snapshot = nil, nil, nil
	s.model, s.engine = "", ""
	s.ready = core.Readiness{CLI: core.InferyardProbe{Status: "not_found"}}
}

func previewReportFixture(reference string) *core.ReportIndex {
	var index core.ReportIndex
	if err := json.Unmarshal(previewReportJSON, &index); err != nil {
		panic("invalid bundled UI preview report: " + err.Error())
	}
	if reference == "long-report" {
		run := &index.Runs[0]
		for i := range 40 {
			run.Requests = append(run.Requests, core.ReportRequest{CaseID: fmt.Sprintf("preview-case-%03d", i), CategoryLabel: "模拟分类", State: "completed"})
		}
	}
	return &index
}

func previewDetailFixture(id string) *core.RunTaskDetail {
	return &core.RunTaskDetail{
		Task: core.RunTask{TaskId: id, Kind: "run", State: "running", Bundle: previewValue("zh-core"), RunId: previewValue("preview-run"), RunPath: previewValue("/preview/results/preview-run")},
		Progress: &core.RunProgress{Done: 42, Total: previewValue(int64(120)), Phase: previewValue("formal"), CurrentCaseId: previewValue("preview-case-043"), Cases: []core.CaseProgress{
			{CaseId: "preview-case-001", State: "completed", QualityState: previewValue("pass"), DurationMs: previewValue(1240.0)},
			{CaseId: "preview-case-002", State: "failed", ErrorCategory: previewValue("preview_failure")},
		}},
		Resources: []core.ResourceSample{
			{Utc: "2026-10-17T00:00:00Z", MemAvailableBytes: previewValue(int64(10 << 30)), ServiceRssBytes: previewValue(int64(2 << 30)), ServiceCpuPercent: previewValue(120.0)},
			{Utc: "2026-10-17T00:00:01Z", MemAvailableBytes: previewValue(int64(9 << 30)), ServiceRssBytes: previewValue(int64(3 << 30)), ServiceCpuPercent: previewValue(240.0)},
			{Utc: "2026-10-17T00:00:02Z", MemAvailableBytes: previewValue(int64(10 << 30)), ServiceRssBytes: previewValue(int64(2 << 30)), ServiceCpuPercent: previewValue(180.0)},
		},
	}
}
