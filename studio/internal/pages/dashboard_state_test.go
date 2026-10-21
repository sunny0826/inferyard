package pages

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestDashboardStages(t *testing.T) {
	for _, tc := range []struct {
		action, want string
		step         int
	}{
		{"device_check", "正在准备设备检查", 1},
		{"prepare_candidate", "候选引擎", 2},
		{"bind", "启动服务", 3},
		{"run", "开始运行", 4},
		{"resume", "继续未完成的运行", 4},
		{"report", "在报告页生成报告。", 5},
		{"verify", "请在报告页核验报告。", 5},
		{"done", "运行已完成", 6},
	} {
		t.Run(tc.action, func(t *testing.T) {
			s := testState()
			s.registry = modelRegistryFixture()
			s.progress = &inferyard.WorkspaceProgress{NextAction: tc.action, Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", NextAction: tc.action}}}
			tt := ui.NewTester(s.view, 1120, 2400)
			if s.step() != tc.step || !tt.HasText(tc.want) {
				t.Fatal(s.step(), tt.Texts())
			}
			if tc.action == "verify" {
				if _, found := tt.Find("核验报告"); found {
					t.Fatal("task page must not execute verify")
				}
				if _, ok := tt.Find("阶段步骤 6"); !ok {
					t.Fatal("verify track must be stage six")
				}
			}
			for _, other := range []string{"运行设备检查", "候选引擎", "高级：手动绑定", "开始运行", "继续未完成的运行", "生成报告并核验", "已核验 verified"} {
				if other != tc.want && tt.HasText(other) {
					t.Fatalf("unexpected stage: %s", other)
				}
			}
			if tt.HasText("任务队列") || tt.HasText("运行历史") {
				t.Fatal(tt.Texts())
			}
		})
	}
	s := testState()
	s.progress = nil
	if s.step() != 0 {
		t.Fatal("missing progress must show stage zero")
	}
	s.tasks = []core.RunTask{{State: "running"}}
	if s.step() != 4 {
		t.Fatal("active task must take priority")
	}
}

func TestGuidedCommandsPreserveOutputAndResume(t *testing.T) {
	s := testState()
	s.engine = "/engine path/it's server"
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", NextCommand: ptr("inferyard config create --out '/frozen/new output' --engine old")}
	args, err := inferyard.SplitCommand(s.nextCommand())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, []string{"config", "create", "--out", "/frozen/new output", "--engine", s.engine}) {
		t.Fatal(args)
	}
	s.progress.NextAction = "device_check"
	s.progress.NextCommand = ptr("inferyard device-check --out /frozen/device")
	s.ready.Models.Path = ptr("")
	if strings.Contains(s.nextCommand(), "--models-root") {
		t.Fatal("empty models root injected")
	}
	s.ready.Models.Path = ptr("/local models")
	args, err = inferyard.SplitCommand(s.nextCommand())
	if err != nil || !slices.Equal(args, []string{"device-check", "--out", "/frozen/device", "--models-root", "/local models"}) {
		t.Fatal(args, err)
	}
	s.progress.NextAction = "run"
	s.progress.NextCommand = ptr("inferyard run --out /wrong")
	s.progress.Bundles = []inferyard.BundleProgress{{Phase: "run_incomplete", NextAction: "resume", NextCommand: ptr("inferyard resume --from /previous --out /resume-output")}}
	args, err = inferyard.SplitCommand(s.nextCommand())
	if err != nil || !slices.Equal(args, []string{"resume", "--from", "/previous", "--out", "/resume-output"}) {
		t.Fatal(args, err)
	}
}

func TestDashboardRefreshClearsOnlyRefreshFailure(t *testing.T) {
	s := testState()
	s.failure = &core.ApiError{Code: "invalid_request", Message: human("invalid_request")}
	snapshot := dashboardSnapshot{candidate: "/candidate/one", startup: core.Startup{Args: []string{"--host", "::1", "--port", "48857"}}}
	s.applyDashboard(snapshot, core.Err("cli_error"))
	if s.refreshFailure != human("cli_error") || s.bindEndpoint != "http://[::1]:48857" {
		t.Fatalf("failure=%q endpoint=%q", s.refreshFailure, s.bindEndpoint)
	}
	s.process, s.bindEndpoint, s.endpoint = "12 · custom", "http://127.0.0.1:1234", "http://127.0.0.1:9999"
	s.applyDashboard(snapshot, nil)
	if s.refreshFailure != "" || s.failure == nil || s.process != "12 · custom" || s.bindEndpoint != "http://127.0.0.1:1234" {
		t.Fatal("refresh discarded edits or operation error")
	}
	snapshot.candidate = "/candidate/two"
	s.applyDashboard(snapshot, nil)
	if s.process != "" || s.bindEndpoint != "http://[::1]:48857" || s.endpoint != "http://127.0.0.1:9999" {
		t.Fatal("candidate transition mixed binding and resume inputs")
	}
	s.failure = nil
	tt := ui.NewTester(s.view, 1120, 1600)
	if tt.HasText(human("cli_error")) {
		t.Fatal("recovered refresh failure still visible")
	}
}

