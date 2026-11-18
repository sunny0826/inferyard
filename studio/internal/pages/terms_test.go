package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// The main flow uses the approved display terms; internal identifiers stay in
// the advanced details. Numeric evidence and behaviour are unchanged.
func TestMainFlowUsesDisplayTerms(t *testing.T) {
	s := testState()
	s.selected = "dashboard"
	s.task.stage = "run"
	s.tasks = []core.RunTask{{TaskId: "active", Kind: "run", State: "running", Bundle: ptr("zh-core")}, {TaskId: "waiting", Kind: "run", State: "starting", Queued: true}}
	tt := ui.NewTester(s.view, 1120, 2600)
	texts := strings.Join(tt.Texts(), " | ")
	for _, banned := range []string{"运行 benchmark", "启动与绑定", "分母为冻结选中题集合", "单核等效", "至少选择一个题包"} {
		if strings.Contains(texts, banned) {
			t.Fatalf("main flow still shows %q: %s", banned, texts)
		}
	}
	for _, want := range []string{"配置测评", "连接并核验服务", "运行当前测评题集", "本次测评的题以开始运行时固定"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestCandidateStageKeepsTechnicalIdentifiersInDetails(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.candidateView, 1120, 1400)
	if tt.HasText("候选引擎") {
		t.Fatal("main flow still says 候选引擎", tt.Texts())
	}
	if !tt.HasText("引擎") || !tt.HasText("开始测评") {
		t.Fatal(tt.Texts())
	}
}
