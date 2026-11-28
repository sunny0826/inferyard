package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/icons/reicon"
)

func (w *viewState) step() int {
	if w.task.stage != "" {
		switch w.task.stage {
		case "prepare_candidate":
			if w.localEngine() == nil && !w.hasLocalEngine() && w.progress != nil && w.progress.DevicePreflight.Status == "missing" {
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
		if w.hasLocalEngine() {
			return 2
		}
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
		s.flowHeader(c, tr(c, "dashboard.title"))
		host := ui.Column(c.Key("new-task-pending")).FillWidth()
		show := delayedVisible(c, host, "new-task-pending", true)
		host.Children(func() {
			if show {
				card(c, tr(c, "dashboard.cancelling.title"), func() {
					ui.Text(c, tr(c, "dashboard.cancelling.body")).TextColor(c.Theme().TextMuted)
					progressBar(c, -1, tr(c, "dashboard.cancelling.progress"))
				})
			}
		})
		return
	}
	s.flowHeader(c, tr(c, "dashboard.title"))
	if s.environmentMissing() {
		s.setupGuide(c)
		return
	}
	if s.needsTaskAssets() {
		s.taskAssetsView(c)
		return
	}
	s.workflowView(c)
	s.awaitingLocalServiceView(c)
	if len(s.task.start.localCandidates) > 0 && s.task.failure != nil && !s.busy {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Message: tr(c, "dashboard.retry.completed_kept"),
			Actions: func() {
				primaryIconButton(c, reicon.RotateCw, tr(c, "dashboard.retry.button")).OnClick(s.retryBenchmark)
			},
		})
	}
	for _, task := range s.dashboardTaskNotices() {
		ui.Text(c, taskLabel(task))
		errorView(c, task.Error)
	}
}

func (s *viewState) environmentMissing() bool {
	return s.ready.CLI.Status != "ok" || !s.ready.Workspace.Initialized || !s.ready.Results.Exists || !s.ready.Models.Exists
}

func (s *viewState) benchmarkWorkflowSteps(c *ui.Context) ([]workflowStepItem, int, bool, string, stateKind) {
	step := s.step()
	failed := s.stepFailed()
	stepText := fmt.Sprintf("%d / 6", min(6, max(1, step+1)))
	stepKind := stateActive
	switch {
	case step >= 6:
		stepText = "6 / 6"
		stepKind = stateSuccess
	case failed:
		stepKind = stateDanger
	}
	configSummary := s.model
	if s.model != "" && s.engine != "" {
		configSummary = s.model + " · " + s.engine
	} else if s.engine != "" {
		configSummary = s.engine
	}
	bindSummary := ""
	if s.engineState.Pid > 0 && s.engineState.Endpoint != "" {
		bindSummary = fmt.Sprintf("PID %d · %s", s.engineState.Pid, s.engineState.Endpoint)
	} else if s.engineState.Endpoint != "" {
		bindSummary = s.engineState.Endpoint
	}
	items := []workflowStepItem{
		{Label: tr(c, "dashboard.stage.prepare")},
		{Label: tr(c, "dashboard.stage.device_check")},
		{Label: tr(c, "dashboard.stage.configure"), Summary: configSummary},
		{Label: tr(c, "dashboard.stage.bind"), Summary: bindSummary},
		{Label: tr(c, "dashboard.stage.run")},
		{Label: tr(c, "dashboard.stage.report")},
	}
	return items, step, failed, stepText, stepKind
}

func speedWorkflowSteps(c *ui.Context, current int, failed bool, r *speedTask) ([]workflowStepItem, string, stateKind) {
	stepText := fmt.Sprintf("%d / 5", min(5, max(1, current+1)))
	stepKind := stateActive
	switch {
	case current >= 5:
		stepText = "5 / 5"
		stepKind = stateSuccess
	case failed:
		stepKind = stateDanger
	}
	var configSummary, measureSummary string
	if r != nil {
		configSummary = fmt.Sprintf("%s · %s", r.model, speedContext(r.context))
		if r.completed > 0 {
			measureSummary = trf(c, "speed.task.completed_count", "completed", r.completed)
		}
	}
	items := []workflowStepItem{
		{Label: tr(c, "speed.step.prepare"), Summary: configSummary},
		{Label: tr(c, "speed.step.bind")},
		{Label: tr(c, "speed.step.plan")},
		{Label: tr(c, "speed.step.measure"), Summary: measureSummary},
		{Label: tr(c, "speed.step.read")},
	}
	return items, stepText, stepKind
}

