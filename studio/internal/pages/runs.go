package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"strconv"
	"strings"
)

func (s *viewState) runsView(c *ui.Context) {
	s.header(c, "运行")
	errorView(c, s.historyError)
	card(c, "启动测评", func() {
		field(c, "zh-core 已绑定配置路径", &s.firstConfig)
		if ui.Checkbox(c, &s.single, "仅运行一个题包").Changed() {
			s.confirmed = false
		}
		if s.single {
			ui.Checkbox(c, &s.confirmed, "我确认只运行一个题包")
		} else {
			field(c, "zh-svg-pelican 已绑定配置路径", &s.secondConfig)
		}
		ui.Text(c, "默认双题包，按配置顺序串行运行。运行前请核对设备、预算与停止条件。")
		if ui.PrimaryButton(c, "启动运行").Disabled(s.busy || strings.TrimSpace(s.firstConfig) == "" || (!s.single && strings.TrimSpace(s.secondConfig) == "") || (s.single && !s.confirmed)).Clicked() {
			req := core.StartRunsRequest{ConfigPaths: []string{strings.TrimSpace(s.firstConfig)}}
			if s.single {
				confirmed := true
				req.AllowSingleBundle = &confirmed
			} else {
				req.ConfigPaths = append(req.ConfigPaths, strings.TrimSpace(s.secondConfig))
			}
			s.work(func() (string, *core.ApiError) {
				tasks, e := s.service.StartRuns(req)
				if e == nil {
					ids := []string{}
					for _, t := range tasks {
						ids = append(ids, t.TaskId)
					}
					s.win.Update(func() { s.groups = append(s.groups, ids) })
				}
				return "运行已加入队列。", e
			})
		}
	})
	if s.resumeSource != nil {
		s.resumeView(c)
	}
	card(c, "任务队列", func() {
		if len(s.tasks) == 0 {
			ui.Text(c, "暂无任务。")
		}
		grouped := map[string]bool{}
		for i, group := range s.groups {
			ui.Text(c, fmt.Sprintf("启动组 %d · %d 个任务", i+1, len(group))).Bold()
			for _, id := range group {
				grouped[id] = true
				found := false
				for _, task := range s.tasks {
					if task.TaskId == id {
						s.taskRow(c, task)
						found = true
						break
					}
				}
				if !found {
					ui.Text(c, id+" · 任务已不在内存表中")
				}
			}
		}
		for _, task := range s.tasks {
			if !grouped[task.TaskId] {
				s.taskRow(c, task)
			}
		}
	})
	card(c, "运行历史", func() {
		ui.Text(c, "结果目录："+text(s.history.ResultsRoot))
		ui.Text(c, "扫描时间："+s.history.ScannedAt)
		if len(s.history.Batches) == 0 && len(s.history.StandaloneRuns) == 0 {
			ui.Text(c, "暂无运行记录。")
		}
		for _, batch := range s.history.Batches {
			ui.Text(c, "批次 "+batch.BatchId+" · "+aggregateStatus(batch.Runs)).Bold()
			ui.Text(c, batch.Path).Selectable()
			for _, run := range batch.Runs {
				s.historyRow(c, run)
			}
		}
		if len(s.history.StandaloneRuns) > 0 {
			ui.Text(c, "独立运行").Bold()
			for _, run := range s.history.StandaloneRuns {
				s.historyRow(c, run)
			}
		}
	})
}
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
func (s *viewState) taskRow(c *ui.Context, t core.RunTask) {
	ui.Row(c).Key(t.TaskId).Gap(12).Children(func() {
		ui.Text(c, text(t.Bundle)+" · "+taskLabel(t)).Grow(1)
		if ui.Button(c, "查看任务 "+t.TaskId).Clicked() {
			s.selectTask(t.TaskId)
		}
	})
}
func (s *viewState) historyRow(c *ui.Context, r core.RunSummary) {
	ui.Column(c).Key(r.Path).Gap(6).Padding(10).Children(func() {
		ui.Text(c, r.RunId+" · "+stateLabel(r.Status)).Bold()
		ui.Text(c, text(r.ModelName)+" · "+text(r.Bundle)+" · "+text(r.Engine))
		ui.Text(c, r.Path).Selectable()
		ui.Text(c, fmt.Sprintf("封存：%t · 完整度：%s · 停止原因：%s", r.Sealed, text(r.Completeness), text(r.StopReason)))
		score := "证据不足"
		if r.Score != nil {
			rate := "无有效题"
			if r.Score.Rate != nil {
				rate = fmt.Sprintf("%.1f%%", *r.Score.Rate*100)
			}
			score = fmt.Sprintf("%d/%d · %s", r.Score.Passed, r.Score.Valid, rate)
		}
		ui.Text(c, "得分："+score)
		ui.Text(c, "关系："+text(r.Relation)+" · 来源："+text(r.ParentRunId)+" · 开始："+text(r.StartedAt)+" · 结束："+text(r.FinishedAt))
		if ui.Button(c, "续测 "+r.RunId).Disabled(s.busy || r.BatchId == nil || !r.Resumable || s.denied[r.Path]).Clicked() {
			source := r
			s.resumeSource = &source
			s.endpoint = ""
			s.pid = ""
		}
		if r.BatchId == nil {
			ui.Text(c, "独立运行不支持续测。")
		} else if !r.Resumable || s.denied[r.Path] {
			ui.Text(c, "当前来源不可续测（not_resumable）。")
		}
		for _, task := range s.tasks {
			if emptyText(task.RunPath) == r.Path && ui.Button(c, "实时详情 "+task.TaskId).Clicked() {
				s.selectTask(task.TaskId)
			}
		}
	})
}
func (s *viewState) resumeView(c *ui.Context) {
	card(c, "续测 "+s.resumeSource.RunId, func() {
		ui.Text(c, s.resumeSource.Path).Selectable()
		field(c, "endpoint_url（可选）", &s.endpoint)
		field(c, "server_pid（与 endpoint 同给同省）", &s.pid)
		ui.Text(c, "不填写时沿用来源配置；来源是否可续测由 CLI 最终裁决。")
		if ui.PrimaryButton(c, "确认续测").Disabled(s.busy || s.denied[s.resumeSource.Path]).Clicked() {
			s.resume()
		}
		if ui.Button(c, "关闭续测设置").Clicked() {
			s.resumeSource = nil
		}
	})
}
func (s *viewState) resume() {
	req := core.ResumeRunRequest{FromRun: s.resumeSource.Path}
	endpoint, pid := strings.TrimSpace(s.endpoint), strings.TrimSpace(s.pid)
	if (endpoint == "") != (pid == "") {
		s.failure = &core.ApiError{Code: "invalid_request", Message: "endpoint 与 PID 必须同给同省。"}
		return
	}
	if pid != "" {
		value, e := strconv.ParseInt(pid, 10, 64)
		if e != nil || value <= 0 {
			s.failure = &core.ApiError{Code: "invalid_request", Message: "PID 必须为正整数。"}
			return
		}
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
