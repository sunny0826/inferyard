package pages

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestModelsFourSections(t *testing.T) {
	s := testState()
	s.selected = "models"
	tt := ui.NewTester(s.view, 1120, 2600)
	texts := tt.Texts()
	previous := -1
	for _, title := range []string{"模型", "下载", "引擎服务", "绑定存活提示"} {
		index := slices.Index(texts, title)
		if index <= previous {
			t.Fatalf("section order: %v", texts)
		}
		previous = index
	}
}

func TestModelsUnifiedDownloadMeasurements(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading", ProgressReason: ptr("engine_transfer_progress_unavailable")},
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 800)
	click(t, tt, "engine-fixture")
	for _, want := range []string{"引擎", "模型", "已下载 null · 速率 null · 归档获取接口未提供字节读数", "1.50", "2048", "1 MiB = 1,048,576 bytes"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("已下载 0 bytes") {
		t.Fatal("fabricated engine bytes")
	}
	for _, name := range []string{"engine-fixture", "model-fixture"} {
		click(t, tt, name)
		if !tt.HasText("取消下载") {
			t.Fatal("missing cancel", name)
		}
		enabled := false
		for i := 0; i < 8; i++ {
			tt.Key(0, ui.KeyTab)
			if tt.Focused("取消下载") {
				enabled = true
				break
			}
		}
		if !enabled {
			t.Fatal("cancel is not keyboard accessible", name)
		}
	}
	if !tt.HasText("已下载 1572864 bytes") {
		t.Fatal(tt.Texts())
	}
	for _, state := range []string{"failed", "cancelled"} {
		s.downloads[1].State = state
		tt.Frame()
		if tt.HasText("取消下载") || !tt.HasText("输出目录保留；再次下载请更换目录名。") {
			t.Fatal(tt.Texts())
		}
	}
}

func TestModelsAdoptCompletedEngine(t *testing.T) {
	for _, initial := range []string{"", "/existing/llama-server"} {
		s := testState()
		s.selected, s.engine = "models", initial
		s.downloads = []core.DownloadTask{
			{TaskId: "old", Kind: "engine", OutName: "old-engine", State: "done", EnginePath: ptr("/old/llama-server"), FinishedAt: ptr("2026-10-14T01:00:00Z")},
			{TaskId: "new", Kind: "engine", OutName: "new-engine", State: "done", EnginePath: ptr("/new/llama-server"), FinishedAt: ptr("2026-10-14T02:00:00Z")},
			{TaskId: "pending", Kind: "engine", State: "downloading"},
		}
		tt := ui.NewTester(s.view, 1120, 2600)
		click(t, tt, "new-engine")
		if !tt.HasText("引擎路径：/new/llama-server") {
			t.Fatal(tt.Texts())
		}
		want := initial
		if want == "" {
			want = "/new/llama-server"
		}
		if s.engine != want {
			t.Fatal("adopted", s.engine, "want", want)
		}
		s.downloads[1].EnginePath = ptr("/later/llama-server")
		tt.Frame()
		if s.engine != want {
			t.Fatal("overwrote adopted path")
		}
	}
}

func TestModelsEngineServiceStates(t *testing.T) {
	for _, tc := range []struct{ state, label string }{{"stopped", "已停止"}, {"starting", "启动中"}, {"running", "运行中"}, {"stopping", "停止中"}, {"failed", "失败"}} {
		s := testState()
		s.engineState = core.EngineState{State: tc.state, Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/candidate.toml", LogPath: "/engine.log"}
		tt := ui.NewTester(func(c *ui.Context) { s.modelEngineServiceView(c) }, 1000, 900)
		for _, want := range []string{"服务状态：" + tc.label, "PID：42", "http://127.0.0.1:8080", "/candidate.toml", "/engine.log", "请先到向导页准备冻结配置。", "不会自动重启；Studio 只管理自己启动的进程。"} {
			if !tt.HasText(want) {
				t.Fatal(want, tt.Texts())
			}
		}
		click(t, tt, "启动服务")
		if s.busy {
			t.Fatal("empty candidate admitted")
		}
		if tc.state == "starting" || tc.state == "running" || tc.state == "stopping" {
			if !tt.HasText("重复启动返回当前状态，未创建新进程。") {
				t.Fatal(tt.Texts())
			}
		}
		for _, code := range []string{"engine_exited", "engine_start_timeout"} {
			s.engineState.State = "failed"
			s.engineState.Error = &core.ApiError{Code: code}
			tt.Frame()
			if !tt.HasText(code) || !tt.HasText("/engine.log") {
				t.Fatal(tt.Texts())
			}
		}
		s.engineState.Endpoint = "http://192.168.1.5:8080"
		tt.Frame()
		if tt.HasText("192.168.1.5") {
			t.Fatal("non-loopback endpoint displayed")
		}
	}
}

func TestModelsBindingStates(t *testing.T) {
	for _, tc := range []struct {
		bound *string
		stale bool
		want  string
	}{
		{nil, false, "尚未绑定服务"}, {nil, true, "尚未绑定服务"},
		{ptr("/bound"), false, "存活提示匹配"}, {ptr("/bound"), true, "原绑定可能已失效，请到运行页重新绑定"},
	} {
		s := testState()
		s.progress = &inferyard.WorkspaceProgress{Bundles: []inferyard.BundleProgress{{Phase: "run_missing", BoundPath: tc.bound}}}
		s.bindingStale = tc.stale
		tt := ui.NewTester(func(c *ui.Context) { s.modelBindingView(c) }, 1000, 500)
		for _, want := range []string{tc.want, "server_pid / process_start_ticks", "CLI 在 bind / run 时完成", "服务运行中不代表绑定有效"} {
			if !tt.HasText(want) {
				t.Fatal(want, tt.Texts())
			}
		}
	}
}
