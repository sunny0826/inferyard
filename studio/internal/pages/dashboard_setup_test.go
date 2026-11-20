package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// candidateState is the fresh configuration step: the device preflight is
// recorded and the workspace asks to prepare a candidate.
func candidateState() viewState {
	s := testState()
	s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	s.progress.NextAction = "prepare_candidate"
	s.progress.DevicePreflight = inferyard.ProgressPreflight{Status: "ok", Path: ptr("/w/preflight")}
	s.registry = core.Registry{
		Models:  []core.ModelAsset{{Name: "Preview-Model-Q4.gguf", Available: true, Architecture: "llama", SizeBytes: ptr(int64(4 << 30))}},
		Engines: []core.EngineAsset{{Name: "preview-engine"}},
	}
	s.engine, s.model = "preview-engine", "Preview-Model-Q4.gguf"
	return s
}

// The specialty benchmark replaces the six-stage flow, so the configuration
// step must not keep the numbered rail that only makes sense inside it.
func TestSpeedTypeDropsOrphanedStepNumber(t *testing.T) {
	s := candidateState()
	tt := ui.NewTester(s.stageView, 1120, 1800)
	if !tt.HasText("阶段编号 3") {
		t.Fatalf("regular benchmark lost its numbered stage: %v", tt.Texts())
	}
	click(t, tt, "长上下文速度")
	if s.speed.selected != true {
		t.Fatal("the switch did not choose the specialty type")
	}
	if tt.HasText("阶段编号 3") || tt.HasText("阶段步骤 3") {
		t.Fatalf("specialty type kept an orphaned step badge: %v", tt.Texts())
	}
	if !tt.HasText(termBenchmarkStage) || !tt.HasText("上下文档位") {
		t.Fatalf("specialty setup is not on the configuration card: %v", tt.Texts())
	}
	click(t, tt, "常规测评")
	if s.speed.selected {
		t.Fatal("the switch did not return to the regular benchmark")
	}
}

// The benchmark type sits above the fields it reshapes.
func TestSpeedTypeSwitchPrecedesModelFields(t *testing.T) {
	s := candidateState()
	tt := ui.NewTester(s.candidateView, 1120, 1800)
	typeAt, ok := tt.Find("测评类型")
	if !ok {
		t.Fatalf("no 测评类型 label: %v", tt.Texts())
	}
	modelAt, ok := tt.Find("测评模型")
	if !ok {
		t.Fatalf("no 测评模型 field: %v", tt.Texts())
	}
	if typeAt.Y >= modelAt.Y {
		t.Fatalf("测评类型 at %v is not above 测评模型 at %v", typeAt.Y, modelAt.Y)
	}
}

// The dropdown names the model, so the line under it carries metadata only.
func TestModelMetadataDoesNotRepeatTheName(t *testing.T) {
	s := candidateState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	if tt.HasText("测评模型：") {
		t.Fatalf("the summary repeats the field name and value: %v", tt.Texts())
	}
	if !tt.HasText("架构 llama · 约 4.0 GiB") {
		t.Fatalf("model metadata is missing: %v", tt.Texts())
	}
	s.registry.Models[0].Architecture, s.registry.Models[0].SizeBytes = "", nil
	tt.Frame()
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "架构") || strings.Contains(text, "GiB") {
			t.Fatalf("a model without metadata invented a line: %v", tt.Texts())
		}
	}
}

// Evaluation sets are a labeled field with plain-language descriptions; the
// checkbox keeps its own accessible name.
func TestBundleSelectionIsLabeledAndDescribed(t *testing.T) {
	s := candidateState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	for _, want := range []string{termBundle, "中文基础能力", "SVG 绘图专项"} {
		if !tt.HasText(want) {
			t.Fatalf("bundle selection missed %q: %v", want, tt.Texts())
		}
	}
	click(t, tt, "zh-svg-pelican")
	if got := s.checkedBundles(); len(got) != 1 || got[0] != inferyard.DefaultBundleCore {
		t.Fatalf("the checkbox no longer toggles its bundle: %v", got)
	}
}

// The preparation card offers the settings shortcut its copy mentions and
// states the three milestones a first run goes through.
func TestSetupGuideOffersSettingsAndMilestones(t *testing.T) {
	s := testState()
	s.ready.CLI.Status = "not_found"
	s.selected = "dashboard"
	tt := ui.NewTester(s.setupGuide, 1120, 1200)
	for _, want := range []string{"首次使用流程：1. 准备运行环境 → 2. 获取引擎与模型 → 3. 开始首次测评", "准备环境"} {
		if !tt.HasText(want) {
			t.Fatalf("setup guide missed %q: %v", want, tt.Texts())
		}
	}
	click(t, tt, "前往设置")
	if s.selected != "settings" {
		t.Fatalf("前往设置 did not open settings: %q", s.selected)
	}
}
