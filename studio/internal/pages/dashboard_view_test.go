package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

func TestDashboardViews(t *testing.T) {
	version := core.Version
	core.Version = "0.0.2"
	t.Cleanup(func() { core.Version = version })
	total := int64(120)
	cases := []struct {
		name  string
		state viewState
		want  string
	}{
		{"CLI unavailable", viewState{loaded: true}, "准备环境"},
		{"initialize", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}}}, "准备环境"},
		{"external service", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{{Phase: "bound_missing", CandidatePath: strptr("/tmp/c")}}}, startup: core.Startup{Path: "/engine/llama-server", Args: []string{"--port", "48857"}}}, "高级：手动绑定"},
		{"managed engine", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{{Phase: "bound_missing", CandidatePath: strptr("/tmp/c")}}, NextCommand: strptr("inferyard config bind --candidate /tmp/c --pid 1 --endpoint http://x --out /tmp/new")}, engineState: core.EngineState{State: "running", Pid: 4242, Endpoint: "http://127.0.0.1:48857"}}, "重新绑定失败的题包"},
		{"running", viewState{loaded: true, tasks: []core.RunTask{{State: "running"}}, runProgress: &core.RunProgress{Done: 86, Total: &total}}, "86 / 120 题"},
		{"verified", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{DefaultScopeComplete: true}}, "已核验 verified"},
		{"device defaults", viewState{loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "device_check", Workspace: "/tmp/ws"}, modelURL: core.DefaultModelURL, engineURL: core.DefaultEngineURL()}, "引擎 URL"},
		{"initialized outdated CLI", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok"}, Workspace: core.DirectoryReadiness{Path: strptr("/tmp/ws"), Exists: true, Initialized: true}}}, "已初始化"},
		{"checkout CLI", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok", Version: strptr("0.0.3"), Source: "checkout", Path: strptr("/src/inferyard")}}}, "准备环境"},
		{"release CLI", viewState{loaded: true, ready: core.Readiness{CLI: core.InferyardProbe{Status: "ok", Version: strptr("0.0.3"), Source: "release", Path: strptr("/usr/bin/inferyard")}}}, "准备环境"},
	}
	for i := range cases {
		tc := &cases[i]
		if tc.name == "external service" || tc.name == "managed engine" {
			tc.state.failure = core.Err("cli_error")
		}
		t.Run(tc.name, func(t *testing.T) {
			switch tc.name {
			case "CLI unavailable", "initialize", "initialized outdated CLI", "checkout CLI", "release CLI":
				tc.state.selected = "dashboard"
			case "verified":
				tc.state.selected = "reports"
			case "device defaults":
				tc.state.selected = "downloads"
			default:
				tc.state.selected = "dashboard"
			}
			tt := ui.NewTester(tc.state.view, 1120, 2400)
			if !tt.HasText(tc.want) {
				t.Fatalf("missing %q: %v", tc.want, tt.Texts())
			}
			if tc.name == "initialized outdated CLI" {
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
				if !tt.HasText("准备环境") {
					t.Fatalf("missing auto-install action: %v", tt.Texts())
				}
				if !tt.HasText("路径在设置页修改") {
					t.Fatal("missing manual path action")
				}
			}
			if tc.name == "external service" {
				click(t, tt, "高级：手动绑定")
				for _, label := range []string{"启动服务", "绑定进程", "已启动的本机进程"} {
					if !tt.HasText(label) {
						t.Fatalf("missing %q: %v", label, tt.Texts())
					}
				}
				if tt.HasText("复制启动命令") || tt.HasText("/engine/llama-server") {
					t.Fatal("startup path exposed")
				}
				if tt.HasText("由操作者启动") {
					t.Fatal("external-start instruction must be gone")
				}
			}
			if tc.name == "managed engine" {
				for _, label := range []string{"运行中 · PID 4242 · http://127.0.0.1:48857", "重新绑定失败的题包", "停止服务"} {
					if !tt.HasText(label) {
						t.Fatalf("missing %q: %v", label, tt.Texts())
					}
				}
				cmd, e := tc.state.managedBindCommand()
				if e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(cmd, "'--pid' '4242'") || !strings.Contains(cmd, "'--endpoint' 'http://127.0.0.1:48857'") {
					t.Fatalf("managed bind command %q", cmd)
				}
			}
		})
	}
}
func TestSecondBundleAndIncompleteRun(t *testing.T) {
	w := viewState{selected: "dashboard", loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_complete"}, {Bundle: "zh-svg-pelican", Phase: "candidate_missing"}}}}
	tt := ui.NewTester(w.view, 1000, 740)
	if w.active().Bundle != "zh-svg-pelican" || !tt.HasText("zh-core · 已封存") || !tt.HasText("zh-svg-pelican") {
		t.Fatal(tt.Texts())
	}
	w.progress.NextAction = "run"
	w.progress.Bundles[1].NextAction = "resume"
	w.progress.Bundles[1].Phase = "run_incomplete"
	tt.Frame()
	if !tt.HasText("继续未完成的运行") || slices.Contains(tt.Texts(), "run_incomplete") {
		t.Fatal(tt.Texts())
	}
}

// A blocked reason on an in-flight single-mode run is flow guidance (why
// resume is not offered), not a failure: during benchmark execution the
// banner must show translated guidance instead of the raw-code fallback.
func TestIncompleteRunBlockedReasonShowsGuidance(t *testing.T) {
	w := viewState{selected: "dashboard", loaded: true, progress: &inferyard.WorkspaceProgress{NextAction: "run", Bundles: []inferyard.BundleProgress{{
		Bundle: "zh-core", Phase: "run_incomplete", NextAction: "run", BlockedReason: ptr("single_run_resume_not_supported"),
	}}}}
	tt := ui.NewTester(w.view, 1000, 740)
	if !tt.HasText("单次运行不支持断点续跑") || tt.HasText("尚未识别的原因码") || tt.HasText("single_run_resume_not_supported") {
		t.Fatal(tt.Texts())
	}
}
func TestCommandArguments(t *testing.T) {
	args := setArg([]string{"config", "create", "--engine", "old"}, "--engine", "/path with spaces/it's engine")
	parsed, err := inferyard.SplitCommand(commandOf(args))
	if err != nil || !slices.Equal(parsed, args) {
		t.Fatalf("roundtrip: %v %v", parsed, err)
	}
	if environmentName.MatchString("token with spaces") || environmentName.MatchString("hf-secret-token") {
		t.Fatal("invalid env name accepted")
	}
}

func TestStudioOriginNamesPackagedBuild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got := studioOrigin()
	want := "Studio 发行包 · " + exe
	if strings.Contains(filepath.ToSlash(exe), "/go-build") {
		want = "Studio 源码运行 · " + exe
	}
	if got != want {
		t.Fatalf("studioOrigin = %q, want %q", got, want)
	}
}
