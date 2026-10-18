package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestSpeedOutdatedCLIGuidance(t *testing.T) {
	s := runStateFixture()
	s.speed = &speedWorkspace{selected: true, context: 131072}
	s.checks.Supported = false
	s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckUnknown, Reason: "cli_outdated"}
	tt := ui.NewTester(s.candidateView, 1120, 1600)
	if !tt.HasText("当前 CLI 不支持引擎核验；请在设置中选择支持长上下文速度的 CLI。") {
		t.Fatal("missing actionable CLI diagnosis", tt.Texts())
	}
	if tt.HasText("引擎版本已核验；模型架构与引擎的适配在启动时验证。") || tt.HasText("仅列出核验可用的引擎；默认选中其中最新下载的引擎。") {
		t.Fatal("unverified engine presented as verified", tt.Texts())
	}
	click(t, tt, "开始专项测评")
	if s.busy || s.speed.current != nil {
		t.Fatal("old CLI admitted specialist work")
	}
	click(t, tt, "打开 CLI 设置")
	if s.selected != "settings" || s.speed.context != 131072 || s.model != "model.gguf" {
		t.Fatal("settings navigation discarded the task selection")
	}
}

func TestSpeedAdmissionExplainsBlockingCondition(t *testing.T) {
	for _, tc := range []struct {
		name, hint string
		apply      func(*viewState)
	}{
		{"checking", "正在核验所选引擎，请稍候。", func(s *viewState) {
			s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckChecking}
		}},
		{"ineligible", "所选引擎不可用：引擎运行库不完整或库链接无效。请重新选择。", func(s *viewState) {
			s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"}
		}},
		{"unknown", "所选引擎尚未通过核验，请到模型页查看原因。", func(s *viewState) {
			s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckUnknown}
		}},
		{"preflight", "请先完成工作区设备检查。", func(s *viewState) { s.progress.DevicePreflight.Path = nil }},
		{"busy", "正在处理其他操作，请等待完成。", func(s *viewState) { s.busy = true }},
		{"resetting", "正在结束上一任务，请等待完成。", func(s *viewState) { s.newTaskPending = true }},
		{"model_context", "所选模型最多支持 8K 上下文，不能运行 64K 档；请更换长上下文模型。", func(s *viewState) {
			s.registry.Models = []core.ModelAsset{{Name: s.model, ContextLength: ptr(int64(8192)), Available: true}}
		}},
		{"orphaned", "请先处理遗留服务记录，再开始专项测评。", func(s *viewState) { s.engineState.State = "orphaned" }},
		{"run_active", "已有任务尚未结束，请等待完成或取消后再开始。", func(s *viewState) { s.tasks = []core.RunTask{{State: "running"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := runStateFixture()
			s.speed = &speedWorkspace{selected: true, context: 65536}
			tc.apply(s)
			tt := ui.NewTester(s.candidateView, 1120, 1600)
			if !tt.HasText(tc.hint) {
				t.Fatal("blocking reason missing", tt.Texts())
			}
			click(t, tt, "开始专项测评")
			if s.speed.current != nil {
				t.Fatal("blocked selection created a task")
			}
		})
	}
}

func TestNewTaskFormDoesNotRepeatSpecialtyHistoryErrors(t *testing.T) {
	for _, specialty := range []bool{false, true} {
		s := runStateFixture()
		s.task.stage = "prepare_candidate"
		s.speed = &speedWorkspace{selected: specialty, context: 65536}
		s.tasks = []core.RunTask{
			{Kind: "context_speed", State: "failed", Error: &core.ApiError{Code: "cli_blocked", Message: "previous-specialty-failure"}},
			{Kind: "run", State: "failed", Error: &core.ApiError{Code: "cli_blocked", Message: "ordinary-preparation-failure"}},
		}
		tt := ui.NewTester(s.dashboardView, 1120, 1600)
		if tt.HasText("previous-specialty-failure · cli_blocked") {
			t.Fatal("new task repeats an error owned by specialty history", tt.Texts())
		}
		if !tt.HasText("ordinary-preparation-failure · cli_blocked") || len(s.tasks) != 2 {
			t.Fatal("ordinary feedback or original task history was lost", tt.Texts())
		}
	}
}
