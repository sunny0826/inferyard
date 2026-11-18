//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func preparePreview(t *testing.T) (*previewSession, *ui.Tester) {
	t.Helper()
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	click(t, tt, "准备环境")
	if !p.state.busy || p.state.ready.Workspace.Initialized {
		t.Fatal("environment preparation skipped its pending state")
	}
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.step() != 1 || !p.state.busy {
		t.Fatal("device-check stage was skipped")
	}
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.busy || !p.state.preflightReady() || !tt.HasText("开始测评") {
		t.Fatal("environment preparation did not unlock benchmark", tt.Texts())
	}
	return p, tt
}

func TestPreviewCompleteWorkflowWithoutSceneSelector(t *testing.T) {
	p, tt := preparePreview(t)
	p.state.model = "Preview-Small.gguf"
	tt.Frame()
	click(t, tt, "开始测评")
	p.advance(400 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("连接并核验服务") || p.state.engineState.State != "starting" {
		t.Fatal("missing service startup transition")
	}
	p.advance(1300 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("10 / 120 题") {
		t.Fatal("run did not advance", tt.Texts())
	}
	click(t, tt, "查看实时详情")
	first := p.state.detail.Progress.Done
	p.advance(time.Second)
	tt.Frame()
	if p.state.detail.Progress.Done <= first || len(p.state.detail.Resources) < 2 {
		t.Fatal("open detail did not receive live progress")
	}
	click(t, tt, "返回运行")
	click(t, tt, "下载")
	p.advance(20 * time.Second)
	click(t, tt, "报告")
	if len(p.state.historyRuns()) != 2 || len(p.state.reports) != 1 || p.state.busy {
		t.Fatal("background workflow did not finish both bundles")
	}
	p.state.selected = "dashboard"
	tt.Frame()
	click(t, tt, "查看报告")
	if p.state.openedReport.reference != p.state.task.report || !tt.HasText("Preview-Small.gguf") {
		t.Fatal("task did not open its own generated report", tt.Texts())
	}
	if !tt.HasText("120 个有效样本") {
		t.Fatal("generated report must count every request latency sample", tt.Texts())
	}
	click(t, tt, "新任务")
	if len(p.state.reports) != 1 || len(p.state.historyRuns()) != 2 {
		t.Fatal("new task erased previous results")
	}
	index, err := p.report(p.state.reports[0].ReportId)
	if err != nil || len(index.Runs) != 2 {
		t.Fatal("generated report does not contain both runs", err)
	}
	assertSimulatedReportRuns(t, index.Runs)
	if p.state.service != nil || p.state.taskCancel != nil {
		t.Fatal("simulation reached real runtime")
	}
}

func assertSimulatedReportRuns(t *testing.T, runs []core.ReportRun) {
	t.Helper()
	wantRequests := map[string]int{"zh-core": 120, "zh-svg-pelican": 1}
	for _, run := range runs {
		want, ok := wantRequests[run.Facets.BundleLabel]
		if !ok || run.Model != "Preview-Small.gguf" || len(run.Requests) != want || run.Source.RunID == "preview-run" {
			t.Fatal("generated report did not use the actual simulated run", run.Model, run.Facets.BundleLabel, len(run.Requests), run.Source.RunID)
		}
	}
}

func TestPreviewCancellationPreservesOtherWork(t *testing.T) {
	for _, elapsed := range []time.Duration{600 * time.Millisecond, 2 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			p, tt := preparePreview(t)
			saveDownloadName(t, p, tt, "kept-model")
			click(t, tt, "下载")
			click(t, tt, "下载模型")
			click(t, tt, "新任务")
			click(t, tt, "开始测评")
			p.advance(elapsed)
			tt.Frame()
			if elapsed < time.Second {
				confirmLabels(t, tt, "新任务", "确认开始新任务")
			} else {
				click(t, tt, "查看实时详情")
				confirmLabels(t, tt, "取消任务", "确认取消任务")
			}
			if !p.state.newTaskPending {
				t.Fatal("cancel skipped draining")
			}
			p.advance(20 * time.Second)
			tt.Frame()
			if p.state.task.stage != "prepare_candidate" || p.state.busy || p.state.running() != nil || len(p.state.reports) != 0 {
				t.Fatal("cancelled task resumed or produced a report")
			}
			if len(p.state.downloads) != 1 || p.state.downloads[0].State != "done" || p.state.settings.Settings.ModelDownloadName != "kept-model" {
				t.Fatal("new task erased downloads or settings")
			}
			for _, task := range p.state.tasks {
				if task.State != "cancelled" {
					t.Fatal("cancelled run lost its terminal state")
				}
			}
		})
	}
}

// saveDownloadName enters the settings page, types a name and saves it.
func saveDownloadName(t *testing.T, p *previewSession, tt *ui.Tester, name string) {
	t.Helper()
	click(t, tt, "设置")
	p.state.modelName = name
	tt.Frame()
	click(t, tt, "保存下载目录名")
}

// confirmLabels presses an entry and then its inline confirmation.
func confirmLabels(t *testing.T, tt *ui.Tester, entry, confirm string) {
	t.Helper()
	click(t, tt, entry)
	click(t, tt, confirm)
}

