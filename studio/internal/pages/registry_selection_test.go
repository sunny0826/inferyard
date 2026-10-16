package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestCandidateUsesRegistryNames(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	tt := ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	if s.engine != s.registry.DefaultEngine {
		t.Fatal(s.engine)
	}
	if !tt.HasText("候选引擎") || !tt.HasText("测评模型") || tt.HasText("引擎文件绝对路径") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, s.registry.DefaultEngine)
	if err := tt.ChooseMenuItem(s.registry.Engines[1].Name); err != nil {
		t.Fatal(err)
	}
	if s.engine != s.registry.Engines[1].Name {
		t.Fatal("engine choice did not update", s.engine)
	}
	if len(designFieldInputs(tt, "候选引擎")) != 0 {
		t.Fatal("engine path input remains")
	}
}

func TestCandidateEngineDropdownThreeStates(t *testing.T) {
	// Normal: verified engines only, ineligible filtered out, create enabled.
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	if s.engine != "engine-2 · llama-server" {
		t.Fatal("default must be the verified latest download", s.engine)
	}
	if !tt.HasText("仅列出核验可用的引擎；默认选中其中最新下载的引擎。") || tt.HasText("没有可用的引擎") || tt.HasText("当前选择不可用") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "engine-2 · llama-server")
	if err := tt.ChooseMenuItem("release · inference-engine"); err == nil {
		t.Fatal("ineligible engine stayed in the dropdown")
	}

	// Checking hint.
	s = testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.checks.Checking = 1
	tt = ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	if !tt.HasText("正在核验 1 个引擎…") {
		t.Fatal(tt.Texts())
	}

	// Empty: nothing verified usable -> disabled with guidance.
	s = testState()
	s.registry = modelRegistryFixture()
	s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{
		"engine-2 · llama-server":    {State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"},
		"release · inference-engine": {State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"},
	}}
	tt = ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	if !tt.HasText("没有可用的引擎，请到下载页下载推荐引擎。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "运行 benchmark")
	if s.busy {
		t.Fatal("create reached the CLI without a usable engine")
	}
	click(t, tt, "到下载页")
	if s.selected != "downloads" {
		t.Fatal(s.selected)
	}

	// Revoked: the selected engine failed re-verification.
	s = testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.engine = "release · inference-engine"
	tt = ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 900)
	for _, want := range []string{"当前选择不可用", "引擎运行库不完整或库链接无效", "到模型页重选"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	click(t, tt, "运行 benchmark")
	if s.busy {
		t.Fatal("create reached the CLI with a revoked engine")
	}
	click(t, tt, "到模型页重选")
	if s.selected != "models" {
		t.Fatal(s.selected)
	}
}

func TestCandidateModelSelectSharesSelection(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1000, 700)
	if s.model != "" {
		t.Fatal(s.model)
	}
	click(t, tt, "使用 CLI 推荐模型")
	if err := tt.ChooseMenuItem("model.gguf"); err != nil {
		t.Fatal(err)
	}
	if s.model != "model.gguf" {
		t.Fatal("model choice did not update the shared selection", s.model)
	}
}

func TestRunAndServiceViewsHidePaths(t *testing.T) {
	s := testState()
	s.failure = core.Err("cli_error")

	s.startup = core.Startup{Path: "/private/llama-server", Args: []string{"--model", "/private/model.gguf"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.candidateView(c); s.serviceView(c) }) }, 1120, 1800)
	if tt.HasText("复制启动命令") {
		t.Fatal("startup command remains")
	}
	for _, v := range tt.Texts() {
		if strings.Contains(v, "/private/") || strings.Contains(v, "/w/bound/") {
			t.Fatal("path leaked", v)
		}
	}
	for _, label := range []string{"第一配置绝对路径", "第二配置绝对路径"} {
		inputs := designFieldInputs(tt, label)
		if len(inputs) != 0 {
			t.Fatal("removed quick-run input remains", label)
		}
	}
	click(t, tt, "高级：手动绑定")
	if !tt.HasText("绑定进程") {
		t.Fatal("manual binding fallback missing")
	}
}

func TestOrphanServiceViews(t *testing.T) {
	for _, page := range []string{"models", "dashboard"} {
		s := testState()
		s.model, s.engine = "model.gguf", "engine-2 · llama-server"
		s.engineState = core.EngineState{State: "orphaned", Pid: 4242, Candidate: "candidate-1", LogPath: "engine.log", Endpoint: "http://127.0.0.1:8080"}
		render := s.runPanelView
		if page == "dashboard" {
			render = s.serviceView
		}
		tt := ui.NewTester(render, 1000, 1200)
		if page == "models" {
			assertModelsReadOnly(t, tt)
			if !tt.HasText("遗留引擎") || tt.HasText("用这个组合新开任务") {
				t.Fatal(tt.Texts())
			}
			continue
		}
		for _, want := range []string{"遗留引擎", "停止遗留服务"} {
			if !tt.HasText(want) {
				t.Fatal(page, want, tt.Texts())
			}
		}
		if page == "dashboard" {
			click(t, tt, "启动服务")
		} else {
			click(t, tt, "启动")
		}
		if s.busy {
			t.Fatal("orphan allowed a new startup")
		}
	}
}

func TestOrphanRecoveryButtonOnBothPages(t *testing.T) {
	for _, page := range []string{"models", "dashboard"} {
		for _, code := range []string{"engine_owner_invalid", "service_identity_unreadable", "engine_owner_remove_failed", ""} {
			s := testState()
			s.model, s.engine = "model.gguf", "engine-2 · llama-server"
			s.engineState = core.EngineState{State: "orphaned"}
			if code != "" {
				s.engineState.Error = core.Err(code)
			}
			render := s.runPanelView
			if page == "dashboard" {
				render = s.serviceView
			}
			tt := ui.NewTester(render, 1000, 1800)
			if page == "models" {
				assertModelsReadOnly(t, tt)
				if !tt.HasText("遗留引擎") || tt.HasText("用这个组合新开任务") {
					t.Fatal(tt.Texts())
				}
				continue
			}
			if tt.HasText("仅删除记录（不终止进程）") != (code != "") {
				t.Fatal(page, code, tt.Texts())
			}
			if code != "" && !tt.HasText("logs/engine-owner.json") {
				t.Fatal(tt.Texts())
			}
		}
	}
}
