package pages

import (
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
		{"device_check", "运行设备检查", 1},
		{"prepare_candidate", "引擎文件绝对路径", 2},
		{"bind", "绑定已有进程", 3},
		{"run", "开始运行", 4},
		{"resume", "继续未完成的运行", 4},
		{"report", "生成报告并核验", 5},
		{"verify", "报告已生成，尚未核验", 5},
		{"done", "已核验 verified", 6},
	} {
		t.Run(tc.action, func(t *testing.T) {
			s := testState()
			s.progress = &inferyard.WorkspaceProgress{NextAction: tc.action, Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", NextAction: tc.action}}}
			tt := ui.NewTester(s.view, 1120, 2400)
			if s.step() != tc.step || !tt.HasText(tc.want) {
				t.Fatal(s.step(), tt.Texts())
			}
			if tc.action == "verify" {
				if !tt.HasText("核验报告") {
					t.Fatal("verify action missing")
				}
				if _, ok := tt.Find("阶段步骤 6"); !ok {
					t.Fatal("verify track must be stage six")
				}
			}
			for _, other := range []string{"运行设备检查", "引擎文件绝对路径", "绑定已有进程", "开始运行", "继续未完成的运行", "生成报告并核验", "已核验 verified"} {
				if other != tc.want && tt.HasText(other) {
					t.Fatalf("unexpected stage: %s", other)
				}
			}
			if !tt.HasText("任务队列") || !tt.HasText("运行历史") {
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
	s.enginePreset = true
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
	if s.modelURL != core.DefaultModelURL || s.engineURL != core.DefaultEngineURL() || s.engine != "/manual/engine" || !s.enginePreset {
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
	if tt.HasText("生成报告并核验") {
		t.Fatal("premature report guide")
	}
	s.progress.Bundles[1].Phase = "run_complete"
	tt.Frame()
	if !tt.HasText("生成报告并核验") || !tt.HasText("选择来源 run（1–100 个）") {
		t.Fatal(tt.Texts())
	}
	s.progress.DefaultScopeComplete = true
	tt.Frame()
	if !tt.HasText("已核验 verified") || tt.HasText("生成报告并核验") {
		t.Fatal(tt.Texts())
	}
}

func TestDeviceGuidancePreservesSnapshot(t *testing.T) {
	s := testState()
	s.selected = "device"
	s.progress.NextAction = "device_check"
	tt := ui.NewTester(s.view, 1120, 1600)
	if !tt.HasText("检查设备") || !tt.HasText("尚无设备快照") {
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
	for _, code := range []string{"unsupported_platform", "device_preflight_blocked", "blocked", "preflight_missing", "incomplete", "run_incomplete", "cancelled", "output_exists", "settings_incomplete", "cli_unavailable", "install_unsupported", "download_failed", "install_checksum_failed", "install_unpack_failed", "install_failed", "install_version_unknown", "engine_unpack_failed", "engine_binary_missing", "cli_outdated", "cli_error", "invalid_request", "not_found", "conflict", "internal_error", "error", "unknown"} {
		if human(code) == generic || human(code) == "" {
			t.Fatalf("unmapped code %s", code)
		}
	}
}
