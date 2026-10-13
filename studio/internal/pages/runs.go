package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *viewState) runsView(c *ui.Context) {
	s.header(c, "运行")
	s.runGuide(c)
	errorView(c, s.historyError)
	card(c, "启动测评", func() {
		options := s.boundConfigs()
		if len(options) == 0 {
			ui.Text(c, "尚无已绑定的题包。请先按上方引导完成候选与绑定。")
		}
		names := []string{}
		for _, o := range options {
			names = append(names, o.bundle)
		}
		if len(names) > 0 {
			s.firstBundle = defaultBundle(s.firstBundle, names, "")
			twoBound := len(names) >= 2
			if s.single || !twoBound {
				ui.Field(c, "题包", func() { ui.Select(c, &s.firstBundle, names) })
				ui.Text(c, configOf(options, s.firstBundle)).Selectable()
			} else {
				s.secondBundle = defaultBundle(s.secondBundle, names, s.firstBundle)
				ui.Field(c, "第一题包", func() { ui.Select(c, &s.firstBundle, names) })
				ui.Text(c, configOf(options, s.firstBundle)).Selectable()
				ui.Field(c, "第二题包", func() { ui.Select(c, &s.secondBundle, names) })
				ui.Text(c, configOf(options, s.secondBundle)).Selectable()
			}
		}
		ui.Checkbox(c, &s.manualConfig, "高级：手动配置路径")
		if s.manualConfig {
			field(c, "第一配置绝对路径", &s.manualFirst)
			if !s.single {
				field(c, "第二配置绝对路径", &s.manualSecond)
			}
		}
		first, second := configOf(options, s.firstBundle), configOf(options, s.secondBundle)
		if s.manualConfig {
			first, second = strings.TrimSpace(s.manualFirst), strings.TrimSpace(s.manualSecond)
		}
		twoBound := first != "" && second != ""
		if ui.Checkbox(c, &s.single, "仅运行一个题包").Changed() {
			s.confirmed = false
		}
		if s.single {
			ui.Checkbox(c, &s.confirmed, "我确认只运行一个题包")
		}
		ui.Text(c, "默认双题包，按配置顺序串行运行。运行前请核对设备、预算与停止条件。")
		if !twoBound && !s.single {
			ui.Text(c, "第二题包尚未绑定，双题包运行不可用。")
		}
		duplicate := first == second
		if !s.single && twoBound && duplicate {
			ui.Text(c, "两个题包不能相同。").TextColor(c.Theme().Danger)
		}
		if ui.PrimaryButton(c, "启动运行").AlignSelf(ui.Start).Disabled(s.busy || first == "" || (!s.single && (!twoBound || duplicate)) || (s.single && !s.confirmed)).Clicked() {
			req := core.StartRunsRequest{ConfigPaths: []string{first}}
			if s.single {
				confirmed := true
				req.AllowSingleBundle = &confirmed
			} else {
				req.ConfigPaths = append(req.ConfigPaths, second)
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
	c.Key("tasks-card")
	card(c, "任务队列", func() { s.tasksView(c) })
	c.Key("history-card")
	card(c, "运行历史", func() { s.historyView(c) })
}

// boundOption maps a bound bundle to the frozen config written by `config bind`.
type boundOption struct{ bundle, config string }

func (s *viewState) boundConfigs() []boundOption {
	options := []boundOption{}
	if s.progress == nil {
		return options
	}
	for _, b := range s.progress.Bundles {
		if b.BoundPath != nil {
			options = append(options, boundOption{bundle: b.Bundle, config: filepath.Join(*b.BoundPath, "config.toml")})
		}
	}
	return options
}

// defaultBundle keeps the current selection when still valid, otherwise picks
// the first option that is not exclude.
func defaultBundle(current string, names []string, exclude string) string {
	for _, n := range names {
		if n == current {
			return current
		}
	}
	for _, n := range names {
		if n != exclude {
			return n
		}
	}
	return names[0]
}

func configOf(options []boundOption, bundle string) string {
	for _, o := range options {
		if o.bundle == bundle {
			return o.config
		}
	}
	return ""
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
func (s *viewState) historyRow(c *ui.Context, r core.RunSummary) {
	ui.Column(c).Key(r.Path).Gap(6).Padding(10).Children(func() {
		ui.Text(c, r.RunId+" · "+stateLabel(r.Status)).Bold()
		ui.Text(c, text(r.ModelName)+" · "+text(r.Bundle)+" · "+text(r.Engine))
		ui.Text(c, r.Path).Selectable()
		ui.Text(c, fmt.Sprintf("封存：%t · 完整度：%s · 停止原因：%s", r.Sealed, text(r.Completeness), text(r.StopReason)))
		ui.Text(c, "得分："+runScore(r))
		ui.Text(c, "关系："+text(r.Relation)+" · 来源："+text(r.ParentRunId)+" · 开始："+text(r.StartedAt)+" · 结束："+text(r.FinishedAt))
		ui.Row(c).Gap(10).Children(func() {
			if ui.Button(c, "续测").AlignSelf(ui.Start).Disabled(s.busy || r.BatchId == nil || !r.Resumable || s.denied[r.Path]).Clicked() {
				source := r
				s.resumeSource = &source
				s.endpoint = ""
				s.pid = ""
			}
			for _, task := range s.tasks {
				if emptyText(task.RunPath) == r.Path && ui.Button(c, "实时详情").AlignSelf(ui.Start).Clicked() {
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
		field(c, "endpoint_url（可选）", &s.endpoint)
		field(c, "server_pid（与 endpoint 同给同省）", &s.pid)
		ui.Text(c, "不填写时沿用来源配置；来源是否可续测由 CLI 最终裁决。")
		ui.Row(c).Gap(10).Children(func() {
			if ui.PrimaryButton(c, "确认续测").AlignSelf(ui.Start).Disabled(s.busy || s.denied[s.resumeSource.Path]).Clicked() {
				s.resume()
			}
			if ui.Button(c, "关闭续测设置").AlignSelf(ui.Start).Clicked() {
				s.resumeSource = nil
			}
		})

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
