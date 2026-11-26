package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func TestButtonVariantsHaveUniformHeight(t *testing.T) {
	labels := []string{
		"标准次按钮",
		"标准主按钮",
		"描边按钮",
		"幽灵按钮",
		"危险按钮",
		"主操作空闲",
		"主操作忙碌",
		"次操作空闲",
		"刷新空闲",
		"刷新旋转",
		"向导提交",
		"外链按钮",
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			button(c, labels[0])
			primaryButton(c, labels[1])
			outlineButton(c, labels[2])
			ghostButton(c, labels[3])
			dangerButton(c, labels[4])
			primaryActionButton(c, labels[5], false)
			primaryActionButton(c, labels[6], true)
			secondaryActionButton(c, labels[7], false)
			refreshButton(c, labels[8], false)
			refreshButton(c, labels[9], true)
			wizardActionButton(c, labels[10], labels[10], false)
			updateExternalButton(c, labels[11])
		})
	}, 1200, 400)
	tt.Frame()

	first, ok := tt.AccessByRole(ui.RoleButton, labels[0])
	if !ok || first.Bounds.H <= 0 {
		t.Fatalf("missing reference button %q: ok=%v bounds=%+v", labels[0], ok, first.Bounds)
	}
	for _, label := range labels[1:] {
		got, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok {
			t.Fatalf("missing button %q", label)
		}
		if math.Abs(float64(got.Bounds.H-first.Bounds.H)) > 0.01 {
			t.Fatalf("button %q height = %.2f, want %.2f (matching %q)", label, got.Bounds.H, first.Bounds.H, labels[0])
		}
	}

	// Verify Services onboarding setup card buttons match "+ 启动模型" height (Screenshot 1 regression).
	s := testState()
	s.selected = "services"
	s.loaded = true
	s.serveSupported = true
	s.ready.CLI.Status = "not_found"
	svcTT := ui.NewTester(s.servicesView, 1120, 800)
	svcTT.Frame()

	prepBtn, okPrep := svcTT.AccessByRole(ui.RoleButton, "准备环境")
	settingsBtn, okSet := svcTT.AccessByRole(ui.RoleButton, "前往设置")
	startModelBtn, okStart := svcTT.AccessByRole(ui.RoleButton, "+ 启动模型")
	if !okPrep || !okSet || !okStart {
		t.Fatalf("expected setup/header buttons visible, got prep=%v settings=%v start=%v", okPrep, okSet, okStart)
	}
	if math.Abs(float64(prepBtn.Bounds.H-settingsBtn.Bounds.H)) > 0.01 ||
		math.Abs(float64(prepBtn.Bounds.H-startModelBtn.Bounds.H)) > 0.01 {
		t.Fatalf("services buttons height mismatch: 准备环境=%.2f 前往设置=%.2f +启动模型=%.2f",
			prepBtn.Bounds.H, settingsBtn.Bounds.H, startModelBtn.Bounds.H)
	}

	// Verify inlineButton and inlineDangerButton share identical height.
	inlineTT := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			inlineButton(c, components.ButtonPrimary, "行内主按钮")
			inlineButton(c, components.ButtonOutline, "行内描边")
			inlineDangerButton(c, "行内危险")
		})
	}, 600, 200)
	inlineTT.Frame()
	inPrimary, _ := inlineTT.AccessByRole(ui.RoleButton, "行内主按钮")
	inOutline, _ := inlineTT.AccessByRole(ui.RoleButton, "行内描边")
	inDanger, _ := inlineTT.AccessByRole(ui.RoleButton, "行内危险")
	if math.Abs(float64(inPrimary.Bounds.H-inOutline.Bounds.H)) > 0.01 ||
		math.Abs(float64(inPrimary.Bounds.H-inDanger.Bounds.H)) > 0.01 {
		t.Fatalf("inline button height mismatch: primary=%.2f outline=%.2f danger=%.2f",
			inPrimary.Bounds.H, inOutline.Bounds.H, inDanger.Bounds.H)
	}
}

