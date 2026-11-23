package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type enginePreviewStub struct {
	dispatched []previewIntent
}

func (p *enginePreviewStub) showNotice() {}
func (p *enginePreviewStub) dispatch(intent previewIntent) {
	p.dispatched = append(p.dispatched, intent)
}
func (p *enginePreviewStub) resetTask(string, string)                          {}
func (p *enginePreviewStub) report(string) (*core.ReportIndex, *core.ApiError) { return nil, nil }
func (p *enginePreviewStub) detail(string) *core.RunTaskDetail                 { return nil }
func (p *enginePreviewStub) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}
func (p *enginePreviewStub) detectedEngines() []inferyard.DetectedEngine { return nil }
func (p *enginePreviewStub) downloadLogTail(string) string               { return "" }

func TestEngineCardMetadataAndEmptyOmission(t *testing.T) {
	t.Run("full-running", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{
			State:       "running",
			Family:      "llama-cpp",
			Executable:  "/opt/bin/llama-server",
			Model:       "/models/Qwen3-4B-Q4_K_M.gguf",
			ContextSize: 32768,
			Pid:         42,
			Candidate:   "/w/candidates/c1",
			Endpoint:    "http://127.0.0.1:48857",
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, want := range []string{"llama.cpp · llama-server", "Qwen3-4B-Q4_K_M.gguf", "上下文 32K · PID 42", "c1", "http://127.0.0.1:48857", "打开网页", "停止引擎"} {
			if !tt.HasText(want) {
				t.Fatalf("missing %q in %v", want, tt.Texts())
			}
		}
	})

	t.Run("minimal-omits-empty", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("PID 42") {
			t.Fatalf("missing PID 42: %v", tt.Texts())
		}
		if tt.HasText("缺测") {
			t.Fatalf("empty engine metadata should be omitted, not shown as missing: %v", tt.Texts())
		}
	})

	t.Run("stopped-pending-candidate", func(t *testing.T) {
		s := readyState("reports")
		s.candidate = "/w/candidates/c2"
		s.startup = core.Startup{
			Family:      "polystrata",
			Path:        "/opt/poly/server.py",
			ModelPath:   "/m/qwen36.gguf",
			ContextSize: 65536,
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 已停止"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, want := range []string{"PolyStrata · server.py", "qwen36.gguf", "64K", "c2", "开启引擎"} {
			if !tt.HasText(want) {
				t.Fatalf("missing %q in %v", want, tt.Texts())
			}
		}
	})
}

