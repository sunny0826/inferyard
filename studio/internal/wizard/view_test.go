package wizard

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"slices"
	"testing"
)

func strptr(s string) *string { return &s }

func TestViews(t *testing.T) {
	version := core.Version
	core.Version = "0.0.2"
	t.Cleanup(func() { core.Version = version })
	total := int64(120)
	cases := []struct {
		name  string
		state viewState
		want  string
	}{
		{"CLI unavailable", viewState{loaded: true}, "安装 InferYard CLI"},
		{"initialize", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}}}, "新工作区目录"},
		{"external service", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "bind"}, startup: core.Startup{Path: "/engine/llama-server", Args: []string{"--port", "48857"}}}, "复制启动命令"},
		{"running", viewState{loaded: true, tasks: []core.RunTask{{State: "running"}}, runProgress: &core.RunProgress{Done: 86, Total: &total}}, "86 / 120 题"},
		{"verified", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{DefaultScopeComplete: true}}, "已核验 verified"},
		{"device defaults", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "device_check", Workspace: "/tmp/ws"}, modelURL: core.DefaultModelURL, engineURL: core.DefaultEngineURL()}, "已内置可拉取的测试模型与引擎地址"},
		{"initialized outdated CLI", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}, Workspace: core.DirectoryReadiness{Path: strptr("/tmp/ws"), Exists: true, Initialized: true}}}, "工作区已初始化"},
	}
	for i := range cases {
		tc := &cases[i]
		t.Run(tc.name, func(t *testing.T) {
			tt := ui.NewTester(tc.state.view, 1000, 740)
			if !tt.HasText(tc.want) {
				t.Fatalf("missing %q: %v", tc.want, tt.Texts())
			}
			for _, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
				if !tt.HasText(label) {
					t.Fatalf("missing rail %s", label)
				}
			}
			if tc.name == "initialized outdated CLI" {
				if !tt.HasText("无法读取工作区进度——CLI 版本过旧，缺少 progress 命令，请升级 CLI 后点「刷新进度」。") {
					t.Fatalf("missing outdated notice: %v", tt.Texts())
				}
				if tt.HasText("初始化工作区") && tt.HasText("新工作区目录") {
					t.Fatal("init form must not be shown for an initialized workspace")
				}
			}
			if tc.name == "device defaults" {
				if tc.state.modelURL != core.DefaultModelURL || tc.state.engineURL != core.DefaultEngineURL() {
					t.Fatal("built-in asset URLs must be prefilled")
				}
			}
			if tc.name == "CLI unavailable" {
				if !tt.HasText("自动安装 CLI 0.0.2") {
					t.Fatalf("missing auto-install action: %v", tt.Texts())
				}
				if !tt.HasText("手动指定路径") {
					t.Fatal("missing manual path action")
				}
			}
			if tc.name == "external service" {
				if tt.HasText("endpoint") {
					t.Fatal("binding form shown before confirmation")
				}
				if err := tt.Click("复制启动命令"); err != nil {
					t.Fatal(err)
				}
				if tt.Clipboard() != "'/engine/llama-server' '--port' '48857'" {
					t.Fatalf("clipboard %q", tt.Clipboard())
				}
				if !tt.HasText("由操作者启动") {
					t.Fatal("missing external instruction")
				}
			}
		})
	}
}
func TestSecondBundleAndIncompleteRun(t *testing.T) {
	w := viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete"}, {Bundle: "zh-svg-pelican", Phase: "candidate_missing"}}}}
	tt := ui.NewTester(w.view, 1000, 740)
	if w.active().Bundle != "zh-svg-pelican" || !tt.HasText("zh-core · 已封存") || !tt.HasText("zh-svg-pelican") {
		t.Fatal(tt.Texts())
	}
	w.progress.NextAction = "run"
	w.progress.Bundles[1].NextAction = "resume"
	w.progress.Bundles[1].Phase = "run_incomplete"
	tt = ui.NewTester(w.view, 1000, 740)
	if !tt.HasText("继续未完成的运行") || slices.Contains(tt.Texts(), "run_incomplete") {
		t.Fatal(tt.Texts())
	}
}
func TestCommandArguments(t *testing.T) {
	args := setArg([]string{"config", "create", "--engine", "old"}, "--engine", "/path with spaces/it's engine")
	parsed, err := inferyard.SplitCommand(commandOf(args))
	if err != nil || !slices.Equal(parsed, args) {
		t.Fatalf("roundtrip: %v %v", parsed, err)
	}
	if envName.MatchString("token with spaces") || envName.MatchString("hf-secret-token") {
		t.Fatal("invalid env name accepted")
	}
}
