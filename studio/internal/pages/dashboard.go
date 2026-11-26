package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
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
	if s.speed == nil || !s.speed.selected {
		s.conclusionView(c)
	}
	s.stageView(c)
	s.awaitingLocalServiceView(c)
	if len(s.task.start.localCandidates) > 0 && s.task.failure != nil && !s.busy {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Message: tr(c, "dashboard.retry.completed_kept"),
			Actions: func() {
				button(c, tr(c, "dashboard.retry.button")).OnClick(s.retryBenchmark)
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

func (s *viewState) conclusionView(c *ui.Context) {
	if s.progress == nil {
		return
	}
	t := tokens(c)
	step := s.step()
	stepText := fmt.Sprintf("%d / 6", min(6, max(1, step+1)))
	stepKind := stateActive
	if step >= 6 {
		stepText = "6 / 6"
		stepKind = stateSuccess
	}
	applyMaterial(c, ui.Column(c.Key("workspace-progress-card")).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "dashboard.progress.title"), 2).Grow(1)
			inlineBadge(c, stepText, stepKind, badgeOptions{Size: sizeSM})
		})
		phaseProgress(c, phaseProgressOptions{
			Labels: []string{
				tr(c, "dashboard.stage.prepare"),
				tr(c, "dashboard.stage.device_check"),
				tr(c, "dashboard.stage.configure"),
				tr(c, "dashboard.stage.bind"),
				tr(c, "dashboard.stage.run"),
				tr(c, "dashboard.stage.report"),
			},
			Current:  step,
			RowLabel: tr(c, "dashboard.progress.row_label"),
			BarLabel: tr(c, "dashboard.progress.bar_label"),
		})
		bundles := s.taskBundles()
		failed := s.failedBundles()
		if len(bundles) > 0 {
			applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Section).
				Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
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
			})
		}
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
		if s.task.failure != nil || s.bindFailure != nil {
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
		card(c, tr(c, "dashboard.stage.configure"), body)
		return
	}
	stepTrack(c, "3", tr(c, "dashboard.stage.configure"), body)
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

func (s *viewState) renderPreparingBenchmarkView(c *ui.Context) {
	t := tokens(c)
	ui.Column(c.Key("preparing-benchmark-card")).FillWidth().Gap(t.Density.Row).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if s.model != "" {
				ui.Text(c, s.model).Bold().FontSize(t.Type.Section).SingleLine()
			}
			if s.engine != "" {
				enginePill(c, s.engine)
			}
			for _, bundle := range s.task.bundles {
				inlineBadge(c, bundle, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
			}
			inlineBadge(c, tr(c, "benchmarks.badge.active_flow"), stateActive, badgeOptions{Size: sizeSM, Style: badgeDot})
		})
		progressBar(c, -1, tr(c, "sidebar.status.queue_preparing"))
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
		stepTrack(c, "2", tr(c, "dashboard.stage.device_check"), func() {
			ui.Text(c, tr(c, "dashboard.device_check.running"))
			s.checkWorkspaceDevice()
			button(c, tr(c, "dashboard.device_check.open_device")).OnClick(func() {
				s.libraryHwExpanded = true
				s.navigateTo("library", "")
			})
		})
	case "prepare_candidate":
		if s.isPreparingBenchmark() {
			s.configStage(c, func() {
				s.renderPreparingBenchmarkView(c)
			})
			break
		}
		ui.Column(c.Key("config-stage")).Gap(tokens(c).Density.CardGap).Children(func() {
			s.speedTypeSwitch(c)
			s.configStage(c, func() {
				if s.task.failure != nil {
					failureView(c, tr(c, "dashboard.stage.configure_failed"), s.task.failure, tr(c, "dashboard.retry.completed_kept"))
				}
				s.candidateFields(c)
			})
		})
	case "bind":
		stepTrack(c, "4", tr(c, "dashboard.stage.bind"), func() { s.serviceView(c) })
	case "run", "resume":
		if s.serviceCardVisible() {
			card(c, tr(c, "dashboard.service.card_title"), func() { s.serviceView(c) })
		}
		stepTrack(c, "5", tr(c, "dashboard.stage.run"), func() { s.runView(c) })
	case "report":
		stepTrack(c, "6", tr(c, "dashboard.stage.report"), func() {
			if s.task.stage != "" {
				ui.Text(c, tr(c, "dashboard.report.generating"))
			} else {
				ui.Text(c, tr(c, "dashboard.report.open_hint"))
				button(c, tr(c, "dashboard.completion.view_report")).OnClick(func() { s.selected = "reports" })
			}
		})
	case "done":
		stepTrack(c, "6", tr(c, "dashboard.stage.report"), func() { s.completionView(c) })
	default:
		if s.progress == nil {
			ui.Text(c, tr(c, "dashboard.progress.unreadable"))
		} else {
			ui.Text(c, trf(c, "dashboard.progress.unknown_stage", "stage", action))
		}
	}
}
