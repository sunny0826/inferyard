package pages

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func compactRunFixture() *viewState {
	s := testState()
	s.ready.Workspace.Initialized, s.ready.Results.Exists, s.ready.Models.Exists = true, true, true
	s.progress = &inferyard.WorkspaceProgress{NextAction: "run", Bundles: []inferyard.BundleProgress{{
		Bundle: "zh-core", Phase: "run_incomplete", NextAction: "run",
		CandidatePath: ptr("/fixture/candidate"), BoundPath: ptr("/fixture/bound"),
	}}}
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
	s.tasks = []core.RunTask{{TaskId: "fixture", State: "running", RunPath: ptr("/fixture/run")}}
	s.runProgress = &core.RunProgress{Done: 2, Total: ptr(int64(120))}
	return &s
}

func TestRunningServiceStaysInCorner(t *testing.T) {
	for _, stage := range []string{"", "run"} {
		for _, state := range []string{"queued", "running", "cancelling"} {
			t.Run(stage+"/"+state, func(t *testing.T) {
				s := compactRunFixture()
				s.task.stage, s.tasks[0].State = stage, state
				tt := ui.NewTester(s.view, 1120, 800)
				status := "引擎 运行中 · 队列 执行中"
				before, ok := tt.Find(status)
				if !ok {
					t.Fatal("missing corner service status", tt.Texts())
				}
				run, ok := tt.Find("运行当前题包")
				if !ok || before.Y >= run.Y {
					t.Fatal("service status is not above run content")
				}
				for _, flags := range []struct{ busy, refreshing bool }{{}, {true, false}, {false, true}, {true, true}} {
					s.busy, s.refreshing = flags.busy, flags.refreshing
					tt.Frame()
					for _, label := range []string{"模型服务", "启动模型服务并绑定", "启动服务", "停止服务", "停止", "执行中"} {
						if slices.Contains(tt.Texts(), label) {
							t.Errorf("active run still renders %q", label)
						}
					}
					after, ok := tt.Find(status)
					if !ok || after != before {
						t.Error("refresh replaced or moved the corner status")
					}
					if !tt.HasText("2 / 120 题") || !tt.HasText("取消运行") {
						t.Error("run progress or cancellation missing")
					}
				}
			})
		}
	}
}

func TestServiceCardStaysCollapsedBetweenTaskRuns(t *testing.T) {
	s := compactRunFixture()
	s.task.stage, s.busy = "run", true
	s.tasks = nil
	tt := ui.NewTester(s.view, 1120, 800)
	if tt.HasText("模型服务") || !tt.HasText("引擎 运行中 · 队列 空") {
		t.Fatal("service expanded before queue refresh or between bundles", tt.Texts())
	}
	s.task.failure, s.busy = core.Err("engine_exited"), false
	s.engineState.State = "failed"
	tt.Frame()
	if !tt.HasText("模型服务") || !tt.HasText("启动服务") {
		t.Fatal("failed task lost service recovery", tt.Texts())
	}
}

func TestActiveEngineStatusSurvivesBackgroundRefresh(t *testing.T) {
	for _, state := range []string{"starting", "running", "stopping", "failed", "orphaned"} {
		s := testState()
		s.engineState.State = state
		tt := ui.NewTester(s.cornerStatus, 800, 600)
		label := "引擎 " + stateLabel(state) + " · 队列 空"
		s.busy, s.refreshing = true, true
		tt.Frame()
		if !tt.HasText(label) || slices.Contains(tt.Texts(), "执行中") {
			t.Errorf("%s status replaced by background work: %v", state, tt.Texts())
		}
	}
}
