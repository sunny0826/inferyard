package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"strconv"
	"strings"
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
	ui.Column(c).Key(r.Path).Gap(6).Padding(10).Children(func() {
		ui.Text(c, r.RunId+" · "+stateLabel(r.Status)).Bold()
		ui.Text(c, text(r.ModelName)+" · "+text(r.Bundle)+" · "+text(r.Engine))
		ui.Text(c, r.Path).Selectable()
		sealed := "未封存"
		if r.Sealed {
			sealed = "已封存（不代表已核验）"
		}
		ui.Text(c, sealed+" · 完整度："+stateLabel(text(r.Completeness)))
		if r.StopReason != nil {
			ui.Text(c, "停止原因："+human(*r.StopReason)+" · "+*r.StopReason)
		}
		ui.Text(c, "得分："+runScore(r))
		componentFold(c, "运行原始详情", func() {
			ui.Text(c, "关系："+text(r.Relation)+" · 来源："+text(r.ParentRunId)+" · 开始："+text(r.StartedAt)+" · 结束："+text(r.FinishedAt)).Selectable()
		})
		ui.Row(c).Wrap().Gap(c.Theme().Space(2.5)).Children(func() {
			if button(c, "续测").AlignSelf(ui.Start).Disabled(s.busy || r.BatchId == nil || !r.Resumable || s.denied[r.Path]).Clicked() {
				source := r
				s.resumeSource = &source
				s.endpoint = ""
				s.pid = ""
			}
			for _, task := range s.tasks {
				if emptyText(task.RunPath) == r.Path && button(c, "实时详情").AlignSelf(ui.Start).Clicked() {
					s.selectTask(task.TaskId)
				}
			}
		})

		if r.BatchId == nil {
			ui.Text(c, "独立运行不支持续测。")
		} else if !r.Resumable || s.denied[r.Path] {
			ui.Text(c, "当前来源不可续测（not_resumable）。")
		}

	})
}
func (s *viewState) resumeView(c *ui.Context) {
	card(c, "续测 "+s.resumeSource.RunId, func() {
		ui.Text(c, s.resumeSource.Path).Selectable()
		endpointError, pidError := resumeErrors(s.endpoint, s.pid)
		field(c, "endpoint_url（可选）", &s.endpoint).Description("本机服务 URL；与 PID 同时填写或同时省略。").Error(endpointError)
		field(c, "server_pid（与 endpoint 同给同省）", &s.pid).Description("正整数；不填写时沿用来源配置。").Error(pidError)
		ui.Text(c, "不填写时沿用来源配置；来源是否可续测由 CLI 最终裁决。")
		ui.Row(c).Wrap().Gap(c.Theme().Space(2.5)).Children(func() {
			if primaryButton(c, "确认续测").AlignSelf(ui.Start).Disabled(s.busy || s.denied[s.resumeSource.Path]).Clicked() {
				s.resume()
			}
			if button(c, "关闭续测设置").AlignSelf(ui.Start).Clicked() {
				s.resumeSource = nil
			}
		})

	})
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
	req := core.ResumeRunRequest{FromRun: s.resumeSource.Path}
	endpoint, pid := strings.TrimSpace(s.endpoint), strings.TrimSpace(s.pid)
	if pid != "" {
		value, _ := strconv.ParseInt(pid, 10, 64)
		req.EndpointUrl = &endpoint
		req.ServerPid = &value
	}
	s.work(func() (string, *core.ApiError) {
		task, e := s.service.Resume(req)
		s.win.Update(func() {
			if e != nil {
				if e.Details["reason"] == "not_resumable" {
					s.denied[req.FromRun] = true
				}
				return
			}
			s.groups = append(s.groups, []string{task.TaskId})
			s.resumeSource = nil
			s.selectTask(task.TaskId)
		})
		return "续测已加入队列。", e
	})
}

func selectableTable(state *ui.ListState, key func(int) any, label func(int) string) {
	if state.Selected == nil {
		index := -1
		state.Selected = &index
	}
	state.Key, state.Label = key, label
}

func selectedRow(state *ui.ListState, count int) int {
	if state.Selected == nil || *state.Selected < 0 || *state.Selected >= count {
		return -1
	}
	return *state.Selected
}

