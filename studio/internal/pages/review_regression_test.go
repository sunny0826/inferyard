package pages

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestRecoveryCardSurvivesRunPhase(t *testing.T) {
	for _, phase := range []string{"run_missing", "run_incomplete", "bound_missing"} {
		s := testState()
		s.selected = "runs"
		s.progress = &inferyard.WorkspaceProgress{NextAction: "run", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: phase, CandidatePath: ptr("/candidate"), BoundPath: ptr("/bound")}}}
		s.bindingStale = true
		s.engineState = core.EngineState{State: "running", Pid: 42}
		tt := ui.NewTester(s.view, 1120, 2400)
		for _, label := range []string{"启动服务", "停止服务", "重新绑定", "旧绑定已失效，请重启服务并重新绑定"} {
			if !tt.HasText(label) {
				t.Fatalf("%s lacks %s", phase, label)
			}
		}
	}
}

func TestRebindUsesFreshDirectoryAndCandidateFile(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidates", "zh-core-1")
	if err := os.MkdirAll(candidate, 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "bound", "zh-core-1")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(old, "config.toml")
	os.WriteFile(marker, []byte("original evidence"), 0600)
	s := testState()
	s.candidate = candidate
	s.progress = &inferyard.WorkspaceProgress{Workspace: root, NextAction: "run", NextCommand: ptr("inferyard run --config old"), Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_missing", CandidatePath: &candidate, BoundPath: &old}}}
	s.engineState = core.EngineState{State: "running", Pid: 123, Endpoint: "http://127.0.0.1:48857"}
	args, err := inferyard.SplitCommand(s.managedBindCommand())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"config", "bind", "--candidate", filepath.Join(candidate, "candidate.toml"), "--out", filepath.Join(root, "bound", "zh-core-2"), "--pid", "123", "--endpoint", s.engineState.Endpoint}
	if !slices.Equal(args, want) {
		t.Fatalf("%v", args)
	}
	os.MkdirAll(filepath.Join(root, "bound", "zh-core-2"), 0700)
	args, _ = inferyard.SplitCommand(s.bindCommand("456", "http://localhost:48857"))
	if !slices.Contains(args, filepath.Join(root, "bound", "zh-core-3")) || !slices.Contains(args, "456") {
		t.Fatal(args)
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "original evidence" {
		t.Fatal("original changed")
	}
	if *s.progress.NextCommand != "inferyard run --config old" {
		t.Fatal("rewrote next_command")
	}
	s.progress.NextAction = "bind"
	s.progress.Bundles[0].Phase = "bound_missing"
	s.progress.Bundles[0].NextCommand = ptr("inferyard config bind --candidate '/frozen/candidate.toml' --pid 1 --endpoint http://localhost --out '/frozen/output'")
	args, _ = inferyard.SplitCommand(s.managedBindCommand())
	if !slices.Contains(args, "/frozen/output") {
		t.Fatal("rewrote CLI --out", args)
	}
}

func TestManualConfigAvailableWithoutBindings(t *testing.T) {
	s := testState()
	s.selected, s.progress = "runs", nil
	tt := ui.NewTester(s.view, 1120, 1600)
	if err := tt.Click("高级：手动配置路径"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("第一配置绝对路径") || !tt.HasText("第二配置绝对路径") {
		t.Fatal(tt.Texts())
	}
	// Empty manual paths cannot invoke the deliberately absent service.
	_ = tt.Click("启动运行")
	if s.busy {
		t.Fatal("empty manual paths admitted")
	}
}

func TestWorkspaceSwitchClearsEngineSession(t *testing.T) {
	s := testState()
	s.settings.Effective.WorkspaceRoot = ptr("/old")
	s.candidate, s.engine, s.process, s.bindEndpoint, s.initDir = "old", "old", "42", "old", "old"
	s.startup = core.Startup{Path: "old"}
	s.processes = []core.ProcessCandidate{{PID: 42}}
	s.engineState = core.EngineState{State: "running", Pid: 42}
	s.enginePreset, s.bindingStale = true, true
	s.manualFirst, s.manualSecond, s.firstBundle, s.detailID = "old", "old", "old", "old"
	s.applySettings(core.SettingsResponse{Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/new")}})
	if s.candidate != "" || s.engine != "" || s.process != "" || s.bindEndpoint != "" || s.startup.Path != "" || len(s.processes) != 0 || s.engineState.Pid != 0 || s.enginePreset || s.bindingStale || s.detailID != "" || s.manualFirst != "" {
		t.Fatal("workspace retained old session")
	}
}

func TestInstallAndDownloadFeedbackStayOnTheirPages(t *testing.T) {
	s := testState()
	s.cliInstallStatus = "CLI 专属进度"
	s.downloads = []core.DownloadTask{{Kind: "engine", TaskId: "fixture", OutName: "fixture-engine", State: "failed", Error: &core.ApiError{Code: "engine_unpack_failed", Message: "引擎专属进度"}}}
	s.ready.CLI.Status = "not_found"
	s.selected = "models"
	tt := ui.NewTester(s.view, 1120, 1800)
	if tt.HasText("引擎专属进度") {
		t.Fatal("error details expanded by default", tt.Texts())
	}
	click(t, tt, "观测依据与输出")
	if !tt.HasText("引擎专属进度") || tt.HasText("CLI 专属进度") {
		t.Fatal(tt.Texts())
	}
	s.selected = "settings"
	tt.Frame()
	if !tt.HasText("CLI 专属进度") || tt.HasText("引擎专属进度") || tt.HasText("一键配置目录") {
		t.Fatal(tt.Texts())
	}
}
