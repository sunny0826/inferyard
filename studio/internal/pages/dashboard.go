package pages

import (
	"github.com/egoist/mygo/ui"
)

func (w *viewState) step() int {
	if w.task.stage != "" {
		switch w.task.stage {
		case "prepare_candidate":
			if w.localEngine() == nil && w.progress != nil && w.progress.DevicePreflight.Status == "missing" {
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
	if w.binding || w.bindingPhase() {
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
	if s.newTaskPending {
		s.header(c, "任务")
		host := ui.Column(c.Key("new-task-pending")).FillWidth()
		show := delayedVisible(c, host, "new-task-pending", true)
		host.Children(func() {
			if show {
				card(c, "正在取消当前任务", func() {
					ui.Text(c, "正在等待安全收尾，完成后将进入新任务准备…").TextColor(c.Theme().TextMuted)
					progressBar(c, -1, "等待任务收尾")
				})
			}
		})
		return
	}
	s.header(c, "任务")
	if s.environmentMissing() {
		s.setupGuide(c)
		return
	}
	if s.needsTaskAssets() {
		s.taskAssetsView(c)
		return
	}
	if s.speed == nil || !s.speed.selected {
		s.conclusionView(c)
	}
	s.stageView(c)
	s.awaitingLocalServiceView(c)
	if len(s.task.start.localCandidates) > 0 && s.task.failure != nil && !s.busy {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Message: "已完成题集将保留，确认问题解决后仅重试未完成项。",
			Actions: func() {
				button(c, "重试未完成的测评题集").OnClick(s.retryBenchmark)
			},
		})
	}
	for _, task := range s.tasks {
		if task.Kind != "context_speed" && emptyText(task.RunPath) == "" {
			if task.Queued && task.Bundle != nil && task.Error == nil {
				continue
			}
			if s.stepOwnsTaskError(task) {
				continue
			}
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
		phaseProgress(c, phaseProgressOptions{
			Labels:      []string{"准备", "设备检查", termBenchmarkStage, "连接并核验服务", "运行", "报告"},
			Current:     step,
			RowLabel:    "六阶段进度",
			BarLabel:    "工作区六阶段准备进度",
			Description: "六个准备阶段，不表示样本完成率。",
		})
		failed := s.failedBundles()
		for _, b := range s.taskBundles() {
			if task := s.liveBundleTask(b.Bundle); task != nil {
				kind := stateActive
				if task.Queued && task.State == "starting" {
					kind = stateNeutral
				}
				badge(c, b.Bundle+" · "+taskLabel(*task), kind, badgeOptions{Style: badgeDot})
				continue
			}
			if failed[b.Bundle] {
				badge(c, b.Bundle+" · 失败", stateDanger, badgeOptions{Style: badgeDot})
				continue
			}
			kind := stateNeutral
			switch b.Phase {
			case "run_complete":
				kind = stateSuccess
			case "run_incomplete":
				kind = stateWarning
			}
			badge(c, b.Bundle+" · "+stateLabel(b.Phase), kind, badgeOptions{Style: badgeDot})
			if b.BlockedReason != nil {
				if *b.BlockedReason == "no_local_model" && s.availableTaskAssets().model && s.preparingTask() {
					continue
				}
				bannerKind := stateDanger
				if b.Phase == "run_incomplete" {
					bannerKind = stateWarning
				}
				noticeBanner(c, noticeBannerOptions{
					Kind:    bannerKind,
					Size:    bannerCompact,
					Message: human(*b.BlockedReason),
				})
			}
		}
		if s.task.failure != nil || s.bindFailure != nil {
			cause, _ := s.stepDiagnosis()
			code := ""
			if cause != nil {
				code = cause.Code
			}
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateDanger,
				Size:    bannerCompact,
				Message: "当前任务已中止：" + human(code) + "。原因与恢复操作见下方当前阶段。",
			})
		}
	})
}

func (s *viewState) serviceCardVisible() bool {
	if s.running() != nil || (s.task.stage != "" && s.task.failure == nil) {
		return false
	}
	b := s.bindTarget()
	return b != nil && !blockedPlan(*b) && (b.CandidatePath != nil || b.BoundPath != nil)
}

func (s *viewState) failedBundles() map[string]bool {
	failed := map[string]bool{}
	if !s.stepFailed() {
		return failed
	}
	var names []string
	switch {
	case s.failedBundle != "":
		names = []string{s.failedBundle}
	case s.active() != nil:
		names = []string{s.active().Bundle}
	default:
		names = s.task.bundles
	}
	for _, name := range names {
		failed[name] = true
	}
	for _, b := range s.taskBundles() {
		if failed[b.Bundle] && b.Phase == "run_complete" {
			delete(failed, b.Bundle)
		}
	}
	return failed
}

func (s *viewState) configStage(c *ui.Context, body func()) {
	if s.speed != nil && s.speed.selected {
		card(c, termBenchmarkStage, body)
		return
	}
	stepTrack(c, "3", termBenchmarkStage, body)
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
	if s.binding || s.bindingPhase() {
		action = "bind"
	}
	if s.task.stage != "" {
		action = s.task.stage
		if action == "prepare_candidate" && s.localEngine() == nil && s.progress != nil && s.progress.DevicePreflight.Status == "missing" {
			action = "device_check"
		}
	}
	if action == "device_check" && s.hasLocalEngine() {
		action = "prepare_candidate"
	}
	switch action {
	case "device_check":
		stepTrack(c, "2", "设备检查", func() {
			ui.Text(c, "正在检查设备，结果可在设备页查看。")
			s.checkWorkspaceDevice()
			button(c, "查看设备页明细").OnClick(func() { s.selected = "device" })
		})
	case "prepare_candidate":
		ui.Column(c.Key("config-stage")).Gap(tokens(c).Density.CardGap).Children(func() {
			s.speedTypeSwitch(c)
			s.configStage(c, func() {
				if s.task.failure != nil {
					failureView(c, termBenchmarkStage+"失败", s.task.failure, "原因解决后可从本阶段重新开始；已完成的测评题集结果会保留。")
				}
				s.candidateFields(c)
			})
		})
	case "bind":
		stepTrack(c, "4", "连接并核验服务", func() { s.serviceView(c) })
	case "run", "resume":
		if s.serviceCardVisible() {
			card(c, "模型服务", func() { s.serviceView(c) })
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
	case "done":
		stepTrack(c, "6", "报告", func() { s.completionView(c) })
	default:
		if s.progress == nil {
			ui.Text(c, "无法读取工作区进度，请刷新重试")
		} else {
			ui.Text(c, "未识别的进度阶段："+action)
		}
	}
}