func (s *viewState) tasksView(c *ui.Context) {
	if len(s.tasks) == 0 {
		ui.Text(c, "暂无任务。")
	}
	selectableTable(&s.tasksTable, func(i int) any { return s.tasks[i].TaskId }, func(i int) string { return text(s.tasks[i].Bundle) })
	c.Key("tasks-table")
	table := ui.Table(c, &s.tasksTable, []ui.TableColumn{{Title: "题包", Width: 200}, {Title: "状态", Width: 300}, {Title: "类型", Width: 100}}, len(s.tasks), func(row, col int) {
		task := s.tasks[row]
		switch col {
		case 0:
			ui.Text(c, text(task.Bundle))
		case 1:
			statusBadge(c, taskLabel(task), runColor(c, task))
		case 2:
			ui.Text(c, task.Kind)
		}
	}).Height(200).Label("运行任务队列")
	if s.returnToTasks {
		s.tasksTable.Focus(c)
		s.returnToTasks = false
	}
	submitted := table.Submitted()
	index := selectedRow(&s.tasksTable, len(s.tasks))
	if index >= 0 {
		task := s.tasks[index]
		ui.Text(c, task.TaskId).Selectable()
		errorView(c, task.Error)
		for i, group := range s.groups {
			for _, id := range group {
				if id == task.TaskId {
					ui.Text(c, fmt.Sprintf("启动组 %d · %d 个任务", i+1, len(group)))
				}
			}
		}
	}
	if index < 0 {
		ui.Text(c, "选择一个任务后查看详情。")
	}
	button(c, "查看详情").AlignSelf(ui.Start).Disabled(index < 0).OnClick(func() { s.selectTask(s.tasks[index].TaskId) })
	if submitted && index >= 0 {
		s.selectTask(s.tasks[index].TaskId)
	}
	for _, group := range s.groups {
		for _, id := range group {
			found := false
			for _, task := range s.tasks {
				if task.TaskId == id {
					found = true
					break
				}
			}
			if !found {
				ui.Text(c, id+" · 任务已不在内存表中")
			}
		}
	}
}

func runScore(r core.RunSummary) string {
	if r.Score == nil {
		return "证据不足"
	}
	rate := "无有效题"
	if r.Score.Rate != nil {
		rate = fmt.Sprintf("%.1f%%", *r.Score.Rate*100)
	}
	return fmt.Sprintf("%d/%d · %s", r.Score.Passed, r.Score.Valid, rate)
}

func (s *viewState) historyView(c *ui.Context) {
	ui.Text(c, "结果目录："+configuredValue(emptyText(s.history.ResultsRoot)))
	ui.Text(c, "扫描时间："+configuredValue(s.history.ScannedAt))
	runs := s.historyRuns()
	if len(runs) == 0 && s.historyError == nil && s.loaded {
		ui.Text(c, "暂无运行记录。")
	}
	selectableTable(&s.historyTable, func(i int) any { return runs[i].Path }, func(i int) string { return runs[i].RunId })
	c.Key("history-table")
	ui.Table(c, &s.historyTable, []ui.TableColumn{{Title: "RunId", Width: 230}, {Title: "模型", Width: 180}, {Title: "题包", Width: 160}, {Title: "状态", Width: 100}, {Title: "得分", Width: 160}, {Title: "开始时间", Width: 220}, {Title: "批次", Width: 200}}, len(runs), func(row, col int) {
		r := runs[row]
		batch := "独立运行"
		if r.BatchId != nil {
			batch = *r.BatchId
		}
		ui.Text(c, []string{r.RunId, text(r.ModelName), text(r.Bundle), stateLabel(r.Status), runScore(r), text(r.StartedAt), batch}[col])
	}).Height(240).Label("运行历史表")
	if i := selectedRow(&s.historyTable, len(runs)); i >= 0 {
		r := runs[i]
		for _, batch := range s.history.Batches {
			for _, run := range batch.Runs {
				if run.Path == r.Path {
					ui.Text(c, "批次 "+batch.BatchId+" · "+aggregateStatus(batch.Runs))
					ui.Text(c, batch.Path).Selectable()
					break
				}
			}
		}
		s.historyRow(c, r)
	}
}