func TestBenchmarksPromotesRunHistoryAndDrawerInteractions(t *testing.T) {
	s := readyState("benchmarks")
	s.benchFlowOpen = false
	s.registry.Models = []core.ModelAsset{
		{Name: "Qwen-7B.gguf", Path: "/models/Qwen-7B.gguf", Format: "gguf", Available: true},
		{Name: "Llama-8B.gguf", Path: "/models/Llama-8B.gguf", Format: "gguf", Available: true},
	}
	rate := 7.0 / 13.0
	batchID := "batch-01"
	runComplete := core.RunSummary{
		RunId: "run-complete", Path: "/results/run-complete",
		ModelName: ptr("Qwen-7B.gguf"), Bundle: ptr("zh-core"),
		Status: "complete", Sealed: true, Completeness: ptr("complete"),
		StartedAt: ptr("2026-10-16T23:55:00Z"), FinishedAt: ptr("2026-10-16T23:58:12Z"),
		Score: &core.RunScore{Passed: 7, Valid: 13, Rate: &rate},
	}
	runResumable := core.RunSummary{
		RunId: "run-partial", Path: "/results/run-partial",
		BatchId:   &batchID,
		ModelName: ptr("Llama-8B.gguf"), Bundle: ptr("zh-svg-pelican"),
		Status: "partial", Sealed: false, Resumable: true,
		StartedAt: ptr("2026-10-17T01:00:00Z"),
	}
	s.history = core.RunHistoryResponse{
		Batches:        []core.BatchSummary{{BatchId: batchID, Path: "/results/batch-01", Runs: []core.RunSummary{runResumable}}},
		StandaloneRuns: []core.RunSummary{runComplete},
	}
	s.reports = []core.ReportSummary{{
		ReportId: "rep-complete",
		Path:     "/reports/rep-complete",
		RunIds:   []string{"run-complete"},
		RunSummaries: []core.ReportListRun{{
			RunID: "run-complete", Model: "Qwen-7B.gguf", BundleLabel: "zh-core",
		}},
	}}

	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	// 1. Top-level tabs on Benchmarks expose Active & Recent + Run History.
	tasksTab, ok := tt.AccessByRole(ui.RoleTab, "当前任务")
	if !ok || !tasksTab.Selected {
		t.Fatalf("expected 当前任务 tab selected on Benchmarks, got ok=%v info=%+v", ok, tasksTab)
	}
	historyTab, ok := tt.AccessByRole(ui.RoleTab, "运行历史")
	if !ok || historyTab.Selected {
		t.Fatalf("expected 运行历史 tab visible on Benchmarks, got ok=%v info=%+v", ok, historyTab)
	}

	// 2. Unreported resumable run is surfaced directly in Recently Completed!
	if !tt.HasText("Llama-8B.gguf · zh-svg-pelican") || !tt.HasText("可续测") {
		t.Fatalf("expected unreported resumable run in Recently Completed, texts=%v", tt.Texts())
	}

	// 3. Clicking the recent history run row opens the detail drawer with Resume action.
	click(t, tt, "Llama-8B.gguf · zh-svg-pelican")
	tt.Frame()
	if s.historyDetailPath != "/results/run-partial" {
		t.Fatalf("expected history detail drawer open for /results/run-partial, got %q", s.historyDetailPath)
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, "运行详情"); !ok {
		t.Fatalf("expected 运行详情 drawer dialog open")
	}
	click(t, tt, "续测")
	tt.Frame()
	if s.resumeSource == nil || s.resumeSource.RunId != "run-partial" || s.historyDetailPath != "" {
		t.Fatalf("expected clicking 续测 to hand off cleanly to resumeDialog, got resumeSource=%+v detailPath=%q", s.resumeSource, s.historyDetailPath)
	}
	s.closeResumeDialog()
	tt.Frame()

	// 4. Switch to Run History tab on Benchmarks via "查看全部运行历史 ›".
	click(t, tt, "查看全部运行历史 ›")
	tt.Frame()
	if s.benchmarksTab != benchmarksTabHistory {
		t.Fatalf("expected benchmarksTab=%d, got %d", benchmarksTabHistory, s.benchmarksTab)
	}
	if !tt.HasText("Qwen-7B.gguf") || !tt.HasText("Llama-8B.gguf") {
		t.Fatalf("expected both history runs in table, texts=%v", tt.Texts())
	}

	// 5. Filtering by query narrows visible rows.
	s.historyQuery = "qwen"
	tt.Frame()
	if !tt.HasText("Qwen-7B.gguf") || tt.HasText("Llama-8B.gguf") {
		t.Fatalf("expected query filter 'qwen' to isolate Qwen-7B.gguf, texts=%v", tt.Texts())
	}

	// 6. Opening standalone completed run shows View Report + Run Again, without flat defensive disclaimer.
	rowInfo, ok := tt.AccessByRole(ui.RoleRow, "run-complete")
	if !ok {
		t.Fatalf("expected table row 'run-complete'")
	}
	tt.ClickAt(rowInfo.Bounds.X+40, rowInfo.Bounds.Y+rowInfo.Bounds.H/2)
	tt.Frame()
	if s.historyDetailPath != "/results/run-complete" {
		t.Fatalf("expected clicking table row to open detail drawer, got %q", s.historyDetailPath)
	}
	if tt.HasText("独立运行不支持续测。") {
		t.Fatalf("must not render flat defensive disclaimer in history detail drawer")
	}
	click(t, tt, "同配置再测")
	tt.Frame()
	if !s.benchFlowOpen || s.benchmarksTab != benchmarksTabTasks || s.model != "Qwen-7B.gguf" {
		t.Fatalf("expected 同配置再测 to open benchmark setup with preset model, got flow=%v tab=%d model=%q",
			s.benchFlowOpen, s.benchmarksTab, s.model)
	}

	// 7. While inside benchmark flow, top-right "运行历史" button jumps straight back to Run History tab.
	click(t, tt, "运行历史")
	tt.Frame()
	if s.benchFlowOpen || s.benchmarksTab != benchmarksTabHistory {
		t.Fatalf("expected flow header 运行历史 button to jump to history tab, got flow=%v tab=%d", s.benchFlowOpen, s.benchmarksTab)
	}
}