func (s *viewState) currentStageProgress(c *ui.Context, action string) workflowProgressInfo {
	if s.isPreparingBenchmark() {
		return workflowProgressInfo{
			Active:    true,
			Value:     -1,
			AriaLabel: tr(c, "sidebar.status.queue_preparing"),
		}
	}
	if action == "bind" && s.engineState.State == "starting" {
		return workflowProgressInfo{
			Active:    true,
			Value:     -1,
			AriaLabel: tr(c, "dashboard.service.starting_progress"),
		}
	}
	if task := s.running(); task != nil && (action == "run" || action == "resume") {
		val := -1.0
		pct := ""
		p := s.runProgress
		if p != nil && p.Total != nil && *p.Total > 0 && (p.Done > 0 || p.CurrentCaseId != nil) {
			val = progressValue(p.Done, p.Total)
			if val >= 0 {
				pct = fmt.Sprintf("%.0f%%", val*100)
			}
		}
		return workflowProgressInfo{
			Active:       true,
			Value:        val,
			PercentLabel: pct,
			AriaLabel:    tr(c, "dashboard.run.progress_bar_label"),
		}
	}
	return workflowProgressInfo{}
}

func (s *viewState) stageAction() string {
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
	return action
}

func (s *viewState) stageHandlesFailure(action string) bool {
	switch action {
	case "prepare_candidate":
		return s.task.failure != nil && !s.isPreparingBenchmark()
	case "bind":
		return s.stepFailed()
	case "run", "resume":
		return s.stepFailed() && (s.running() == nil || s.serviceCardVisible())
	default:
		return false
	}
}

func (s *viewState) renderBundleStatusRow(c *ui.Context) {
	bundles := s.taskBundles()
	if len(bundles) == 0 {
		return
	}
	failed := s.failedBundles()
	for _, b := range bundles {
		if task := s.liveBundleTask(b.Bundle); task != nil {
			kind := stateActive
			if task.Queued && task.State == "starting" {
				kind = stateNeutral
			}
			badge(c, b.Bundle+" · "+taskLabel(*task), kind, badgeOptions{Style: badgeDot})
			continue
		}
		if failed[b.Bundle] {
			badge(c, trf(c, "dashboard.bundle.failed_badge", "bundle", b.Bundle), stateDanger, badgeOptions{Style: badgeDot})
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
	}
}

func (s *viewState) renderStageAlerts(c *ui.Context, action string) {
	bundles := s.taskBundles()
	failed := s.failedBundles()
	for _, b := range bundles {
		if s.liveBundleTask(b.Bundle) != nil || failed[b.Bundle] {
			continue
		}
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
	if (s.task.failure != nil || s.bindFailure != nil) && !s.stageHandlesFailure(action) {
		cause, _ := s.stepDiagnosis()
		code := ""
		if cause != nil {
			code = cause.Code
		}
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Size:    bannerCompact,
			Message: trf(c, "dashboard.task.aborted", "reason", human(code)),
		})
	}
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

func (s *viewState) isPreparingBenchmark() bool {
	if s.task.failure != nil {
		return false
	}
	if s.probingReuse {
		return true
	}
	return s.busy && s.busyAction == "prepare-benchmark" && s.task.stage == "prepare_candidate"
}

func (s *viewState) renderPreparingBenchmarkView(c *ui.Context, embedBar bool) {
	t := tokens(c)
	ui.Column(c.Key("preparing-benchmark-card")).FillWidth().Gap(t.Density.Row).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if s.model != "" {
				modelIconBox(c, s.modelIconHint(s.model), 26, t.Radius.SM)
				ui.Text(c, s.model).Bold().FontSize(t.Type.Section).SingleLine()
			}
			if s.engine != "" {
				s.enginePill(c, s.engine)
			}
			for _, bundle := range s.task.bundles {
				inlineBadge(c, bundle, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
			}
			inlineBadge(c, tr(c, "benchmarks.badge.active_flow"), stateActive, badgeOptions{Size: sizeSM, Style: badgeDot})
		})
		if embedBar {
			progressBar(c, -1, tr(c, "sidebar.status.queue_preparing"))
		}
	})
}