func TestAssetDefaultsApplyOnlyOnce(t *testing.T) {
	s := testState()
	s.engine = "/manual/engine"
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != core.DefaultModelURL() || s.engineURL != core.DefaultEngineURL() || s.engine != "/manual/engine" || !s.assetsPreset {
		t.Fatal("initial defaults")
	}
	s.modelURL, s.engineURL, s.engine = "", "", ""
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != "" || s.engineURL != "" || s.engine != "" {
		t.Fatal("refresh overwrote manual clearing")
	}
	s = testState()
	s.modelURL, s.engineURL, s.engine = "manual-model", "manual-engine", "/manual"
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != "manual-model" || s.engineURL != "manual-engine" {
		t.Fatal("refresh overwrote manual URLs")
	}
}

func TestReportGuidanceRequiresAllBundlesSealed(t *testing.T) {
	s := testState()
	s.selected = "reports"
	tt := ui.NewTester(s.view, 1120, 1600)
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("premature verify")
	}
	s.progress.NextAction = "verify"
	tt.Frame()
	if _, ok := tt.Find("核验"); !ok {
		t.Fatal(tt.Texts())
	}
	s.progress.Report.Status = "verified"
	tt.Frame()
	if !tt.HasText("已核验") {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("verified report offers verify")
	}
	s.progress.Report.Status = "generated"
	s.progress.NextAction = "report"
	tt.Frame()
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("report action offers verify")
	}
}

func TestDeviceGuidancePreservesSnapshot(t *testing.T) {
	s := testState()
	s.selected = "device"
	s.progress.NextAction = "device_check"
	tt := ui.NewTester(s.view, 1120, 1600)
	if slices.Contains(tt.Texts(), "检查设备") || !tt.HasText("尚无设备快照") ||
		!slices.Contains(tt.Texts(), "刷新设备") {
		t.Fatal(tt.Texts())
	}
}

func TestManagedEngineStopDisabledWhileRunning(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "running", Pid: 42}
	s.tasks = []core.RunTask{{State: "running"}}
	tt := ui.NewTester(s.serviceView, 1120, 1600)
	// A disabled click must not reach the service (deliberately absent in this fixture).
	_ = tt.Click("停止服务")
	if s.busy {
		t.Fatal("stop action started during a run")
	}
}

func TestHumanAllKnownCodes(t *testing.T) {
	generic := human("unrecognized")
	for _, code := range []string{"service_discovery_invalid_response", "service_discovery_error", "service_discovery_unsupported_platform",
		"listener_enumeration_failed", "listener_table_incomplete", "listener_ownership_incomplete", "process_identity_unavailable",
		"models_timeout", "models_unreachable", "models_http_1xx", "models_http_3xx", "models_http_4xx", "models_http_5xx",
		"models_invalid_response", "models_empty", "models_response_too_large", "health_timeout", "health_unreachable",
		"startup_arguments_mismatch", "service_process_unavailable", "binding_progress_changed",
		"engine_orphaned", "engine_owner_invalid", "engine_owner_write_failed", "engine_owner_remove_failed", "engine_stop_failed", "unsupported_platform", "device_preflight_blocked", "blocked", "preflight_missing", "incomplete", "run_incomplete", "cancelled", "output_exists", "settings_incomplete", "cli_unavailable", "install_unsupported", "download_failed", "install_checksum_failed", "install_unpack_failed", "install_failed", "install_version_unknown", "engine_unpack_failed", "engine_binary_missing", "cli_outdated", "cli_error", "invalid_request", "not_found", "conflict", "internal_error", "error", "unknown",
		"engine_check_error", "engine_check_invalid_response", "invalid_library_manifest", "unsupported_engine_build_requires_adapter_validation",
		"unsupported_runtime_profile", "runtime_receipt_invalid", "runtime_profile_mismatch", "runtime_asset_mismatch", "runtime_receipt_missing",
		"engine_version_query_failed", "package_resource_invalid", "io_error", "invalid_input", "candidate_mismatch",
		"engine_exited", "engine_start_timeout", "engine_start_failed", "engine_port_in_use",
		"single_run_resume_not_supported", "duration_resume_requires_new_window", "batch_plan_not_finished"} {
		if human(code) == generic || human(code) == "" || strings.Contains(human(code), "尚未识别的原因码") {
			t.Fatalf("unmapped code %s", code)
		}
	}
}

func TestDashboardPrepareSkipsInitializedWorkspace(t *testing.T) {
	for _, initialized := range []bool{true, false} {
		r := core.Readiness{CLI: core.InferyardProbe{Status: "ok"}, Workspace: core.DirectoryReadiness{Path: ptr(t.TempDir()), Exists: true, Initialized: initialized}}
		installs, provisions, inits := 0, 0, 0
		e := prepareEnvironmentSteps(context.Background(), func() core.Readiness { return r }, func(context.Context) (string, *core.ApiError) { installs++; return "", nil }, func() *core.ApiError { provisions++; r.Results.Exists = true; r.Models.Exists = true; return nil }, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			inits++
			args, _ := inferyard.SplitCommand(q.Command)
			if len(args) != 3 || args[0] != "init" || args[1] != "--out" || args[2] == *r.Workspace.Path {
				t.Fatal(args)
			}
			r.Workspace.Initialized = true
			return core.WizardResult{}, nil
		})
		want := 0
		if !initialized {
			want = 1
		}
		if e != nil || installs != 0 || provisions != 1 || inits != want {
			t.Fatal(e, installs, provisions, inits)
		}
	}
}

