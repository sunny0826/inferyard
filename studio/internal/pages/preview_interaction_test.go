//go:build ui_debug && !release

package pages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPreviewBenchmarkButtonsStartWork(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	tt := previewTester(p, 1120, 2000)
	click(t, tt, "新任务")
	click(t, tt, "运行 benchmark")
	if !p.state.busy || p.state.task.stage != "prepare_candidate" {
		t.Fatal("benchmark button did not begin simulated work", tt.Texts())
	}
	if p.state.service != nil {
		t.Fatal("preview created a real service")
	}
}

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
	if p.state.busy || !p.state.preflightReady() || !tt.HasText("运行 benchmark") {
		t.Fatal("environment preparation did not unlock benchmark", tt.Texts())
	}
	return p, tt
}

func TestPreviewCompleteWorkflowWithoutSceneSelector(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "模型")
	click(t, tt, "Preview-Model-Q4.gguf")
	click(t, tt, "Preview-Small.gguf")
	click(t, tt, "用这个组合新开任务")
	click(t, tt, "运行 benchmark")
	p.advance(400 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("启动与绑定") || p.state.engineState.State != "starting" {
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
	click(t, tt, "新任务")
	if len(p.state.reports) != 1 || len(p.state.historyRuns()) != 2 {
		t.Fatal("new task erased previous results")
	}
	index, err := p.report(p.state.reports[0].ReportId)
	if err != nil || len(index.Runs) != 2 {
		t.Fatal("generated report does not contain both runs", err)
	}
	for _, run := range index.Runs {
		if run.Model != "Preview-Small.gguf" || len(run.Requests) != 120 || run.Source.RunID == "preview-run" {
			t.Fatal("generated report did not use the actual simulated run", run.Model, run.Source.RunID)
		}
		if *run.Dashboard.Latency.SampleCount != 120 || *run.Dashboard.Latency.Excluded != 0 || *run.Dashboard.SampledPeakRSSBytes != float64((2<<30)+120*(1<<20)) {
			t.Fatal("report metrics do not describe the simulated run")
		}
	}
	before, _ := json.Marshal(index)
	p.state.model = "different-model"
	after, _ := json.Marshal(index)
	if string(before) != string(after) || p.state.service != nil || p.state.taskCancel != nil {
		t.Fatal("report changed with later selection or simulation reached real runtime")
	}
}

func TestPreviewCompletedTaskOpensItsGeneratedReport(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "运行 benchmark")
	p.advance(20 * time.Second)
	tt.Frame()
	click(t, tt, "查看报告")
	if p.state.openedReport.reference != p.state.task.report || !tt.HasText("Preview-Model-Q4.gguf") || !tt.HasText("preview-case-120") {
		t.Fatal("task did not open its own generated report", tt.Texts())
	}
}

func TestPreviewFailureRetriesThroughNormalButton(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "下次操作模拟失败")
	click(t, tt, "运行 benchmark")
	p.advance(2 * time.Second)
	tt.Frame()
	if p.state.engineState.State != "failed" || p.state.task.failure == nil || p.failNext {
		t.Fatal("one-shot failure was not attached to the accepted operation")
	}
	click(t, tt, "启动服务")
	p.advance(20 * time.Second)
	tt.Frame()
	if p.state.task.stage != "done" || p.state.task.failure != nil || !tt.HasText("查看报告") {
		t.Fatal("retry did not recover through the normal workflow", tt.Texts())
	}
}

func TestPreviewCancellationPreservesOtherWork(t *testing.T) {
	for _, elapsed := range []time.Duration{600 * time.Millisecond, 2 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			p, tt := preparePreview(t)
			click(t, tt, "设置")
			p.state.modelName = "kept-model"
			tt.Frame()
			click(t, tt, "保存下载目录名")
			click(t, tt, "下载")
			click(t, tt, "下载模型")
			click(t, tt, "新任务")
			click(t, tt, "运行 benchmark")
			p.advance(elapsed)
			tt.Frame()
			if elapsed < time.Second {
				click(t, tt, "新任务")
			} else {
				click(t, tt, "查看实时详情")
				click(t, tt, "取消任务")
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

func TestPreviewDownloadsCancelRetryAndPopulateModels(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "下载")
	click(t, tt, "下载模型")
	click(t, tt, "下载模型")
	first, second := p.state.downloads[0].TaskId, p.state.downloads[1].TaskId
	click(t, tt, "取消下载")
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
	click(t, tt, p.state.model)
	name := retry.OutName + " · " + emptyText(retry.FileName)
	click(t, tt, name)
	if p.state.model != name {
		t.Fatal("completed download could not be selected")
	}
}

func TestPreviewSettingsDeviceAndReportRecovery(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "设置")
	p.state.modelName = "saved-model"
	tt.Frame()
	click(t, tt, "保存下载目录名")
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
	click(t, tt, "运行 benchmark")
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
	click(t, tt, "运行 benchmark")
	p.advance(20 * time.Second)
	tt.Frame()
	click(t, tt, "停止")
	click(t, tt, "新任务")
	p.advance(time.Second)
	tt.Frame()
	if p.state.engineState.State != "stopped" || p.state.busy || p.state.newTaskPending {
		t.Fatal("new task left service stopping or task pending", p.state.engineState.State)
	}
}

func TestPreviewNewTaskCancelsPendingHistoricalReport(t *testing.T) {
	p, tt := preparePreview(t)
	click(t, tt, "运行 benchmark")
	p.advance(20 * time.Second)
	tt.Frame()
	p.dispatch(previewCreateReport{paths: p.state.task.runs})
	click(t, tt, "新任务")
	p.advance(400 * time.Millisecond)
	tt.Frame()
	click(t, tt, "运行 benchmark")
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