func (s *viewState) workflowView(c *ui.Context) {
	action := s.stageAction()
	configuring := action == "prepare_candidate" && !s.isPreparingBenchmark()
	speedTabSelected := configuring && s.speed != nil && s.speed.selected

	var steps []workflowStepItem
	var current int
	var failed bool
	var stepCount string
	var stepKind stateKind
	rowLabel := tr(c, "dashboard.progress.row_label")
	if speedTabSelected {
		steps, stepCount, stepKind = speedWorkflowSteps(c, 0, false, nil)
		rowLabel = tr(c, "speed.task.progress_card")
	} else if s.progress != nil {
		steps, current, failed, stepCount, stepKind = s.benchmarkWorkflowSteps(c)
	}

	var modeSwitch func()
	if configuring && s.speed != nil {
		modeSwitch = func() {
			s.speedTypeSwitch(c)
		}
	}

	var subHeader func()
	if !speedTabSelected && s.progress != nil {
		bundles := s.taskBundles()
		showBundles := len(bundles) > 1 || (len(bundles) == 1 && (s.running() == nil || action != "run"))
		if showBundles {
			subHeader = func() { s.renderBundleStatusRow(c) }
		}
	}

	var prog workflowProgressInfo
	if !speedTabSelected {
		prog = s.currentStageProgress(c, action)
	}

	pipelineCard(c, pipelineCardOptions{
		Key:        "workspace-progress-card",
		ModeSwitch: modeSwitch,
		Steps:      steps,
		Current:    current,
		Failed:     failed,
		RowLabel:   rowLabel,
		StepCount:  stepCount,
		StepKind:   stepKind,
		Progress:   prog,
		SubHeader:  subHeader,
		Body: func(embedBar bool) {
			if !speedTabSelected && s.progress != nil {
				s.renderStageAlerts(c, action)
			}
			s.renderStageBody(c, action, embedBar)
		},
	})
}

func (s *viewState) renderStageBody(c *ui.Context, action string, embedBar bool) {
	switch action {
	case "device_check":
		s.checkWorkspaceDevice()
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			ui.Text(c, tr(c, "dashboard.device_check.running")).Grow(1).MinWidth(0)
			outlineIconButton(c, reicon.Cpu, tr(c, "dashboard.device_check.open_device")).OnClick(func() {
				s.libraryHwExpanded = true
				s.navigateTo("library", "")
			})
		})
	case "prepare_candidate":
		if s.isPreparingBenchmark() {
			s.renderPreparingBenchmarkView(c, embedBar)
			return
		}
		if s.task.failure != nil && (s.speed == nil || !s.speed.selected) {
			failureView(c, tr(c, "dashboard.stage.configure_failed"), s.task.failure, tr(c, "dashboard.retry.completed_kept"))
		}
		s.candidateFields(c)
	case "bind":
		s.serviceViewWithBar(c, embedBar)
	case "run", "resume":
		if s.serviceCardVisible() {
			s.serviceViewWithBar(c, embedBar)
			ui.Box(c).FillWidth().Height(1).Background(tokens(c).Color.BorderSubtle)
		}
		s.runViewWithBar(c, embedBar)
	case "report":
		if s.task.stage != "" {
			ui.Text(c, tr(c, "dashboard.report.generating"))
		} else {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				ui.Text(c, tr(c, "dashboard.report.open_hint")).Grow(1).MinWidth(0)
				primaryIconButton(c, reicon.Eye, tr(c, "dashboard.completion.view_report")).OnClick(func() { s.selected = "reports" })
			})
		}
	case "done":
		s.completionView(c)
	default:
		if s.progress == nil {
			ui.Text(c, tr(c, "dashboard.progress.unreadable"))
		} else {
			ui.Text(c, trf(c, "dashboard.progress.unknown_stage", "stage", action))
		}
	}
}
