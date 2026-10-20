package pages

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestDashboardEnvironmentVisibilityAndOrder(t *testing.T) {
	s := testState()
	s.ready.CLI.Status = "not_found"
	tt := ui.NewTester(s.view, 1120, 3200)
	if !tt.HasText("环境准备") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"工作区进度", "阶段步骤 4"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("unexpected section %s", label)
		}
	}
	s.ready.CLI.Status = "ok"
	s.ready.Workspace.Initialized = true
	s.ready.Results.Exists = true
	s.ready.Models.Exists = true
	tt.Frame()
	for _, label := range []string{"安装 InferYard CLI", "环境准备", "新工作区目录"} {
		if tt.HasText(label) {
			t.Fatalf("ready environment still shows %s", label)
		}
	}
	s.ready.Models.Exists = false
	tt.Frame()
	if !tt.HasText("环境准备") || tt.HasText("新工作区目录") {
		t.Fatal(tt.Texts())
	}
}

func TestDashboardOverviewStageMarkers(t *testing.T) {
	for _, tc := range []struct {
		action string
		step   int
	}{{"device_check", 1}, {"prepare_candidate", 2}, {"bind", 3}, {"run", 4}, {"resume", 4}, {"report", 5}, {"verify", 5}, {"done", 6}} {
		t.Run(tc.action, func(t *testing.T) {
			s := testState()
			s.progress = &inferyard.WorkspaceProgress{NextAction: tc.action}
			tt := ui.NewTester(s.conclusionView, 1120, 800)
			current := 0
			for i, label := range []string{"准备", "设备检查", "benchmark", "启动与绑定", "运行", "报告"} {
				want := label
				if i < tc.step {
					want = "✓  " + label
				}
				if !tt.HasText(want) || (i >= tc.step && tt.HasText("✓  "+label)) {
					t.Fatalf("stage %d: %v", tc.step, tt.Texts())
				}
				for _, n := range modelUINodes(tt, "description", "当前阶段："+label) {
					text := n.FieldByName("text").String()
					if text != "" && text != label {
						continue
					}
					current++
					if i != tc.step {
						t.Fatalf("wrong current stage %s", label)
					}
				}
			}
			wantCurrent := 1
			if tc.step == 6 {
				wantCurrent = 0
			}
			if current != wantCurrent {
				t.Fatalf("current nodes %d, want %d", current, wantCurrent)
			}
		})
	}
}

func TestDashboardMissingAndUnknownProgress(t *testing.T) {
	s := testState()
	s.progress = nil
	s.refreshFailure = "进度读取失败详情"
	tt := ui.NewTester(s.view, 1120, 2400)
	if !tt.HasText("无法读取工作区进度，请刷新重试") || !tt.HasText(s.refreshFailure) || slices.Contains(tt.Texts(), "工作区进度") {
		t.Fatal(tt.Texts())
	}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "future_action"}
	tt.Frame()
	if !tt.HasText("未识别的进度阶段：future_action") || tt.HasText("无法读取工作区进度，请刷新重试") {
		t.Fatal(tt.Texts())
	}
}

func TestDashboardTrackHeadingLevel(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) { stepTrack(c, "6", "报告核验", func() {}) }, 800, 600)
	headings := 0
	designNodes(tt, func(n reflect.Value, _ bool) {
		if n.FieldByName("role").Uint() == uint64(ui.RoleHeading) {
			headings++
			if n.FieldByName("text").String() != "报告核验" || n.FieldByName("level").Int() != 2 {
				t.Fatal("track must have only its level-two title as a heading")
			}
		}
	})
	if headings != 1 {
		t.Fatalf("heading count %d", headings)
	}
}

func TestDashboardBenchmarkHasOneHeading(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate"}
	tt := ui.NewTester(s.view, 1120, 2400)
	headings := 0
	designNodes(tt, func(n reflect.Value, _ bool) {
		if n.FieldByName("role").Uint() == uint64(ui.RoleHeading) &&
			n.FieldByName("text").String() == "benchmark" {
			headings++
		}
	})
	if headings != 1 {
		t.Fatalf("benchmark heading count %d, want 1", headings)
	}
	for _, label := range []string{"候选引擎", "测评模型", "运行 benchmark"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing benchmark control %q", label)
		}
	}
}

func TestDashboardRunningDisablesQuickRunAndOpensDetail(t *testing.T) {
	s := testState()
	s.task.stage = "run"
	s.tasks = []core.RunTask{{TaskId: "active", State: "running"}}
	s.runProgress = &core.RunProgress{Done: 2, Total: ptr(int64(8))}
	s.ctx = context.Background()
	entered := make(chan struct{}, 10)
	s.ctx = refreshContext{s.ctx, entered}
	tt := ui.NewTester(s.view, 1120, 3200)
	if !tt.HasText("2 / 8 题") || tt.HasText("运行 benchmark") {
		t.Fatal(tt.Texts())
	}
	_ = tt.Click("运行 benchmark")
	if s.busy {
		t.Fatal("quick run started during an active task")
	}
	tt.Announcements()
	click(t, tt, "查看实时详情")
	if s.detailID != "active" || tt.HasText("任务队列") || !tt.HasText("返回运行") {
		t.Fatal(tt.Texts())
	}
	if got := tt.Announcements(); len(got) != 1 || got[0] != "运行详情" {
		t.Fatal(got)
	}
	click(t, tt, "返回运行")
	if s.detailID != "" || !tt.HasText("运行当前题包") {
		t.Fatal(tt.Texts())
	}
	if got := tt.Announcements(); len(got) != 1 || got[0] != "任务" {
		t.Fatal(got)
	}
}

func TestDashboardSingleBundleStillNeedsExplicitConfirmation(t *testing.T) {
	s := testState()
	s.selectedBundles = map[string]bool{}
	tt := ui.NewTester(s.candidateView, 1000, 800)
	click(t, tt, "运行 benchmark")
	if s.busy || !tt.HasText("至少选择一个题包。") {
		t.Fatal(tt.Texts())
	}
}

func TestDashboardHasOneStageTrack(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.view, 1120, 2400)
	tracks := 0
	designNodes(tt, func(n reflect.Value, _ bool) {
		if strings.HasPrefix(n.FieldByName("label").String(), "阶段步骤 ") {
			tracks++
		}
	})
	if tracks != 1 {
		t.Fatalf("stage track count %d", tracks)
	}
}
