package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func modelRegistryFixture() core.Registry {
	return core.Registry{
		Models: []core.ModelAsset{
			{Name: "model.gguf", Format: "gguf", SizeBytes: ptr(int64(4 << 30)), Architecture: "llama", ContextLength: ptr(int64(8192)), Available: true},
			{Name: "new.gguf", Format: "gguf", Available: true},
			{Name: "broken.gguf", Format: "gguf", Reason: "invalid_or_unreadable_gguf"},
		},
		Engines:       []core.EngineAsset{{Name: "engine-2 · llama-server", Source: "引擎下载 · engine-2", LatestDownload: true}, {Name: "release · inference-engine", Source: "用户引擎目录"}},
		DefaultEngine: "engine-2 · llama-server",
	}
}

func eligibleChecks() core.EngineCheckOverview {
	return core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{
		"engine-2 · llama-server":    {State: core.EngineCheckEligible, Version: "prism-b10743-adfffbe", Release: "prism-b10743-adfffbe"},
		"release · inference-engine": {State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"},
	}}
}

func assertModelsReadOnly(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for _, label := range []string{"启动", "停止", "绑定", "停止遗留服务", "仅删除记录（不终止进程）", "到 Dashboard 完成设备检查", "用这个组合新开任务", "请选择引擎", "请选择模型"} {
		if _, ok := tt.Find(label); ok {
			t.Fatal("models page exposes an action or selection", label, tt.Texts())
		}
	}
}

func TestModelsEmptyRegistryGuidance(t *testing.T) {
	s := testState()
	s.selected = "models"
	tt := ui.NewTester(s.view, 1120, 2000)
	for _, want := range []string{"尚未发现本地模型。", "尚未发现可用引擎。"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	assertModelsReadOnly(t, tt)
	click(t, tt, "到下载页")
	if s.selected != "downloads" {
		t.Fatal(s.selected)
	}
	tt.Frame()
	if !tt.HasText("新建下载任务") || !tt.HasText("下载引擎") {
		t.Fatal(tt.Texts())
	}
}

// The Models page lists installed resources with their verification verdict and
// never offers a second editor for the task's model/engine pair.
func TestModelsPageListsInstalledResourcesReadOnly(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, want := range []string{
		"已安装的模型", "model.gguf · GGUF", "架构 llama", "上下文 8192 tokens",
		"broken.gguf · GGUF", "不可用：模型不存在、不可读或 GGUF 元数据无效",
		"已安装的引擎", "engine-2 · llama-server", "版本已核验",
		"release · inference-engine", "不可用：引擎运行库不完整或库链接无效",
		"1 个可用 · 1 个不可用",
		"在任务页选择本次测评使用的模型与引擎；此页只展示已安装资源和本机服务。",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	assertModelsReadOnly(t, tt)
}

func TestModelsPageHidesPaths(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/secret/candidates/zh-core-1", LogPath: "/secret/logs/engine.log"}
	s.discoveryLoaded = true
	s.discovery.Services = nil
	s.settings.Effective.ModelsRoot = ptr("/secret/models")
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, v := range tt.Texts() {
		if strings.Contains(v, "/secret") {
			t.Fatal("path leaked", v)
		}
	}
	assertModelsReadOnly(t, tt)
	if !tt.HasText("状态：运行中") {
		t.Fatal("managed service state missing", tt.Texts())
	}
}

// An outdated CLI cannot verify engines; the page says so instead of presenting
// them as verified.
func TestModelsPageReportsUnverifiedEngines(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.registry = modelRegistryFixture()
	s.checks = core.EngineCheckOverview{Supported: false}
	tt := ui.NewTester(s.view, 1120, 2000)
	if !tt.HasText("当前 CLI 不支持引擎核验，所列引擎均未核验。") || !tt.HasText("未核验（CLI 过旧）") {
		t.Fatal(tt.Texts())
	}
}

// The task page remains the only place that changes the pair, and the removal of
// the models-page picker must not remove the task-page compatibility gate.
func TestTaskPageStillOwnsCombinationSelection(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(func(c *ui.Context) { s.candidateView(c) }, 1120, 1400)
	for _, want := range []string{"引擎", "测评模型", "开始测评"} {
		if !tt.HasText(want) {
			t.Fatalf("task page lost %q: %v", want, tt.Texts())
		}
	}
	click(t, tt, "请选择模型")
	if err := tt.ChooseMenuItem("model.gguf · GGUF"); err != nil {
		t.Fatal(err)
	}
	if s.model != "model.gguf" {
		t.Fatal("task page model choice did not update the selection", s.model)
	}
}
