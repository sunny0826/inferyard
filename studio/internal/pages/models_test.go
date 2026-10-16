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
			{Name: "model.gguf", SizeBytes: ptr(int64(4 << 30)), Architecture: "llama", ContextLength: ptr(int64(8192)), Available: true},
			{Name: "new.gguf", Available: true},
			{Name: "broken.gguf", Reason: "invalid_or_unreadable_gguf"},
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

func TestModelsPageZonesAndNoDownloadCards(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.model, s.engine = "model.gguf", "engine-2 · llama-server"
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, want := range []string{
		"选择模型与引擎", "组合状态", "用这个组合新开任务", "本机运行的服务",
		"model.gguf × engine-2 · llama-server",
		"Studio 启动的服务随 Studio 退出关闭，不会自动重启。",
	} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	for _, gone := range []string{
		"下载任务", "下载预置模型", "本地模型清单", "引擎清单", "创建模型下载",
		"绑定操作在 Dashboard 进行",
		"适配性按引擎版本核验与模型格式判断",
		"启动使用冻结候选的启动参数",
		"discover 发现结果，只读",
	} {
		if tt.HasText(gone) {
			t.Fatal("models page keeps download/registry UI: "+gone, tt.Texts())
		}
	}
	assertModelsReadOnly(t, tt)
	if len(modelUINodes(tt, "text", "绑定")) != 0 {
		t.Fatal("models page must not offer binding", tt.Texts())
	}
}

func TestModelsEmptyRegistryGuidance(t *testing.T) {
	s := testState()
	s.selected = "models"
	tt := ui.NewTester(s.view, 1120, 2000)
	for _, want := range []string{"暂无可选组合；到下载页下载引擎与模型。"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	assertModelsReadOnly(t, tt)
	if tt.HasText("请选择模型") || tt.HasText("请选择引擎") || tt.HasText("刷新服务列表") || tt.HasText("用这个组合新开任务") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "到下载页")
	if s.selected != "downloads" {
		t.Fatal(s.selected)
	}
	tt.Frame()
	if !tt.HasText("新建下载任务") || !tt.HasText("下载引擎") {
		t.Fatal(tt.Texts())
	}
}

func TestEngineOptionAnnotations(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	options := s.engineOptions()
	if len(options) != 2 {
		t.Fatal(options)
	}
	if options[0].disabled || options[0].note != "可用 · 版本已核验" {
		t.Fatalf("%+v", options[0])
	}
	if !options[1].disabled || options[1].note != "不可用 · 引擎运行库不完整或库链接无效" {
		t.Fatalf("%+v", options[1])
	}
	// Selecting a model never changes the engine verdict annotation.
	s.model = "model.gguf"
	if got := s.engineOptions()[0].note; got != "可用 · 版本已核验" {
		t.Fatal(got)
	}
	s.model = ""
	s.checks.Engines["engine-2 · llama-server"] = core.EngineEligibility{State: core.EngineCheckChecking}
	if got := s.engineOptions()[0]; got.note != "核验中…" || !got.disabled {
		t.Fatalf("%+v", got)
	}
	// Reviewed degradation: outdated CLI stays selectable; other failures disable.
	s.checks.Engines["engine-2 · llama-server"] = core.EngineEligibility{State: core.EngineCheckUnknown, Reason: "cli_outdated"}
	if got := s.engineOptions()[0]; got.note != "未核验（CLI 过旧，建议升级）" || got.disabled {
		t.Fatalf("%+v", got)
	}
	s.checks.Engines["engine-2 · llama-server"] = core.EngineEligibility{State: core.EngineCheckUnknown, Reason: "engine_version_query_failed"}
	if got := s.engineOptions()[0]; got.note != "未核验 · 引擎版本查询失败或超时" || !got.disabled {
		t.Fatalf("%+v", got)
	}
	// An active service locks every option.
	s.engineState.State = "running"
	for _, option := range s.engineOptions() {
		if !option.disabled {
			t.Fatalf("%+v", option)
		}
	}
}

func TestModelOptionAnnotations(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	options := s.modelOptions()
	if len(options) != 3 || options[0].note != "" || options[0].disabled {
		t.Fatalf("%+v", options)
	}
	if !options[2].disabled || options[2].note != "不可用 · 模型不存在、不可读或 GGUF 元数据无效" {
		t.Fatalf("%+v", options[2])
	}
	// Selecting an engine never changes the model annotations either.
	s.engine = "engine-2 · llama-server"
	if got := s.modelOptions()[0].note; got != "" {
		t.Fatal(got)
	}
}

func TestEngineFitTexts(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	if got := s.engineFit(); got != "1 个可用 · 1 个不可用" {
		t.Fatal(got)
	}
	s.checks.Checking = 2
	if got := s.engineFit(); got != "核验中…（1 个可用 · 1 个不可用）" {
		t.Fatal(got)
	}
	s.checks = core.EngineCheckOverview{Supported: false}
	if got := s.engineFit(); got != "未核验（CLI 过旧，建议升级）" {
		t.Fatal(got)
	}
}

func TestModelsSelectionRevokeWarning(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.engine = "release · inference-engine"
	tt := ui.NewTester(s.selectionView, 1120, 900)
	for _, want := range []string{"当前选择不可用", "引擎运行库不完整或库链接无效", "请重新选择"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
}

func TestModelsPickerSelectsThroughUI(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	tt := ui.NewTester(s.selectionView, 1120, 1000)
	click(t, tt, "请选择引擎")
	tt.Frame()
	if !tt.HasText("可用 · 版本已核验") || !tt.HasText("不可用 · 引擎运行库不完整或库链接无效") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "engine-2 · llama-server")
	tt.Frame()
	if s.engine != "engine-2 · llama-server" {
		t.Fatal(s.engine)
	}
	click(t, tt, "engine-2 · llama-server")
	tt.Frame()
	click(t, tt, "搜索引擎")
	tt.Type("zzz")
	tt.Frame()
	if !tt.HasText("没有匹配的引擎，请修改搜索词。") {
		t.Fatal(tt.Texts())
	}
}

func TestModelsPageHidesPaths(t *testing.T) {
	s := testState()
	s.selected = "models"
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.model, s.engine = "model.gguf", "engine-2 · llama-server"
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/secret/candidates/zh-core-1", LogPath: "/secret/logs/engine.log"}
	s.candidateMatches = func(candidate, model, engine string) (bool, bool) { return false, true }
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
	if !tt.HasText("运行中 · 另一个组合的服务（候选 zh-core-1）") {
		t.Fatal(tt.Texts())
	}
}
