package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func aggregateStatus(runs []core.RunSummary) string {
	priority := map[string]int{"complete": 1, "partial": 2, "dirty": 3, "cancelled": 4}
	state := ""
	for _, r := range runs {
		if priority[r.Status] > priority[state] {
			state = r.Status
		}
	}
	return stateLabel(state)
}
func (s *viewState) historyRow(c *ui.Context, r core.RunSummary) {
	t := tokens(c)
	ui.Column(c.Key(r.Path)).Gap(t.Density.Inset).Padding(t.Density.CardGap).Children(func() {
		ui.Text(c, r.RunId+" · "+stateLabel(r.Status)).Bold()
		ui.Text(c, text(r.ModelName)+" · "+text(r.Bundle)+" · "+text(r.Engine))
		ui.Text(c, displayName(r.Path)).Selectable()
		sealed := "证据未收尾"
		if r.Sealed {
			sealed = termSealed
		}
		ui.Text(c, sealed+" · 完整度："+stateLabel(text(r.Completeness)))
		if r.StopReason != nil {
			ui.Text(c, "停止原因："+stopReasonLabel(*r.StopReason))
		}
		ui.Text(c, "得分："+runScore(r))
		componentFold(c, "运行原始详情", func() {
			ui.Text(c, "时钟基准：UTC · 开始："+formatTimestampOrMissingUTC(r.StartedAt)+" · 结束："+formatTimestampOrMissingUTC(r.FinishedAt)).Selectable()
			if r.StopReason != nil {
				ui.Text(c, "原始停止原因："+*r.StopReason).Selectable()
			}
			ui.Text(c, "关系："+text(r.Relation)+" · 来源："+text(r.ParentRunId)).Selectable()
		})
		ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
			button(c, "进入任务").OnClick(func() {
				s.detailID = ""
				s.resumeSource = nil
				s.focusedRun = r.Path
				s.selected = "dashboard"
			})
			if button(c, "续测").AlignSelf(ui.Start).Disabled(s.busy || r.BatchId == nil || !r.Resumable || s.denied[r.Path]).Clicked() {
				source := r
				s.resumeSource = &source
				s.resumeDiscardConfirm = false
				s.endpoint = ""
				s.pid = ""
			}
			for _, task := range s.tasks {
				if emptyText(task.RunPath) == r.Path && button(c, "实时详情").AlignSelf(ui.Start).Clicked() {
					s.selectTask(task.TaskId)
					s.selected = "dashboard"
				}
			}
		})

		if r.BatchId == nil {
			ui.Text(c, "独立运行不支持续测。")
		} else if !r.Resumable || s.denied[r.Path] {
			ui.Text(c, "当前来源不可续测；请回到任务页检查服务连接，或用「新任务」重新测评。")
		}

	})
}
func (s *viewState) resumeDirty() bool {
	return strings.TrimSpace(s.endpoint) != "" || strings.TrimSpace(s.pid) != ""
}

func (s *viewState) closeResumeDialog() {
	s.resumeSource = nil
	s.resumeDiscardConfirm = false
	s.endpoint = ""
	s.pid = ""
}