func TestEngineCardOpenWebUIAndCopyEndpoint(t *testing.T) {
	s := readyState("reports")
	s.engineState = core.EngineState{
		State:       "running",
		Family:      "llama-cpp",
		Executable:  "llama-server",
		Model:       "Qwen3-4B-Q4_K_M.gguf",
		ContextSize: 32768,
		Pid:         42,
		Endpoint:    "http://127.0.0.1:48857",
	}
	tt := sidebarHeaderState(s)
	if err := tt.Click("引擎 · 运行中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("http://127.0.0.1:48857"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("打开网页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	gotURLs := tt.OpenedURLs()
	if len(gotURLs) != 2 || gotURLs[0] != "http://127.0.0.1:48857" || gotURLs[1] != "http://127.0.0.1:48857" {
		t.Fatalf("OpenedURLs = %v, want two loopback opens", gotURLs)
	}
	if err := tt.Click("复制服务地址"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "http://127.0.0.1:48857" {
		t.Fatalf("Clipboard = %q, want loopback endpoint", got)
	}
}

func TestEngineCardWebUIGuardsAndPreviewIsolation(t *testing.T) {
	t.Run("webui-disabled", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{
			State:         "running",
			Family:        "llama-cpp",
			Pid:           42,
			Endpoint:      "http://127.0.0.1:48857",
			WebUIDisabled: true,
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("网页未启用") || tt.HasText("打开网页") {
			t.Fatalf("unexpected webui-disabled texts: %v", tt.Texts())
		}
		_ = tt.Click("http://127.0.0.1:48857")
		tt.Frame()
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("disabled webui still opened URL: %v", tt.OpenedURLs())
		}
		if err := tt.Click("复制服务地址"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if got := tt.Clipboard(); got != "http://127.0.0.1:48857" {
			t.Fatalf("Clipboard = %q", got)
		}
	})

	t.Run("non-loopback-blocked", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{
			State:    "running",
			Pid:      42,
			Endpoint: "http://192.168.1.20:8080",
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("非本机地址") || tt.HasText("打开网页") {
			t.Fatalf("unexpected non-loopback texts: %v", tt.Texts())
		}
		if _, ok := tt.Find("复制服务地址"); ok {
			t.Fatal("non-loopback endpoint exposed copy button")
		}
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("non-loopback endpoint opened URL: %v", tt.OpenedURLs())
		}
	})

	t.Run("preview-no-real-io", func(t *testing.T) {
		s := readyState("reports")
		s.preview = &enginePreviewStub{}
		s.engineState = core.EngineState{
			State:    "running",
			Family:   "llama-cpp",
			Pid:      42,
			Endpoint: "http://127.0.0.1:48857",
		}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 运行中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click("打开网页"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if len(tt.OpenedURLs()) != 0 {
			t.Fatalf("preview mode called OpenURLThen: %v", tt.OpenedURLs())
		}
		if len(s.pendingToasts) == 0 || s.pendingToasts[0].Title != "已在浏览器打开引擎界面（模拟）" {
			t.Fatalf("preview mode did not enqueue simulated toast: %+v", s.pendingToasts)
		}
	})
}

func TestEngineCardUnifiedHeaderStartAction(t *testing.T) {
	t.Run("disabled-without-candidate", func(t *testing.T) {
		spy := &enginePreviewStub{}
		s := readyState("reports")
		s.preview = spy
		s.engineState = core.EngineState{State: "stopped"}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 已停止"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		_ = tt.Click("开启引擎")
		tt.Frame()
		if len(spy.dispatched) != 0 {
			t.Fatalf("disabled header start button dispatched %+v", spy.dispatched)
		}
	})

	t.Run("starts-configured-candidate", func(t *testing.T) {
		spy := &enginePreviewStub{}
		s := readyState("reports")
		s.preview = spy
		s.candidate = "/w/candidates/c1"
		s.engineState = core.EngineState{State: "stopped"}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 已停止"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("已停止") {
			t.Fatalf("missing visible status in unified start button: %v", tt.Texts())
		}
		if err := tt.Click("开启引擎"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if len(spy.dispatched) != 1 {
			t.Fatalf("expected 1 start dispatch, got %+v", spy.dispatched)
		}
		if got, ok := spy.dispatched[0].(previewManagedEngine); !ok || got.candidate != "/w/candidates/c1" {
			t.Fatalf("unexpected start intent: %#v", spy.dispatched[0])
		}
	})
}

func TestEngineCardThemesAndDisabledStates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		dark         bool
		highContrast bool
	}{
		{"light", false, false},
		{"dark", true, false},
		{"high-contrast", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := readyState("reports")
			s.engineState = core.EngineState{
				State:       "running",
				Family:      "llama-cpp",
				Executable:  "llama-server",
				Model:       "Qwen3-4B-Q4_K_M.gguf",
				ContextSize: 32768,
				Pid:         42,
				Endpoint:    "http://127.0.0.1:48857",
			}
			s.tasks = []core.RunTask{{TaskId: "run", State: "running"}}
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(studioThemeFor(c, tc.dark))
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					ui.Column(c).Width(205).Shrink(0).Children(func() { s.leftNav(c) })
					ui.Column(c).Grow(1).Children(func() { s.cornerStatus(c) })
				})
			}, 1120, 700)
			if tc.highContrast {
				tt.SetPreferences(ui.Preferences{HighContrast: true})
			}
			tt.Frame()
			if err := tt.Click("引擎 · 运行中"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("测评进行中，结束后可停止引擎。") {
				t.Fatalf("missing active-run guard caption: %v", tt.Texts())
			}
			_ = tt.Click("停止引擎")
			tt.Frame()
			if tt.HasText("确认停止引擎") {
				t.Fatalf("disabled stop button opened confirmation: %v", tt.Texts())
			}

			s.tasks = nil
			s.engineState = core.EngineState{State: "stopped"}
			tt.Frame()
			if !tt.HasText("先在任务页选择模型并创建测评配置。") {
				t.Fatalf("missing empty candidate guidance: %v", tt.Texts())
			}
		})
	}
}

