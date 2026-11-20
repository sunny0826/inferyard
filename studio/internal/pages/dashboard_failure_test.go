package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// failedBindState is the bind stage after the task chain reported a failure;
// the engine state alone has no error code, as the issue describes.
func failedBindState() *viewState {
	s := testState()
	s.task = benchmarkTask{
		stage:   "bind",
		bundles: []string{"zh-svg-pelican"},
		start:   startChainResult{candidate: "/w/candidates/zh-svg-pelican.toml"},
		failure: core.Err("engine_start_failed"),
	}
	s.engineState = core.EngineState{State: "failed"}
	return &s
}

func serviceTexts(s *viewState) []string {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.serviceView(c) })
	}, 1120, 1400)
	return tt.Texts()
}

func TestServiceViewShowsTaskFailureWithoutEngineError(t *testing.T) {
	s := failedBindState()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.serviceView(c) })
	}, 1120, 1400)
	if !tt.HasText("连接并核验服务失败") {
		t.Fatalf("missing failure heading: %v", tt.Texts())
	}
	if !tt.HasText("engine_start_failed") || !tt.HasText("引擎进程启动失败") {
		t.Fatalf("missing cause: %v", tt.Texts())
	}
	if !tt.HasText("连接已有本机服务") || !tt.HasText("高级：手动连接") {
		t.Fatalf("missing recovery entry points: %v", tt.Texts())
	}
}

func TestServiceViewReportsFailedStateWithoutCause(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "failed"}
	texts := serviceTexts(&s)
	found := false
	for _, text := range texts {
		if text == "未返回原因码。" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing cause-less failure notice: %v", texts)
	}
}

// The manual and discovered bindings must stay reachable before any failure.
func TestServiceViewKeepsManualBindingWithoutFailure(t *testing.T) {
	s := testState()
	if !s.bindingPhase() {
		t.Fatal("fixture is not in the bind stage")
	}
	texts := serviceTexts(&s)
	for _, want := range []string{"连接已有本机服务", "高级：手动连接"} {
		found := false
		for _, text := range texts {
			if text == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q before a failure: %v", want, texts)
		}
	}
}

// The progress summary must not keep describing the pre-failure phase.
func TestConclusionViewMarksFailedTask(t *testing.T) {
	s := failedBindState()
	s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-svg-pelican", Phase: "candidate_missing", NextAction: "bind"},
	}}
	tt := ui.NewTester(s.conclusionView, 1120, 1000)
	texts := tt.Texts()
	if !tt.HasText("zh-svg-pelican · 失败") {
		t.Fatalf("failed task bundle not reported: %v", texts)
	}
	if tt.HasText("zh-svg-pelican · 待创建测评配置") {
		t.Fatalf("stale phase contradicts the failed step: %v", texts)
	}
}

// The ambiguous-listener warning renders as a tinted warning banner, sharing
// the discovery view's own presentation instead of plain coloured text.
func TestAmbiguousListenerRendersWarningBanner(t *testing.T) {
	s := testState()
	s.discoveryLoaded = true
	s.discovery = inferyard.ServiceDiscovery{Services: []inferyard.DiscoveredService{
		{PID: 4100, ProcessName: "proxy", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:9999", Models: []string{"other.gguf"}, AmbiguousListener: true}}},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.discoveryBody(c, false) }) }, 1120, 900)
	tt.Frame()
	if !tt.HasText("待确认的兼容接口") {
		t.Fatalf("compatible endpoint fold missing: %v", tt.Texts())
	}
	// The fold is collapsed by default; opening it reveals the banner text.
	click(t, tt, "待确认的兼容接口")
	tt.Frame()
	if !tt.HasText("归属存在歧义") {
		t.Fatalf("ambiguous listener warning hidden in the fold: %v", tt.Texts())
	}
}

// One click fills the LM Studio fields from an unambiguous discovered service.
func TestLocalSelectionFillsDiscoveredService(t *testing.T) {
	s := testState()
	s.discoveryLoaded = true
	s.discovery = inferyard.ServiceDiscovery{Services: []inferyard.DiscoveredService{
		{PID: 4001, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:1234", Models: []string{"local.gguf"}}}},
		{PID: 4100, ProcessName: "proxy", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:9999", Models: []string{"other"}, AmbiguousListener: true}}},
		{PID: 4200, ProcessName: "listener", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:4321"}}},
	}}
	s.registry.Engines = []core.EngineAsset{{Name: "lmstudio · 模拟安装", Family: "lmstudio", Kind: "local_installation"}}
	s.engine = "lmstudio · 模拟安装"
	s.model = "local.gguf"
	s.registry.Models = []core.ModelAsset{{Name: "local.gguf", Format: "gguf", Available: true}}
	s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{"lmstudio · 模拟安装": {State: core.EngineCheckEligible}}}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.localSelectionView(c) }) }, 1120, 900)
	texts := tt.Texts()
	if !tt.HasText("PID 4001 · http://127.0.0.1:1234") {
		t.Fatalf("discovered service not offered: %v", texts)
	}
	for _, excluded := range []string{"PID 4100", "PID 4200"} {
		for _, text := range texts {
			if len(text) >= len(excluded) && text[:len(excluded)] == excluded {
				t.Fatalf("ambiguous or catalogue-less service offered: %v", texts)
			}
		}
	}
	if err := tt.Click("PID 4001 · http://127.0.0.1:1234 · 1 个模型 · LM Studio"); err != nil {
		t.Fatal(err)
	}
	if s.localPID != "4001" || s.localEndpoint != "http://127.0.0.1:1234" {
		t.Fatalf("fields not filled: %q %q", s.localPID, s.localEndpoint)
	}
	if reason := s.localSelectionError(); reason != "" {
		t.Fatalf("filled selection is invalid: %s", reason)
	}
}