func (s *viewState) resumeDialog(c *ui.Context) {
	open := s.resumeSource != nil
	dirty := open && s.resumeDirty()
	c.Key("resume-dialog")
	components.Dialog(c, &open, components.DialogOptions{
		Label:                "续测设置",
		DisableBackdropClose: dirty,
		DisableEscapeClose:   dirty,
	}, func(panel ui.Element) {
		if s.resumeSource == nil {
			return
		}
		panel.Width(c.Theme().Space(115)).MaxWidthPercent(90).Children(func() {
			ui.Column(c).Gap(tokens(c).Density.CardGap).Children(func() {
				heading(c, "续测 "+s.resumeSource.RunId, 2)
				ui.Text(c, displayName(s.resumeSource.Path)).Selectable().TextColor(c.Theme().TextMuted)
				endpointError, pidError := resumeErrors(s.endpoint, s.pid)
				field(c, "服务地址（可选）", &s.endpoint).Description("本机服务 URL，留空则沿用原配置").Error(endpointError)
				field(c, "服务 PID（可选）", &s.pid).Description("正整数，须与服务地址同时填写或留空").Error(pidError)
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.CardGap).Children(func() {
					if primaryActionButton(c, "确认续测", s.actionBusy("resume-run")).AlignSelf(ui.Start).Disabled(s.busy || s.denied[s.resumeSource.Path]).Clicked() {
						s.resume()
					}
					if button(c, "关闭续测设置").AlignSelf(ui.Start).Clicked() {
						if s.resumeDirty() {
							s.resumeDiscardConfirm = true
						} else {
							open = false
						}
					}
				})
			})
		})
	})
	if !open && s.resumeSource != nil {
		s.closeResumeDialog()
	}
	if components.AlertDialog(c, &s.resumeDiscardConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: "放弃未保存的续测设置"},
		Title:         "放弃未保存的修改？",
		Message:       "已填写的服务地址或 PID 尚未提交，关闭后将清空。",
		ConfirmLabel:  "放弃修改",
		CancelLabel:   "继续编辑",
	}) == components.DialogConfirmed {
		s.closeResumeDialog()
	}
}
func (s *viewState) resume() {
	ee, pe := resumeErrors(s.endpoint, s.pid)
	if ee != "" || pe != "" {
		message := ee
		if pe != "" {
			message = pe
		}
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	if s.simulate(previewStartRun{bundle: emptyText(s.resumeSource.Bundle), resumePath: s.resumeSource.Path}) {
		return
	}
	req := core.ResumeRunRequest{FromRun: s.resumeSource.Path}
	endpoint, pid := strings.TrimSpace(s.endpoint), strings.TrimSpace(s.pid)
	if pid != "" {
		value, _ := strconv.ParseInt(pid, 10, 64)
		req.EndpointUrl = &endpoint
		req.ServerPid = &value
	}
	s.workAs("resume-run", func() (string, *core.ApiError) {
		task, e := s.service.Resume(req)
		s.win.Update(func() {
			if e != nil {
				if e.Details["reason"] == "not_resumable" {
					s.denied[req.FromRun] = true
				}
				return
			}
			s.closeResumeDialog()
			s.selected = "dashboard"
			s.selectTask(task.TaskId)
		})
		return "续测已加入队列。", e
	})
}

func selectableTable(state *pageTableState, key func(int) any, label func(int) string) {
	if state.Selected == nil {
		index := -1
		state.Selected = &index
	}
	state.Key, state.Label = key, label
}

func selectedRow(state *pageTableState, count int) int {
	if state.Selected == nil || *state.Selected < 0 || *state.Selected >= count {
		return -1
	}
	return *state.Selected
}

func runScore(r core.RunSummary) string {
	if r.Score == nil {
		return "未记录评分"
	}
	rate := "无有效题"
	switch gap := r.Score.RateGap(); {
	case gap != "":
		rate = "含 " + gapDescription(*r.Score, gap)
	case r.Score.Rate != nil:
		rate = fmt.Sprintf("%.1f%%", *r.Score.Rate*100)
	}
	if r.Score.Failed > 0 {
		rate += fmt.Sprintf("，含 %d 题执行失败", r.Score.Failed)
	}
	return fmt.Sprintf("%d/%d · %s", r.Score.Passed, r.Score.Valid, rate)
}

func (s *viewState) historyView(c *ui.Context) {
	ui.Text(c, "结果目录："+configuredValue(displayName(emptyText(s.history.ResultsRoot))))
	ui.Text(c, "扫描时间："+configuredValue(formatTimestamp(s.history.ScannedAt)))
	runs := s.historyRuns()
	if len(runs) == 0 && s.historyError == nil && s.loaded {
		ui.Text(c, "暂无运行记录。")
		return
	}
	selectableTable(&s.historyTable, func(i int) any { return runs[i].Path }, func(i int) string { return runs[i].RunId })
	c.Key("history-table")
	pageTable(c, &s.historyTable, "运行历史表", []ui.TableColumn{{Title: "模型", Width: 190}, {Title: "测评题集", Width: 130}, {Title: "状态", Width: 70}, {Title: "评分", Width: 170}, {Title: "开始时间", Width: 170}}, len(runs), func(row, col int) {
		r := runs[row]
		ui.Text(c, []string{historyModel(r), text(r.Bundle), stateLabel(r.Status), runScore(r), historyStarted(r)}[col])
	}).Height(c.Theme().Space(8)*float32(min(len(runs), 8)+1) + 2).Label("运行历史表")
	if i := selectedRow(&s.historyTable, len(runs)); i >= 0 {
		r := runs[i]
		s.historyIdentity(c, r)
		for _, batch := range s.history.Batches {
			for _, run := range batch.Runs {
				if run.Path == r.Path {
					ui.Text(c, "批次 "+batch.BatchId+" · "+aggregateStatus(batch.Runs))
					ui.Text(c, displayName(batch.Path)).Selectable()
					break
				}
			}
		}
		s.historyRow(c, r)
	} else if len(runs) > 0 {
		ui.Text(c, "点击某行查看详情与操作。").TextColor(c.Theme().TextMuted)
	}
}

func historyModel(r core.RunSummary) string {
	if r.ModelName == nil || *r.ModelName == "" {
		return "未记录模型"
	}
	return *r.ModelName
}

func historyStarted(r core.RunSummary) string {
	raw := emptyText(r.StartedAt)
	if raw == "" {
		return "未记录"
	}
	return formatTimestamp(raw)
}

func formatTimestampOrMissingUTC(p *string) string {
	if p == nil || *p == "" {
		return "缺测"
	}
	return formatTimestampUTC(*p)
}

func (s *viewState) historyIdentity(c *ui.Context, r core.RunSummary) {
	line := "运行 ID：" + r.RunId
	if r.BatchId != nil {
		line += " · 批次：" + *r.BatchId
	} else {
		line += " · 独立运行"
	}
	ui.Text(c, line).Selectable().TextColor(c.Theme().TextMuted)
}
