package pages

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func (w *viewState) engineChoices() ([]components.NativeSelectOption[string], bool, bool) {
	eligible := []string{}
	for _, e := range w.registry.Engines {
		el := w.eligibility(e.Name)
		if el.State == core.EngineCheckEligible || (!w.checks.Supported && el.State != core.EngineCheckIneligible) {
			eligible = append(eligible, e.Name)
		}
	}
	options := []components.NativeSelectOption[string]{}
	listed := false
	for _, name := range eligible {
		options = append(options, components.NativeSelectOption[string]{Value: name, Label: name})
		if name == w.engine {
			listed = true
		}
	}
	revoked := false
	if w.engine != "" && !listed {
		el := w.eligibility(w.engine)
		suffix := l10n.TR("dashboard.engine.checking_suffix")
		if el.State == core.EngineCheckIneligible {
			suffix = l10n.TR("dashboard.engine.ineligible_suffix")
			revoked = true
		}
		options = append(options, components.NativeSelectOption[string]{Value: w.engine, Label: w.engine + suffix, Disabled: true})
	}
	return options, revoked, len(eligible) > 0
}

func (w *viewState) candidateView(c *ui.Context) {
	w.speedTypeSwitch(c)
	w.candidateFields(c)
}

func (w *viewState) candidateFields(c *ui.Context) {
	w.verifyTaskEngines()
	w.adoptDownloadedEngine()
	w.adoptOnlyModel()
	w.reconcileCandidateBeforeRender()

	modelOptions := []components.NativeSelectOption[string]{{Value: "", Label: tr(c, "dashboard.candidate.select_model")}}
	for _, m := range w.registry.Models {
		if m.Available {
			modelOptions = append(modelOptions, components.NativeSelectOption[string]{Value: m.Name, Label: modelLabel(m) + w.candidateModelSuffix(m)})
		}
	}

	t := tokens(c)
	engineOptions, revoked, hasEligible := w.engineChoices()
	engineHint := ""
	if !w.checks.Supported {
		engineHint = tr(c, "dashboard.candidate.cli_outdated_hint")
	}
	cols := 2
	if compactLayout(c) {
		cols = 1
	}
	prevModel, prevEngine := w.model, w.engine
	ui.Grid(c).FillWidth().Columns(cols).Gap(t.Density.Stack).Children(func() {
		selectField(c, tr(c, "dashboard.candidate.engine"), engineHint, &w.engine, engineOptions)
		selectField(c, tr(c, "dashboard.candidate.model"), "", &w.model, modelOptions)
	})
	w.applyCandidateSelectionDiff(prevModel, prevEngine)
	w.recordCandidateSelection()
	if w.checks.Checking > 0 {
		ui.Text(c, trf(c, "dashboard.candidate.verifying_engines", "count", w.checks.Checking)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	}

	meta := w.modelMetadata(w.model)
	speedSelected := w.speed != nil && w.speed.selected
	summary := ""
	if !speedSelected {
		summary = launchSummaryLine(w)
	}
	if meta != "" || summary != "" {
		applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
			if w.model != "" {
				modelIconBox(c, w.modelIconHint(w.model), 28, t.Radius.SM)
			}
			if meta != "" {
				ui.Text(c, meta).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgPrimary)
			}
			if summary != "" {
				if meta != "" {
					ui.Text(c, "·").FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
				}
				ui.Text(c, summary).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			}
		})
	}

	if w.speedSelection(c) {
		return
	}
	w.bundleSelection(c)
	w.localSelectionView(c)
	if notice := lmStudioHandoffNotice(w); notice != "" {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateInfo,
			Size:    bannerCompact,
			Message: notice,
		})
	}
	createDisabled := w.environmentMissing() || !w.preflightReady() || w.busy || w.engine == "" || w.model == "" || revoked || len(w.checkedBundles()) == 0 || w.engineState.State == "orphaned" || w.localSelectionError() != ""
	if revoked {
		el := w.eligibility(w.engine)
		applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
			statusBadge(c, tr(c, "dashboard.candidate.revoked_badge"), stateColor(c, stateWarning))
			ui.Text(c, trf(c, "dashboard.candidate.revoked_reason", "engine", w.engine, "reason", human(el.Reason))).Grow(1).MinWidth(160)
			outlineIconButton(c, reicon.Cpu, tr(c, "dashboard.candidate.view_engines")).OnClick(func() {
				w.libraryTab = 1
				w.navigateTo("library", "")
			})
		})
		createDisabled = true
	} else if !hasEligible {
		if w.checks.Checking == 0 && !w.engineVerifyPending {
			if badName, badEl, ok := w.firstIneligibleEngine(); ok {
				applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
					Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
					statusBadge(c, tr(c, "engine.badge.ineligible"), stateColor(c, stateWarning))
					ui.Text(c, badName+" · "+human(badEl.Reason)).Grow(1).MinWidth(160)
					outlineIconButton(c, reicon.Cpu, tr(c, "dashboard.candidate.view_engines")).OnClick(func() {
						w.libraryTab = 1
						w.navigateTo("library", "")
					})
				})
			} else {
				applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
					Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
					components.Icon(c, reicon.Cpu).Size(14, 14).TextColor(t.Color.FgSecondary)
					ui.Text(c, tr(c, "dashboard.candidate.no_engines")).Grow(1).MinWidth(160)
					studioTooltipGroup(c, ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row), func() {
						outlineIconButton(c, reicon.Download, tr(c, "onboarding.assets.go_downloads")).OnClick(func() {
							w.libraryTab = 1
							w.navigateTo("library", "")
						})
						outlineIconButton(c, reicon.Link, tr(c, "dashboard.candidate.register_engine")).OnClick(func() {
							w.openLocalEngineWizard("")
						})
					})
				})
			}
		}
		createDisabled = true
	}
	if w.model == "" && len(modelOptions) == 1 {
		applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
			components.Icon(c, reicon.Box).Size(14, 14).TextColor(t.Color.FgSecondary)
			ui.Text(c, tr(c, "dashboard.candidate.no_models")).Grow(1).MinWidth(160)
			outlineIconButton(c, reicon.Download, tr(c, "dashboard.candidate.download_model")).OnClick(func() {
				w.libraryTab = 0
				w.navigateTo("library", "")
			})
		})
	}
	if w.engineState.State == "orphaned" {
		w.stopOrphanButton(c)
	}
	if macOSBatteryWarning(w.device.Snapshot) {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Size:    bannerCompact,
			Message: tr(c, "dashboard.candidate.macos_battery_warning"),
		})
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Children(func() {
		if primaryButton(c, tr(c, "dashboard.candidate.start")).Disabled(createDisabled).Clicked() {
			w.startBenchmark()
		}
	})
}

