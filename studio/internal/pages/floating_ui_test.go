package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestPopconfirmClosesOnEscapeWithoutHidingTrigger(t *testing.T) {
	s := testState()
	s.settings.Settings.EngineInstallations = []string{"/opt/llama/installation.json"}
	tt := ui.NewTester(s.localInstallationsView, 1120, 1200)

	click(t, tt, "移除注册")
	if !tt.HasText("确认移除注册") {
		t.Fatalf("popconfirm did not open: %v", tt.Texts())
	}
	if !tt.HasText("移除注册") {
		t.Fatalf("trigger disappeared while popconfirm was open: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("确认移除注册") || s.pendingDestructive != (destructiveRequest{}) {
		t.Fatalf("Escape did not dismiss popconfirm: pending=%+v", s.pendingDestructive)
	}
}

func TestOrphanDiscardRequiresAlertDialogConfirmation(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "orphaned", Error: core.Err("engine_owner_invalid")}
	tt := ui.NewTester(s.stopOrphanButton, 1120, 1200)

	click(t, tt, "仅删除记录（不终止进程）")
	if !tt.HasText("确认仅删除记录") {
		t.Fatalf("discard orphan record did not open confirmation dialog: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("确认仅删除记录") {
		t.Fatalf("Escape did not close orphan discard dialog: %v", tt.Texts())
	}
}

func TestResumeDialogGuardsUnsavedInputAndKeepsCurrentPage(t *testing.T) {
	s := testState()
	s.selected = "reports"
	s.reportsTab = reportsTabHistory
	s.resumeSource = &core.RunSummary{RunId: "run-1", Path: "/results/run-1"}
	s.endpoint = "http://127.0.0.1:8080"
	tt := ui.NewTester(s.view, 1120, 1200)

	if !tt.HasText("续测 run-1") {
		t.Fatalf("resume dialog did not render: %v", tt.Texts())
	}
	if s.selected != "reports" {
		t.Fatalf("resume dialog hijacked page: %q", s.selected)
	}
	click(t, tt, "关闭续测设置")
	if !tt.HasText("放弃未保存的修改？") {
		t.Fatalf("closing dirty resume dialog did not ask confirmation: %v", tt.Texts())
	}
	click(t, tt, "继续编辑")
	if s.resumeSource == nil {
		t.Fatal("continuing edit closed the resume dialog")
	}
	click(t, tt, "关闭续测设置")
	click(t, tt, "放弃修改")
	if s.resumeSource != nil || s.endpoint != "" {
		t.Fatalf("discarding changes left resume state: source=%v endpoint=%q", s.resumeSource, s.endpoint)
	}
}

func TestReportSourcesDialogGuardsCheckedSelectionsOnClose(t *testing.T) {
	s := testState()
	s.history.StandaloneRuns = []core.RunSummary{
		{RunId: "run-1", Path: "/results/run-1"},
		{RunId: "run-2", Path: "/results/run-2"},
	}
	tt := ui.NewTester(s.reportsView, 1120, 1400)

	click(t, tt, "从历史运行生成报告")
	if !s.reportDialogOpen || !tt.HasText("已选 0 / 100") {
		t.Fatalf("report dialog did not open: open=%v texts=%v", s.reportDialogOpen, tt.Texts())
	}
	click(t, tt, "run-1")
	if s.reportChoices.Len() != 1 {
		t.Fatalf("expected 1 selection, got %d", s.reportChoices.Len())
	}
	click(t, tt, "关闭")
	if !tt.HasText("放弃当前选择？") {
		t.Fatalf("closing dialog with selections did not confirm: %v", tt.Texts())
	}
	click(t, tt, "放弃选择")
	if s.reportDialogOpen || s.reportChoices.Len() != 0 {
		t.Fatalf("discard did not close and clear: open=%v len=%d", s.reportDialogOpen, s.reportChoices.Len())
	}
}
