package pages

import (
	"context"
	"reflect"
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
	labels := []string{"安装 InferYard CLI", "环境准备", "初始化目标必须是新目录；成功后自动切换工作区。", "工作区进度", "阶段步骤 4", "启动测评", "任务队列", "运行历史"}
	previous := float32(-1)
	for _, label := range labels {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y <= previous {
			t.Fatalf("section order %s: %+v", label, bounds)
		}
		previous = bounds.Y
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
	}{{"", 0}, {"device_check", 1}, {"prepare_candidate", 2}, {"bind", 3}, {"run", 4}, {"resume", 4}, {"report", 5}, {"verify", 5}, {"done", 6}} {
		t.Run(tc.action, func(t *testing.T) {
			s := testState()
			s.progress = nil
			if tc.action != "" {
				s.progress = &inferyard.WorkspaceProgress{NextAction: tc.action}
			}
			tt := ui.NewTester(s.conclusionView, 1120, 800)
			current := 0
			for i, label := range []string{"准备", "设备检查", "候选", "启动与绑定", "运行", "报告核验"} {
				want := label
				if i < tc.step {
					want = "✓  " + label
				}
				if !tt.HasText(want) || (i >= tc.step && tt.HasText("✓  "+label)) {
					t.Fatalf("stage %d: %v", tc.step, tt.Texts())
				}
				for _, n := range modelUINodes(tt, "widget", "Badge") {
					if n.FieldByName("first").Elem().FieldByName("text").String() == label {
						current++
						if i != tc.step {
							t.Fatalf("wrong current stage %s", label)
						}
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
	if !tt.HasText("无法读取工作区进度，请刷新重试") || !tt.HasText(s.refreshFailure) {
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

func TestActivityStatusToolbar(t *testing.T) {
	s := testState()
	for _, state := range []struct{ busy, refreshing bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		s.busy, s.refreshing = state.busy, state.refreshing
		tt := ui.NewTester(s.activityStatus, 800, 600)
		toolbars := 0
		designNodes(tt, func(n reflect.Value, _ bool) {
			if n.FieldByName("role").Uint() == uint64(ui.RoleToolbar) {
				toolbars++
			}
		})
		want := 0
		if state.busy || state.refreshing {
			want = 1
		}
		if toolbars != want || tt.HasText("执行中…") != state.busy || tt.HasText("正在刷新…") != (state.refreshing && !state.busy) {
			t.Fatalf("state %+v toolbars=%d texts=%v", state, toolbars, tt.Texts())
		}
	}
}

func TestDashboardRunningDisablesQuickRunAndOpensDetail(t *testing.T) {
	s := testState()
	s.single, s.confirmed = true, true
	s.tasks = []core.RunTask{{TaskId: "active", State: "running"}}
	s.runProgress = &core.RunProgress{Done: 2, Total: ptr(int64(8))}
	s.ctx = context.Background()
	entered := make(chan struct{}, 10)
	s.ctx = refreshContext{s.ctx, entered}
	tt := ui.NewTester(s.view, 1120, 3200)
	if !tt.HasText("2 / 8 题") || !tt.HasText("当前运行结束后可启动新的测评。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "启动运行")
	if s.busy {
		t.Fatal("quick run started during an active task")
	}
	tt.Announcements()
	click(t, tt, "查看实时详情")
	if s.detailID != "active" || tt.HasText("任务队列") || !tt.HasText("返回 Dashboard") {
		t.Fatal(tt.Texts())
	}
	if got := tt.Announcements(); len(got) != 1 || got[0] != "运行详情" {
		t.Fatal(got)
	}
	click(t, tt, "返回 Dashboard")
	if s.detailID != "" || !tt.HasText("任务队列") {
		t.Fatal(tt.Texts())
	}
	if got := tt.Announcements(); len(got) != 1 || got[0] != "Dashboard" {
		t.Fatal(got)
	}
}

func TestDashboardSingleBundleStillNeedsExplicitConfirmation(t *testing.T) {
	s := testState()
	s.single = true
	tt := ui.NewTester(s.quickRunView, 1000, 800)
	click(t, tt, "启动运行")
	if s.busy || s.confirmed || !tt.HasText("请显式确认只运行一个题包。") {
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