func TestDashboardNewTaskClearsDetailKeepsSelection(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.model, s.engine = "model", "engine"
	s.detailID = "old"
	s.resumeSource = &core.RunSummary{Path: "old"}
	s.task.report = "old-report"
	ctx, cancel := context.WithCancel(context.Background())
	s.taskCancel = cancel
	s.newBenchmark()
	if ctx.Err() == nil || s.detailID != "" || s.resumeSource != nil || s.task.report != "" || s.model != "model" || s.engine != "engine" {
		t.Fatal(s.task)
	}
	tt := ui.NewTester(s.dashboardView, 1120, 1800)
	if !tt.HasText("运行 benchmark") || !s.selectedBundles["zh-core"] {
		t.Fatal(tt.Texts())
	}
}

func TestDashboardCancelWaitsForRunningAndQueued(t *testing.T) {
	tasks := []core.RunTask{{TaskId: "running", State: "running"}, {TaskId: "queued", State: "queued"}}
	called := map[string]bool{}
	reads := 0
	e := cancelBenchmarkRuns(context.Background(), func() []core.RunTask {
		reads++
		if reads > 2 {
			tasks[0].State = "cancelled"
		}
		return tasks
	}, func(id string) (core.RunTask, *core.ApiError) {
		called[id] = true
		if id == "running" {
			tasks[0].State = "cancelling"
		} else {
			tasks[1].State = "cancelled"
		}
		return core.RunTask{}, nil
	})
	if e != nil || !called["running"] || !called["queued"] || reads < 3 {
		t.Fatal(e, called, reads)
	}
}

func TestDashboardPlanOnlyShowsBlocker(t *testing.T) {
	for _, reason := range []string{"duration_resume_requires_new_window", "batch_plan_not_finished"} {
		s := testState()
		s.progress.Bundles = []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_incomplete", NextAction: "run", NextCommand: ptr("inferyard run --plan /plan"), BlockedReason: &reason}}
		tt := ui.NewTester(s.runView, 1000, 800)
		if !tt.HasText(human(reason)) {
			t.Fatal(tt.Texts())
		}
		if _, found := tt.Find("开始运行"); found {
			t.Fatal("plan execution offered")
		}
	}
}

func TestDashboardFirstDeviceCheckHasNoModelAndRunsOnce(t *testing.T) {
	s := testState()
	s.model = "chosen.gguf"
	s.ready.Models.Path = ptr("/models")
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/workspace", NextAction: "device_check", NextCommand: ptr("inferyard device-check --json --out /preflight"), DevicePreflight: inferyard.ProgressPreflight{Status: "missing"}}
	q, ok := s.takeWorkspaceDeviceRequest()
	args, _ := inferyard.SplitCommand(q.Command)
	if !ok || q.ModelName != "" || !slices.Contains(args, "--models-root") || !slices.Contains(args, "/models") || slices.Contains(args, "--model") {
		t.Fatal(q, args)
	}
	if _, ok = s.takeWorkspaceDeviceRequest(); ok {
		t.Fatal("repeated workspace check")
	}
	s.deviceChecked = ""
	s.progress.DevicePreflight.Status = "ok"
	if _, ok = s.takeWorkspaceDeviceRequest(); ok {
		t.Fatal("checked an existing preflight")
	}
}

func TestDashboardCancelTerminalRaceStillCancelsRemaining(t *testing.T) {
	tasks := []core.RunTask{{TaskId: "finishing", State: "running"}, {TaskId: "queued", State: "starting"}}
	calls := []string{}
	e := cancelBenchmarkRuns(context.Background(), func() []core.RunTask { return append([]core.RunTask(nil), tasks...) }, func(id string) (core.RunTask, *core.ApiError) {
		calls = append(calls, id)
		if id == "finishing" {
			tasks[0].State = "done"
			return core.RunTask{}, core.Conflict("invalid_state")
		}
		tasks[1].State = "cancelled"
		return tasks[1], nil
	})
	if e != nil || !slices.Equal(calls, []string{"finishing", "queued"}) || tasks[1].State != "cancelled" {
		t.Fatal(e, calls, tasks)
	}
}

func TestDashboardNewTaskWaitsForLastEnqueuedRun(t *testing.T) {
	if benchmarkResetReady(true, nil) {
		t.Fatal("reset while producer is still active")
	}
	late := []core.RunTask{{TaskId: "late", State: "starting"}}
	if benchmarkResetReady(false, late) {
		t.Fatal("reset missed the last enqueued run")
	}
	late[0].State = "cancelled"
	if !benchmarkResetReady(false, late) {
		t.Fatal("terminal runs blocked reset")
	}
}