func TestEngineCardHoverAndTransitionStates(t *testing.T) {
	hoverButton := func(t *testing.T, s *viewState, trigger, actionLabel string) *ui.Tester {
		t.Helper()
		tt := sidebarHeaderState(s)
		tt.Frame()
		if err := tt.Click(trigger); err != nil {
			t.Fatalf("click %q: %v", trigger, err)
		}
		tt.Frame()
		r, ok := tt.Find(actionLabel)
		if !ok {
			t.Fatalf("missing button %q in %v", actionLabel, tt.Texts())
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
		return tt
	}

	t.Run("hover-running-shows-stop", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
		tt := hoverButton(t, s, "引擎 · 运行中", "停止引擎")
		if !tt.HasText("停止") {
			t.Fatalf("hovered running button missing 停止 label: %v", tt.Texts())
		}
		if _, ok := tt.Find("停止引擎"); !ok {
			t.Fatalf("hovered running button lost accessible label 停止引擎: %v", tt.Texts())
		}
	})

	t.Run("hover-stopped-shows-start", func(t *testing.T) {
		s := readyState("reports")
		s.candidate = "/w/candidates/c1"
		s.engineState = core.EngineState{State: "stopped"}
		tt := hoverButton(t, s, "引擎 · 已停止", "开启引擎")
		if !tt.HasText("开启") {
			t.Fatalf("hovered stopped button missing 开启 label: %v", tt.Texts())
		}
		if _, ok := tt.Find("开启引擎"); !ok {
			t.Fatalf("hovered stopped button lost accessible label 开启引擎: %v", tt.Texts())
		}
	})

	t.Run("hover-failed-shows-retry", func(t *testing.T) {
		s := readyState("reports")
		s.candidate = "/w/candidates/c1"
		s.engineState = core.EngineState{State: "failed", Error: core.Err("engine_start_failed")}
		tt := hoverButton(t, s, "引擎 · 失败", "开启引擎")
		if !tt.HasText("重试") {
			t.Fatalf("hovered failed button missing 重试 label: %v", tt.Texts())
		}
		if _, ok := tt.Find("开启引擎"); !ok {
			t.Fatalf("hovered failed button lost accessible label 开启引擎: %v", tt.Texts())
		}
	})

	t.Run("starting-and-stopping-progress", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{State: "starting", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 启动中"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if _, ok := tt.Find("模型引擎状态变更中"); !ok {
			t.Fatalf("missing starting progress bar: %v", tt.Texts())
		}
		s.engineState.State = "stopping"
		tt.Frame()
		if _, ok := tt.Find("模型引擎状态变更中"); !ok {
			t.Fatalf("missing stopping progress bar: %v", tt.Texts())
		}
		_ = tt.Click("停止引擎")
		tt.Frame()
		if tt.HasText("确认停止引擎") {
			t.Fatalf("stopping state should disable stop button: %v", tt.Texts())
		}
	})

	t.Run("orphaned-navigates-dashboard", func(t *testing.T) {
		s := readyState("reports")
		s.engineState = core.EngineState{State: "orphaned", Pid: 42}
		tt := sidebarHeaderState(s)
		if err := tt.Click("引擎 · 遗留引擎"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("遗留引擎") {
			t.Fatalf("missing orphaned badge: %v", tt.Texts())
		}
		if err := tt.Click("前往任务"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if s.selected != "dashboard" {
			t.Fatalf("orphaned action did not navigate to dashboard: %q", s.selected)
		}
	})
}
