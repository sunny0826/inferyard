package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) detailView(c *ui.Context) {
	s.header(c, "运行详情")
	if button(c, "← 返回任务").Clicked() {
		s.detailID = ""
	}
	errorView(c, s.detailError)
	if s.detailError != nil {
		button(c, "重试读取详情").OnClick(func() { s.selectTask(s.detailID) })
		if s.detail == nil {
			return
		}
	}
	if s.detail == nil {
		ui.Text(c, "正在读取任务详情…")
		return
	}
	d := s.detail
	t := d.Task
	card(c, text(t.Bundle)+" · "+t.TaskId, func() {
		statusBadge(c, taskLabel(t), runColor(c, t))
		ui.Text(c, "运行目录："+displayName(text(t.RunPath))).Selectable()
		errorView(c, t.Error)
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveCancelRun, id: t.TaskId, label: "取消任务", confirmLabel: "确认取消任务",
			prompt:   cancelRunPrompt(s.task.stage != ""),
			disabled: (s.busy && s.task.stage == "") || terminal(t.State) || t.State == "cancelling",
		}) {
			if s.task.stage != "" {
				s.newBenchmark()
				return
			}
			id := t.TaskId
			if s.simulate(previewCancelRun{id: id}) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.cancelRunTask(id)
				return "已请求取消，等待安全收尾。", e
			})
		}
		componentFold(c, "运行原始字段", func() { detailRawFields(c, t) })
	})
	card(c, "实时进度", func() { s.detailProgress(c, d, t) })
	s.resourceView(c, d.Resources)
}

func detailRawFields(c *ui.Context, t core.RunTask) {
	ui.Text(c, "原始状态："+t.State).Selectable()
	ui.Text(c, "任务类型："+t.Kind).Selectable()
	ui.Text(c, "运行 ID："+text(t.RunId)).Selectable()
	if t.Exit != nil {
		kvview(c, "kv-exit", t.Exit)
	}
}

func (s *viewState) detailProgress(c *ui.Context, d *core.RunTaskDetail, t core.RunTask) {
	p := d.Progress
	if p == nil {
		ui.Text(c, "等待本次固定的题目与进度数据…")
		return
	}
	ui.Text(c, progressHeadline(p))
	progressBar(c, progressValue(p.Done, p.Total), "运行题目进度")
	ui.Text(c, fmt.Sprintf("%s当前题：%s", detailScope(t), text(p.CurrentCaseId)))
	columns := []ui.TableColumn{{Title: "题目", Width: 180}, {Title: "状态", Width: 160}, {Title: "类别", Width: 100}, {Title: "质量", Width: 100}, {Title: "耗时", Width: 100}, {Title: "错误类别", Width: 160}}
	s.casesTable.Key = func(i int) any { return p.Cases[i].CaseId }
	pageTable(c, &s.casesTable, "题目进度表", columns, len(p.Cases), func(row, col int) {
		item := p.Cases[row]
		values := []string{item.CaseId, stateLabel(item.State), optionalText(item.Category), caseQuality(item), formatDurationMs(item.DurationMs), caseErrorCategory(item)}
		ui.Text(c, values[col]).Selectable()
	}).Height(240).Label("题目进度表")
	componentFold(c, "进度原始字段", func() {
		ui.Text(c, fmt.Sprintf("阶段：%s · 序号：%d · 当前题：%s", optionalText(p.Phase), p.LastSeq, text(p.CurrentCaseId))).Selectable()
	})
}

func progressHeadline(p *core.RunProgress) string {
	label := fmt.Sprintf("%d / %s 题", p.Done, progressTotal(p.Total))
	if phase := emptyText(p.Phase); phase != "" {
		return stateLabel(phase) + " · " + label
	}
	return label
}

func detailScope(t core.RunTask) string { return bundleScopeText(t.Kind == "resume") }

func caseQuality(item core.CaseProgress) string {
	if item.QualityState == nil || *item.QualityState == "" {
		return "缺测"
	}
	return reportQuality(item.QualityState)
}

func caseErrorCategory(item core.CaseProgress) string {
	if item.ErrorCategory != nil && *item.ErrorCategory != "" {
		return *item.ErrorCategory
	}
	if item.State == "failed" {
		return "缺测"
	}
	return "—"
}

func optionalText(value *string) string {
	if value == nil || *value == "" {
		return "缺测"
	}
	return *value
}
