package pages

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestOverviewStagesAndNavigation(t *testing.T) {
	for _, tc := range []struct {
		action, page, button string
		step                 int
	}{
		{"", "settings", "下一步：在设置页安装 CLI 或初始化工作区", 0},
		{"device_check", "device", "下一步：在设备页检查设备", 1},
		{"prepare_candidate", "runs", "下一步：在运行页创建候选", 2},
		{"bind", "runs", "下一步：在运行页启动与绑定服务", 3},
		{"run", "runs", "下一步：在运行页运行当前题包", 4},
		{"report", "reports", "下一步：在报告页生成报告并核验", 5},
		{"complete", "reports", "已完成：在报告页查看已核验报告", 6},
	} {
		t.Run(tc.page+tc.action, func(t *testing.T) {
			s := testState()
			s.progress = nil
			if tc.action != "" {
				s.progress = &inferyard.WorkspaceProgress{NextAction: tc.action, DefaultScopeComplete: tc.action == "complete"}
			}
			tt := ui.NewTester(s.view, 1120, 1600)
			if s.step() != tc.step {
				t.Fatalf("step: %d", s.step())
			}
			for i, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
				mark := ""
				if i < tc.step {
					mark = "✓  "
				}
				if !slices.Contains(tt.Texts(), mark+label) {
					t.Fatal(tt.Texts())
				}
			}
			for _, unwanted := range []string{"打开向导", "准备目录", "新工作区目录", "创建候选", "绑定进程"} {
				if slices.Contains(tt.Texts(), unwanted) {
					t.Fatalf("overview includes action %s", unwanted)
				}
			}
			if err := tt.Click(tc.button); err != nil {
				t.Fatal(err)
			}
			if s.selected != tc.page {
				t.Fatalf("navigation: %s", s.selected)
			}
		})
	}
	s := testState()
	s.progress = nil
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

func TestOnboardingRefreshClearsOnlyRefreshFailure(t *testing.T) {
	s := testState()
	s.enginePreset = true
	s.failure = &core.ApiError{Code: "invalid_request", Message: human("invalid_request")}
	snapshot := onboardingSnapshot{candidate: "/candidate/one", startup: core.Startup{Args: []string{"--host", "::1", "--port", "48857"}}}
	s.applyOnboarding(snapshot, core.Err("cli_error"))
	if s.refreshFailure != human("cli_error") || s.bindEndpoint != "http://[::1]:48857" {
		t.Fatalf("failure=%q endpoint=%q", s.refreshFailure, s.bindEndpoint)
	}
	s.process, s.bindEndpoint, s.endpoint = "12 · custom", "http://127.0.0.1:1234", "http://127.0.0.1:9999"
	s.applyOnboarding(snapshot, nil)
	if s.refreshFailure != "" || s.failure == nil || s.process != "12 · custom" || s.bindEndpoint != "http://127.0.0.1:1234" {
		t.Fatal("refresh discarded edits or operation error")
	}
	snapshot.candidate = "/candidate/two"
	s.applyOnboarding(snapshot, nil)
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
	s.applyOnboarding(onboardingSnapshot{}, nil)
	if s.modelURL != core.DefaultModelURL || s.engineURL != core.DefaultEngineURL() || s.engine != "/manual/engine" || !s.enginePreset {
		t.Fatal("initial defaults")
	}
	s.modelURL, s.engineURL, s.engine = "", "", ""
	s.applyOnboarding(onboardingSnapshot{}, nil)
	if s.modelURL != "" || s.engineURL != "" || s.engine != "" {
		t.Fatal("refresh overwrote manual clearing")
	}
	s = testState()
	s.modelURL, s.engineURL, s.engine = "manual-model", "manual-engine", "/manual"
	s.applyOnboarding(onboardingSnapshot{}, nil)
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
