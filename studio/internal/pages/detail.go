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
		host := ui.Column(c.Key("detail-loading-" + s.detailID)).FillWidth().Gap(tokens(c).Density.Section)
		show := delayedVisible(c, host, "detail-load", true)
		host.Children(func() {
			if show {
				detailLoadingSkeleton(c)
			}
		})
		return
	}
	d := s.detail
	t := d.Task
	card(c, text(t.Bundle)+" · "+t.TaskId, func() {
		statusBadge(c, taskLabel(t), runColor(c, t))
		ui.Text(c, "运行目录："+displayName(text(t.RunPath))).Selectable()
		errorView(c, t.Error)
		wholeRound := s.task.stage != ""
		cancelOption := destructiveOption{
			kind: destructiveCancelRun, id: t.TaskId, label: "取消任务", confirmLabel: "确认取消任务",
			prompt:   cancelRunPrompt(wholeRound),
			disabled: (s.busy && s.task.stage == "") || terminal(t.State) || t.State == "cancelling",
		}
		var cancelConfirmed bool
		if wholeRound {
			cancelConfirmed = s.destructiveDialog(c, cancelOption)
		} else {
			cancelConfirmed = s.destructiveAction(c, cancelOption)
		}
		if cancelConfirmed {
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
				return "已请求取消，等待安全收尾", e
			})
		}
		componentFold(c, "运行原始字段", func() { detailRawFields(c, t) })
	})
	card(c, "实时进度", func() { s.detailProgress(c, d) })
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

func (s *viewState) detailProgress(c *ui.Context, d *core.RunTaskDetail) {
	p := d.Progress
	if p == nil {
		ui.Text(c, "等待题目与进度数据…")
		return
	}
	headline := progressHeadline(p)
	if !terminal(d.Task.State) && p.Total != nil && *p.Total > 0 {
		host := ui.Row(c.Key("detail-eta-" + d.Task.TaskId)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row)
		eta := formatETA(c, host, "detail-"+d.Task.TaskId, p.Done, *p.Total)
		host.Children(func() {
			ui.Text(c, headline)
			if eta != "" {
				ui.Text(c, "· "+eta).TextColor(c.Theme().TextMuted)
			}
		})
	} else {
		ui.Text(c, headline)
	}
	progressBar(c, progressValue(p.Done, p.Total), "运行题目进度")
	ui.Text(c, fmt.Sprintf("当前题：%s", text(p.CurrentCaseId)))
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
