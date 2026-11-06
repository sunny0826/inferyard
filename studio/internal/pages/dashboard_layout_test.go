package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

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

func TestDashboardRunningDisablesQuickRunAndOpensDetail(t *testing.T) {
	s := testState()
	s.task.stage = "run"
	s.tasks = []core.RunTask{{TaskId: "active", State: "running"}}
	s.runProgress = &core.RunProgress{Done: 2, Total: ptr(int64(8))}
	s.ctx = refreshContext{context.Background(), make(chan struct{}, 10)}
	tt := ui.NewTester(s.view, 1120, 3200)
	if !tt.HasText("2 / 8 题") || tt.HasText("运行 benchmark") {
		t.Fatal(tt.Texts())
	}
	_ = tt.Click("运行 benchmark")
	if s.busy {
		t.Fatal("quick run started during an active task")
	}
	click(t, tt, "查看实时详情")
	if s.detailID != "active" || !tt.HasText("返回运行") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "返回运行")
	if s.detailID != "" || !tt.HasText("运行当前题包") {
		t.Fatal(tt.Texts())
	}
}
