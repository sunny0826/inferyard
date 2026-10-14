package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
	ui.Text(c, "结果目录："+text(s.history.ResultsRoot))
	ui.Text(c, "扫描时间："+s.history.ScannedAt)
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
