package pages

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestModelsTwoColumns(t *testing.T) {
	s := deviceTestState()
	s.downloads = []core.DownloadTask{
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "done", EnginePath: ptr("/fixture/llama-server")},
	}
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
	left, lok := tt.Find("模型")
	right, rok := tt.Find("引擎服务")
	if !lok || !rok || left.X >= right.X || left.Y != right.Y {
		t.Fatal("expected two aligned columns", left, right)
	}

	if dir := os.Getenv("STUDIO_UI_PREVIEW_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "models-downloads.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

}

func TestModelsUnifiedDownloadMeasurements(t *testing.T) {
	for _, task := range []core.DownloadTask{
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading", ProgressReason: ptr("engine_transfer_progress_unavailable")},
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
	} {
		t.Run(task.Kind, func(t *testing.T) {
			s := testState()
			s.downloads = []core.DownloadTask{task}
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1200)
			for _, want := range []string{task.OutName, "已观测", "速率 bytes/s", "总量", "null", "总量未知", "取消下载"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			amount, _ := tt.Find("已观测")
			rate, _ := tt.Find("速率 bytes/s")
			total, _ := tt.Find("总量")
			if amount.Y != rate.Y || rate.Y != total.Y || amount.X >= rate.X || rate.X >= total.X {
				t.Fatal("metrics are not three columns")
			}
			activity, ok := tt.Find("下载任务活动")
			if !ok || activity.H != 4 {
				t.Fatal("missing activity stripe", activity)
			}
			colors := map[[4]uint8]bool{}
			for x := int(activity.X) + 4; x < int(activity.X+activity.W)-4; x++ {
				p := tt.Image().RGBAAt(x, int(activity.Y+2))
				colors[[4]uint8{p.R, p.G, p.B, p.A}] = true
			}
			if len(colors) < 2 {
				t.Fatal("activity bar is not striped")
			}
			if tt.HasText("输出目录：") {
				t.Fatal("details start open")
			}
			click(t, tt, "观测依据与输出")
			click(t, tt, "测量单位")
			for _, want := range []string{"输出目录：", "文件：", "1 MiB = 1,048,576 bytes", "1 GiB = 1,073,741,824 bytes", "总量未知不显示百分比"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if task.Kind == "engine" {
				if !tt.HasText("已下载 null · 速率 null · 归档获取接口未提供字节读数") || tt.HasText("已下载 0 bytes") {
					t.Fatal(tt.Texts())
				}
			} else if !tt.HasText("1.50 MiB") || !tt.HasText("2048") || !tt.HasText("已下载 1572864 bytes") {
				t.Fatal(tt.Texts())
			}
			enabled := false
			for i := 0; i < 12; i++ {
				tt.Key(0, ui.KeyTab)
				if tt.Focused("取消下载") {
					enabled = true
					break
				}
			}
			if !enabled {
				t.Fatal("cancel is not keyboard accessible")
			}
			for _, state := range []string{"failed", "cancelled", "done"} {
				s.downloads[0].State = state
				tt.Frame()
				if _, ok := tt.Find("下载任务活动"); ok {
					t.Fatal("terminal activity stripe")
				}
				if tt.HasText("取消下载") {
					t.Fatal("terminal cancel")
				}
				if state != "done" && !tt.HasText("输出目录保留；再次下载请更换目录名。") {
					t.Fatal(tt.Texts())
				}
			}
		})
	}
}

func TestModelsAdoptCompletedEngine(t *testing.T) {
	for _, initial := range []string{"", "/existing/llama-server"} {
		s := testState()
		s.selected, s.engine = "models", initial
		s.settings.Effective.WorkspaceRoot = ptr("/ws")
		s.downloads = []core.DownloadTask{
			{TaskId: "old", Kind: "engine", OutName: "old-engine", OutDir: "/ws/engines/old-engine", State: "done", EnginePath: ptr("/old/llama-server"), FinishedAt: ptr("2026-10-14T01:00:00Z")},
			{TaskId: "new", Kind: "engine", OutName: "new-engine", OutDir: "/ws/engines/new-engine", State: "done", EnginePath: ptr("/new/llama-server"), FinishedAt: ptr("2026-10-14T02:00:00Z")},
			{TaskId: "pending", Kind: "engine", OutDir: "/ws/engines/pending", State: "downloading"},
			{TaskId: "foreign", Kind: "engine", OutName: "foreign-engine", OutDir: "/other/engines/foreign", State: "done", EnginePath: ptr("/foreign/llama-server"), FinishedAt: ptr("2026-10-14T03:00:00Z")},
		}
		tt := ui.NewTester(s.view, 1120, 2600)
		details := testState()
		details.downloads = []core.DownloadTask{s.downloads[1]}
		dt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { details.downloadsView(c) }) }, 1000, 1000)
		click(t, dt, "观测依据与输出")
		if !dt.HasText("引擎路径：/new/llama-server") {
			t.Fatal(dt.Texts())
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
	for _, tc := range []struct{ state, label string }{{"stopped", "已停止"}, {"starting", "正在加载模型"}, {"running", "服务已就绪"}, {"stopping", "正在停止服务"}, {"failed", "服务异常退出"}} {
		s := testState()
		s.engineState = core.EngineState{State: tc.state, Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/candidate.toml", LogPath: "/engine.log"}
		tt := ui.NewTester(func(c *ui.Context) { s.modelEngineServiceView(c) }, 1000, 900)
		if tt.HasText("/engine.log") {
			t.Fatal("startup detail starts open")
		}
		click(t, tt, "查看启动行（安全展示）")
		for _, want := range []string{tc.label, "null · 此页无内存采样", "凭据参数与完整进程 argv 不展示", "PID：42", "http://127.0.0.1:8080", "/candidate.toml", "/engine.log", "请先到向导页准备冻结配置。", "不会自动重启；Studio 只管理自己启动的进程。"} {
			if !tt.HasText(want) {
				t.Fatal(want, tt.Texts())
			}
		}
		steps := []string{}
		for _, value := range tt.Texts() {
			if slices.Contains([]string{"stopped", "starting", "running", "stopping"}, value) {
				steps = append(steps, value)
			}
		}
		if len(steps) < 5 || !slices.Equal(steps[len(steps)-5:], []string{"stopped", "starting", "running", "stopping", "stopped"}) {
			t.Fatal("steps", steps)
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
		bound          *string
		stale, unknown bool
		want           string
	}{
		{nil, false, false, "尚未绑定服务"}, {nil, true, false, "尚未绑定服务"},
		{ptr("/bound"), false, false, "存活提示匹配"}, {ptr("/bound"), true, false, "原绑定可能已失效，请到运行页重新绑定"},
		{ptr("/bound"), false, true, "无法确认存活，不据此判定绑定有效"},
	} {
		s := testState()
		s.progress = &inferyard.WorkspaceProgress{Bundles: []inferyard.BundleProgress{{Phase: "run_missing", BoundPath: tc.bound}}}
		s.bindingStale, s.bindingUnknown = tc.stale, tc.unknown
		tt := ui.NewTester(func(c *ui.Context) { s.modelBindingView(c) }, 1000, 500)
		for _, want := range []string{tc.want, "server_pid / process_start_ticks", "CLI 在 bind / run 时完成", "服务运行中不代表绑定有效"} {
			if !tt.HasText(want) {
				t.Fatal(want, tt.Texts())
			}
		}
		click(t, tt, "到运行页绑定")
		if s.selected != "runs" {
			t.Fatal("binding navigation", s.selected)
		}
		if tc.want != "存活提示匹配" && tt.HasText("存活提示匹配") {
			t.Fatal("false liveness match", tc, tt.Texts())
		}
	}
}
