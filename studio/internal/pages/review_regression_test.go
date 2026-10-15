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

func TestBindingRecoveryInBindStage(t *testing.T) {
	for _, phase := range []string{"run_missing", "run_incomplete", "bound_missing"} {
		s := testState()
		s.selected = "dashboard"
		s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: phase, CandidatePath: ptr("/candidate"), BoundPath: ptr("/bound")}}}
		s.bindingStale = true
		s.engineState = core.EngineState{State: "running", Pid: 42}
		tt := ui.NewTester(s.view, 1120, 2400)
		start := "启动服务"
		if phase == "bound_missing" {
			start = "启动并绑定"
		}
		for _, label := range []string{start, "停止服务", "重新绑定", "旧绑定已失效，请重启服务并重新绑定"} {
			if !tt.HasText(label) {
				t.Fatalf("%s lacks %s", phase, label)
			}
		}
	}
}

func TestRecoveryCardSurvivesRunPhase(t *testing.T) {
	for _, action := range []string{"run", "resume"} {
		for _, path := range []string{"candidate", "bound", "none"} {
			for _, running := range []bool{false, true} {
				s := testState()
				b := inferyard.BundleProgress{Bundle: "zh-core", Phase: "run_missing", NextAction: action}
				if path == "candidate" {
					b.CandidatePath = ptr("/candidate")
				} else if path == "bound" {
					b.BoundPath = ptr("/bound")
				}
				s.progress = &inferyard.WorkspaceProgress{NextAction: action, Bundles: []inferyard.BundleProgress{b}}
				s.bindingStale = true
				s.engineState = core.EngineState{State: "running", Pid: 42}
				if running {
					s.tasks = []core.RunTask{{State: "running"}}
				}
				tt := ui.NewTester(s.stageView, 1120, 2400)
				if path == "none" {
					if tt.HasText("模型服务") {
						t.Fatal("service card without candidate or binding")
					}
					continue
				}
				for _, label := range []string{"模型服务", "停止服务", "旧绑定已失效，请重启服务并重新绑定"} {
					if !tt.HasText(label) {
						t.Fatalf("%s/%s running=%v lacks %s", action, path, running, label)
					}
				}
				service, _ := tt.Find("模型服务")
				run, _ := tt.Find("运行当前题包")
				if service.Y >= run.Y {
					t.Fatal("service card must precede run content")
				}
				if running {
					click(t, tt, "停止服务")
					if path == "bound" {
						click(t, tt, "重新绑定")
					}
					if s.busy {
						t.Fatal("service mutation admitted while running")
					}
				} else if path == "bound" && !tt.HasText("重新绑定") {
					t.Fatal("bound run lacks recovery action")
				}
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
	command, e := s.managedBindCommand()
	if e != nil {
		t.Fatal(e)
	}
	args, err := inferyard.SplitCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"config", "bind", "--candidate", filepath.Join(candidate, "candidate.toml"), "--out", filepath.Join(root, "bound", "zh-core-2"), "--pid", "123", "--endpoint", s.engineState.Endpoint}
	if !slices.Equal(args, want) {
		t.Fatalf("%v", args)
	}
	os.MkdirAll(filepath.Join(root, "bound", "zh-core-2"), 0700)
	command, e = s.bindCommand("456", "http://localhost:48857")
	if e != nil {
		t.Fatal(e)
	}
	args, _ = inferyard.SplitCommand(command)
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
	command, e = s.managedBindCommand()
	if e != nil {
		t.Fatal(e)
	}
	args, _ = inferyard.SplitCommand(command)
	if !slices.Contains(args, "/frozen/output") {
		t.Fatal("rewrote CLI --out", args)
	}
}

func TestQuickRunManualRecoveryWithoutProgress(t *testing.T) {
	s := testState()
	s.selected, s.progress = "dashboard", nil
	tt := ui.NewTester(s.view, 1120, 1600)
	if !tt.HasText("启动测评") || !tt.HasText("暂无已绑定题包；可在下方高级选项手动指定配置路径") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "高级：手动配置路径")
	if !tt.HasText("第一配置绝对路径") || !tt.HasText("第二配置绝对路径") {
		t.Fatal(tt.Texts())
	}
	_ = tt.Click("启动运行")
	if s.busy {
		t.Fatal("empty manual paths admitted")
	}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate"}
	tt.Frame()
	if tt.HasText("启动测评") {
		t.Fatal("quick run shown for known progress without bindings")
	}
	s.progress.Bundles = []inferyard.BundleProgress{{Bundle: "zh-core", BoundPath: ptr("/bound")}}
	tt.Frame()
	if !tt.HasText("启动测评") {
		t.Fatal("quick run hidden with a bound bundle")
	}

}

func TestWorkspaceSwitchClearsEngineSession(t *testing.T) {
	s := testState()
	s.settings.Effective.WorkspaceRoot = ptr("/old")
	s.candidate, s.engine, s.process, s.bindEndpoint, s.initDir = "old", "old", "42", "old", "old"
	s.startup = core.Startup{Path: "old"}
	s.processes = []core.ProcessCandidate{{PID: 42}}
	s.engineState = core.EngineState{State: "running", Pid: 42}
	s.registry = modelRegistryFixture()
	s.model, s.bindingStale = "old.gguf", true
	s.manualFirst, s.manualSecond, s.firstBundle, s.detailID = "old", "old", "old", "old"
	s.applySettings(core.SettingsResponse{Effective: core.EffectiveSettings{WorkspaceRoot: ptr("/new")}})
	if s.candidate != "" || s.engine != "" || s.process != "" || s.bindEndpoint != "" || s.startup.Path != "" || len(s.processes) != 0 || s.engineState.Pid != 0 || len(s.registry.Engines) != 0 || s.model != "" || s.bindingStale || s.detailID != "" || s.manualFirst != "" {
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
	if tt.HasText("引擎专属进度") || tt.HasText("CLI 专属进度") {
		t.Fatal(tt.Texts())
	}
	s.selected = "downloads"
	tt.Frame()
	if !tt.HasText("引擎专属进度") || tt.HasText("CLI 专属进度") {
		t.Fatal(tt.Texts())
	}
	s.selected = "dashboard"
	tt.Frame()
	if !tt.HasText("CLI 专属进度") || tt.HasText("引擎专属进度") || tt.HasText("一键配置目录") {
		t.Fatal(tt.Texts())
	}
}
