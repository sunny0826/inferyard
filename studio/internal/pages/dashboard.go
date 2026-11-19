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
	if s.resumeSource != nil {
		s.resumeView(c)
		return
	}
	if s.newTaskPending {
		ui.Text(c, "正在取消并等待安全收尾…")
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
		ui.Text(c, "已完成题集将保留，确认问题解决后仅重试未完成项。")
		button(c, "重试未完成的测评题集").OnClick(s.retryBenchmark)
	}
	for _, task := range s.tasks {
		if task.Kind != "context_speed" && emptyText(task.RunPath) == "" {
			if task.Queued && task.Bundle != nil && task.Error == nil {
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
		for _, b := range s.taskBundles() {
			if task := s.liveBundleTask(b.Bundle); task != nil {
				ui.Text(c, b.Bundle+" · "+taskLabel(*task))
				continue
			}
			// A failed task chain explains the active step; the recorded progress
			// phase may still describe the pre-failure stage and would contradict
			// Step 4. Report the failure instead of the stale phase.
			if s.taskFailedBundle(b.Bundle) {
				ui.Text(c, b.Bundle+" · 失败").TextColor(stateColor(c, stateDanger))
				continue
			}
			ui.Text(c, b.Bundle+" · "+stateLabel(b.Phase))
			if b.BlockedReason != nil {
				if *b.BlockedReason == "no_local_model" && s.availableTaskAssets().model && s.preparingTask() {
					continue
				}
				color := stateColor(c, stateDanger)
				if b.Phase == "run_incomplete" {
					color = stateColor(c, stateWarning)
				}
				ui.Text(c, human(*b.BlockedReason)).TextColor(color)
			}
		}
		if s.task.failure != nil {
			ui.Text(c, "当前任务已中止："+human(s.task.failure.Code)+"。原因与恢复操作见下方当前阶段。").TextColor(stateColor(c, stateDanger))
		}
	})
}

// serviceCardVisible keeps the model-service panel in the run step whenever no
// run is active and the bound plan is actionable, including after a failure so
// the service can be re-bound without leaving the page.
func (s *viewState) serviceCardVisible() bool {
	if s.running() != nil || (s.task.stage != "" && s.task.failure == nil) {
		return false
	}
	b := s.active()
	return b != nil && !blockedPlan(*b) && (b.CandidatePath != nil || b.BoundPath != nil)
}

// taskFailedBundle reports whether bundle belongs to the failed task chain.
func (s *viewState) taskFailedBundle(bundle string) bool {
	if s.task.failure == nil {
		return false
	}
	if len(s.task.bundles) == 0 {
		return true
	}
	for _, name := range s.task.bundles {
		if name == bundle {
			return true
		}
	}
	return false
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
		stepTrack(c, "3", termBenchmarkStage, func() {
			if s.stepFailed() {
				failureView(c, termBenchmarkStage+"失败", s.activeTaskFailure(), "原因解决后可从本阶段重新开始；已完成的测评题集结果会保留。")
			}
			s.candidateView(c)
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