func (w *viewState) runView(c *ui.Context) {
	w.runViewWithBar(c, true)
}

func (w *viewState) runViewWithBar(c *ui.Context, embedBar bool) {
	t := tokens(c)
	if w.stepFailed() && w.running() == nil && !w.serviceCardVisible() {
		heading(c, tr(c, "dashboard.run.heading"), 3)
		failureView(c, tr(c, "dashboard.run.incomplete_title"), w.activeTaskFailure(), tr(c, "dashboard.retry.completed_kept"))
	}
	if task := w.running(); task != nil {
		value := -1.0
		label := tr(c, "dashboard.run.waiting_progress")
		if task.PreparationStage != "" {
			label = taskLabelFor(contextLocale(c), *task)
		}
		var total int64
		p := w.runProgress
		if p != nil && p.Total != nil && *p.Total > 0 {
			total = *p.Total
			if p.Done > 0 || p.CurrentCaseId != nil {
				value = progressValue(p.Done, p.Total)
			}
			label = trf(c, "dashboard.run.cases_progress", "done", p.Done, "total", total)
			if phase := emptyText(p.Phase); phase != "" && p.Done == 0 && p.CurrentCaseId == nil {
				label += " · " + stateLabelFor(contextLocale(c), phase)
			}
		}
		id := task.TaskId
		var cancelConfirmed bool
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c).Grow(1).MinWidth(180).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if w.model != "" {
					modelIconBox(c, w.modelIconHint(w.model), 30, t.Radius.SM)
				} else {
					components.Icon(c, reicon.Play).Size(15, 15).TextColor(stateColor(c, stateActive))
				}
				if bundleName := emptyText(task.Bundle); bundleName != "" {
					ui.Text(c, bundleName).Bold().FontSize(t.Type.Section).TextColor(t.Color.FgPrimary).SingleLine()
				} else {
					heading(c, tr(c, "dashboard.run.heading"), 3)
				}
				if w.model != "" {
					ui.Text(c, w.model).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
				}
				if w.engine != "" {
					w.enginePill(c, w.engine)
				}
			})
			studioTooltipGroup(c, ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row), func() {
				detailIconBtn := primaryIconButton
				if task.AwaitingService {
					detailIconBtn = outlineIconButton
				}
				detailIconBtn(c, reicon.Eye, tr(c, "dashboard.run.view_live_detail")).OnClick(func() { w.selectTask(id) })
				cancelConfirmed = w.cancelRunGate(c, id, task.State)
			})
		})
		if embedBar {
			progressBar(c, value, tr(c, "dashboard.run.progress_bar_label"))
		}
		if task.State == "cancelling" {
			ui.Text(c, tr(c, "dashboard.run.cancelling_notice")).FontSize(t.Type.Caption).TextColor(stateColor(c, stateWarning))
		}
		elapsed := ""
		if task.State != "cancelling" && !task.AwaitingService {
			elapsed = w.formatTaskElapsed(c, emptyText(task.StartedAt))
		}
		applyMaterial(c, ui.Row(c.Key("run-elapsed-"+task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
			ambientLifecycleDot(c, t.Color.Active, task.State != "cancelling" && !task.AwaitingService)
			ui.Text(c, label).FontFeatures("tnum")
			if elapsed != "" {
				ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			}
			ui.Box(c).Grow(1)
			if p != nil && p.CurrentCaseId != nil && *p.CurrentCaseId != "" {
				inlineBadge(c, trf(c, "detail.current_case", "case_id", *p.CurrentCaseId), stateActive, badgeOptions{Size: sizeSM, Style: badgeSoft})
			}
			if embedBar && value >= 0 {
				ui.Text(c, fmt.Sprintf("%.0f%%", value*100)).Bold().FontSize(t.Type.Section).FontFeatures("tnum").TextColor(stateColor(c, stateActive))
			}
		})
		if cancelConfirmed {
			if w.task.stage != "" {
				w.newBenchmark()
				return
			}
			if w.simulate(previewCancelRun{id: id}) {
				return
			}
			w.guideWork(func() (string, *core.ApiError) {
				_, e := w.cancelRunTask(id)
				return l10n.TR("dashboard.toast.cancel_requested"), e
			})
		}
		return
	}
	if !(w.stepFailed() && !w.serviceCardVisible()) {
		heading(c, tr(c, "dashboard.run.heading"), 3)
	}
	for _, task := range w.tasks {
		if task.State == "failed" || task.State == "cancelled" {
			ui.Text(c, tr(c, "dashboard.run.last_incomplete"))
			break
		}
	}
	b := w.active()
	if b == nil || b.NextAction == "none" {
		return
	}
	if b.BlockedReason != nil {
		ui.Text(c, human(*b.BlockedReason))
	}
	if blockedPlan(*b) {
		return
	}
	label := tr(c, "dashboard.run.start_button")
	if b.NextAction == "resume" {
		label = tr(c, "dashboard.run.resume_button")
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Children(func() {
		if primaryButton(c, label).Disabled(w.busy || !w.runServiceReady(*b)).Clicked() {
			if w.simulate(previewStartRun{bundle: b.Bundle}) {
				return
			}
			bundle := *b
			ctx, cancel := context.WithCancel(w.ctx)
			w.taskCancel = cancel
			state, workspace := w.engineState, w.progress.Workspace
			w.guideWork(func() (string, *core.ApiError) {
				defer cancel()
				e := executeExistingBundle(ctx, bundle, workspace, state, w.service.WizardExecute)
				return l10n.TR("dashboard.toast.run_queued"), e
			})
		}
	})
}

func (w *viewState) runServiceReady(b inferyard.BundleProgress) bool {
	if w.engineState.State == "running" {
		return true
	}
	return b.NextAction == "run" && b.BoundPath != nil
}
