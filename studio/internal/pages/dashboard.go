package pages

import (
	"github.com/egoist/mygo/ui"
)

func (w *viewState) step() int {
	if w.task.stage != "" {
		switch w.task.stage {
		case "prepare_candidate":
			if w.progress != nil && w.progress.DevicePreflight.Status == "missing" {
				return 1
			}
			return 2
		case "bind":
			return 3
		case "run":
			return 4
		case "report":
			return 5
		case "done":
			return 6
		}
	}
	if w.running() != nil {
		return 4
	}
	if w.binding || w.autoBind.armed || w.bindingPhase() {
		return 3
	}
	if w.progress == nil {
		return 0
	}
	if w.progress.DefaultScopeComplete {
		return 6
	}
	switch w.progress.NextAction {
	case "device_check":
		return 1
	case "prepare_candidate":
		return 2
	case "bind":
		return 3
	case "run", "resume":
		return 4
	case "done":
		return 6
	default:
		return 5
	}
}

func (s *viewState) dashboardView(c *ui.Context) {
	if s.speed != nil && s.speed.current != nil {
		s.speedTaskView(c)
		return
	}
	if s.resumeSource != nil {
		s.resumeView(c)
		return
	}
	if s.newTaskPending {
		ui.Text(c, "正在取消并等待安全收尾…")
		return
	}
	ui.Text(c, "配置、运行与跟踪本地模型测评。")
	if s.environmentMissing() {
		s.setupGuide(c)
		return
	}
	if s.speed == nil || !s.speed.selected {
		s.conclusionView(c)
	}
	s.stageView(c)
	for _, task := range s.tasks {
		if task.Kind != "context_speed" && emptyText(task.RunPath) == "" {
			ui.Text(c, taskLabel(task))
			errorView(c, task.Error)
		}
	}

}

func (s *viewState) environmentMissing() bool {
	return s.ready.CLI.Status != "ok" || !s.ready.Workspace.Initialized || !s.ready.Results.Exists || !s.ready.Models.Exists
}

func (s *viewState) conclusionView(c *ui.Context) {
	if s.progress == nil {
		return
	}
	card(c, "工作区进度", func() {
		step := s.step()
		ui.Row(c).Wrap().Gap(c.Theme().Space(2)).Label("六阶段进度").Children(func() {
			for i, label := range []string{"准备", "设备检查", "benchmark", "启动与绑定", "运行", "报告"} {
				switch {
				case i < step:
					ui.Text(c, "✓  "+label).TextColor(c.Theme().Success)
				case i == step:
					statusBadge(c, label, c.Theme().Accent).Description("当前阶段：" + label)
				default:
					ui.Text(c, label).TextColor(c.Theme().TextMuted)
				}
			}
		})
		progressBar(c, float64(step)/6, "工作区六阶段准备进度").Description("六个准备阶段，不表示样本完成率。")
		for _, b := range s.taskBundles() {
			ui.Text(c, b.Bundle+" · "+stateLabel(b.Phase))
			if b.BlockedReason != nil {
				// An in-flight single-mode run legitimately reports
				// "resume not supported" while it is still executing; the
				// reason is flow guidance, not a failure, so it is not
				// styled (or announced) as an error.
				color := c.Theme().Danger
				if b.Phase == "run_incomplete" {
					color = c.Theme().Warning
				}
				ui.Text(c, human(*b.BlockedReason)).TextColor(color)
			}
		}
	})
}
func (s *viewState) setupGuide(c *ui.Context) {
	card(c, "环境准备", func() {
		ui.Text(c, "检查并安装 CLI。")
		ui.Text(c, "建立结果目录、模型目录和工作区。")
		ui.Text(c, "已完成的步骤会跳过。")
		if primaryButton(c, "准备环境").Disabled(s.busy).Clicked() {
			s.prepareEnvironment()
		}
		if s.cliInstallStatus != "" {
			ui.Text(c, s.cliInstallStatus)
		}
	})
}
func (s *viewState) stageView(c *ui.Context) {
	action := ""
	if s.progress != nil {
		action = s.progress.NextAction
		if s.progress.DefaultScopeComplete {
			action = "done"
		}
	}
	if s.running() != nil {
		action = "run"
	}
	if s.binding || s.autoBind.armed || s.bindingPhase() {
		action = "bind"
	}
	if s.task.stage != "" {
		action = s.task.stage
		if action == "prepare_candidate" && s.progress != nil && s.progress.DevicePreflight.Status == "missing" {
			action = "device_check"
		}
	}
	switch action {
	case "device_check":
		stepTrack(c, "2", "设备检查", func() {
			ui.Text(c, "正在准备设备检查；检查结果可在设备页查看。")
			s.checkWorkspaceDevice()
			button(c, "查看设备页明细").OnClick(func() { s.selected = "device" })
		})
	case "prepare_candidate":
		stepTrack(c, "3", "benchmark", func() { s.candidateView(c) })
	case "bind":
		stepTrack(c, "4", "启动与绑定", func() { s.serviceView(c) })
	case "run", "resume":
		if s.running() == nil && (s.task.stage == "" || s.task.failure != nil) {
			if b := s.active(); b != nil && !blockedPlan(*b) && (b.CandidatePath != nil || b.BoundPath != nil) {
				card(c, "模型服务", func() { s.serviceView(c) })
			}
		}
		stepTrack(c, "5", "运行", func() { s.runView(c) })
	case "report":
		stepTrack(c, "6", "报告", func() {
			if s.task.stage != "" {
				ui.Text(c, "正在生成任务报告…")
			} else {
				ui.Text(c, "在报告页生成报告。")
				button(c, "查看报告").OnClick(func() { s.selected = "reports" })
			}
		})
	case "verify":
		stepTrack(c, "6", "报告", func() { ui.Text(c, "请在报告页核验报告。") })
	case "done":
		stepTrack(c, "6", "报告", func() {
			path := s.task.report
			if path == "" && s.progress != nil {
				path = emptyText(s.progress.Report.Path)
			}
			if path != "" {
				button(c, "查看报告").AlignSelf(ui.Start).OnClick(func() { s.openReport(path) })
			} else {
				ui.Text(c, "运行已完成，可在报告页查看。")
				button(c, "查看报告列表").AlignSelf(ui.Start).OnClick(func() { s.selected = "reports" })
			}
		})
	default:
		if s.progress == nil {
			ui.Text(c, "无法读取工作区进度，请刷新重试")
		} else {
			ui.Text(c, "未识别的进度阶段："+action)
		}
	}
}