func TestPreviewDownloadsCancelRetryAndPopulateModels(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "下载")
	click(t, tt, "下载模型")
	click(t, tt, "下载模型")
	first, second := p.state.downloads[0].TaskId, p.state.downloads[1].TaskId
	confirmLabels(t, tt, "取消下载", "确认取消下载")
	p.advance(3 * time.Second)
	tt.Frame()
	if p.state.downloads[0].TaskId != first || p.state.downloads[0].State != "cancelled" || p.state.downloads[1].TaskId != second || p.state.downloads[1].State != "done" {
		t.Fatal("cancelling one download affected another")
	}
	click(t, tt, "下次操作模拟失败")
	p.state.modelURL = "invalid-url"
	tt.Frame()
	click(t, tt, "下载模型")
	if !p.failNext || len(p.state.downloads) != 2 {
		t.Fatal("invalid input consumed failure or created a download")
	}
	p.state.modelURL = "https://example.invalid/model.gguf"
	tt.Frame()
	click(t, tt, "下载模型")
	p.advance(2 * time.Second)
	tt.Frame()
	failed := p.state.downloads[2]
	if failed.State != "failed" {
		t.Fatal("download failure was not visible")
	}
	click(t, tt, "重新创建下载任务")
	p.advance(3 * time.Second)
	tt.Frame()
	retry := p.state.downloads[3]
	if retry.State != "done" || retry.TaskId == failed.TaskId || retry.OutName == failed.OutName || p.state.downloads[2].State != "failed" {
		t.Fatal("retry did not preserve old download and allocate a new result")
	}
	click(t, tt, "模型")
	assertModelsInventory(t, tt, retry.OutName+" · "+emptyText(retry.FileName))
}

// assertModelsInventory checks the read-only listing: the completed download is
// present and no selection control remains.
func assertModelsInventory(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if !tt.HasText(name) {
		t.Fatal("completed download is not listed on the read-only models page", tt.Texts())
	}
	if tt.HasText("请选择模型") {
		t.Fatal("models page still offers a selection control")
	}
}

func TestPreviewSettingsDeviceAndReportRecovery(t *testing.T) {
	p, tt := preparePreview(t)
	saveDownloadName(t, p, tt, "saved-model")
	click(t, tt, "报告")
	click(t, tt, "设置")
	if p.state.settings.Settings.ModelDownloadName != "saved-model" {
		t.Fatal("saved name disappeared after navigation")
	}
	click(t, tt, "下载")
	click(t, tt, "下载模型")
	if p.state.downloads[0].OutName != "saved-model" {
		t.Fatal("saved name did not affect the next download")
	}
	click(t, tt, "设备")
	click(t, tt, "刷新设备")
	if !p.state.device.Refreshing {
		t.Fatal("device refresh did not enter pending state")
	}
	p.advance(time.Second)
	tt.Frame()
	if p.state.device.Refreshing || p.state.device.CapturedAt == nil || !tt.HasText("Preview CPU") {
		t.Fatal("device refresh did not complete")
	}
	p.loadScene(previewSceneIndex(t, "报告 · 读取失败"))
	tt.Frame()
	click(t, tt, "重新读取报告")
	if p.state.openedReport.failure != nil || !tt.HasText("各类任务表现") {
		t.Fatal("report retry did not recover")
	}
}

func TestPreviewResetAndCloseInvalidatePendingEvents(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "开始测评")
	stale := p.jobs[0]
	p.loadScene(previewSceneIndex(t, "空数据"))
	p.jobs = append(p.jobs, stale)
	p.advance(time.Minute)
	if p.state.progress != nil || p.state.busy || len(p.state.reports) != 0 {
		t.Fatal("stale scenario event mutated the replacement state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p = newPreviewSession(ctx)
	p.dispatch(previewPrepareEnvironment)
	cancel()
	p.advance(time.Minute)
	if p.state.ready.Workspace.Initialized {
		t.Fatal("closed preview still completed pending work")
	}
}

func TestPreviewNewTaskFinishesServiceStop(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "开始测评")
	p.advance(20 * time.Second)
	tt.Frame()
	click(t, tt, "引擎 · 运行中")
	confirmLabels(t, tt, "停止引擎", "确认停止引擎")
	click(t, tt, "新任务")
	p.advance(time.Second)
	tt.Frame()
	if p.state.engineState.State != "stopped" || p.state.busy || p.state.newTaskPending {
		t.Fatal("new task left service stopping or task pending", p.state.engineState.State)
	}
}

func TestPreviewNewTaskCancelsPendingHistoricalReport(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "开始测评")
	p.advance(20 * time.Second)
	tt.Frame()
	p.dispatch(previewCreateReport{paths: p.state.task.runs})
	confirmLabels(t, tt, "新任务", "确认开始新任务")
	p.advance(400 * time.Millisecond)
	tt.Frame()
	click(t, tt, "开始测评")
	p.advance(200 * time.Millisecond)
	tt.Frame()
	if !p.state.busy || len(p.state.reports) != 1 {
		t.Fatal("old report completion changed the new task or added a cancelled report")
	}
	p.advance(20 * time.Second)
	if p.state.task.stage != "done" || len(p.state.reports) != 2 {
		t.Fatal("new task did not finish with its own report")
	}
}
