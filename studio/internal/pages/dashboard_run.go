package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"path/filepath"
	"strings"
)

var openLogPathFunc = core.OpenPathInOS

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

	modelOptions := []components.NativeSelectOption[string]{{Value: "", Label: tr(c, "dashboard.candidate.select_model")}}
	for _, m := range w.registry.Models {
		if m.Available {
			modelOptions = append(modelOptions, components.NativeSelectOption[string]{Value: m.Name, Label: modelLabel(m) + w.incompatibleModelSuffix(m), Disabled: w.engine != "" && !w.compatibleModel(m)})
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
	ui.Grid(c).FillWidth().Columns(cols).Gap(t.Density.Stack).Children(func() {
		selectField(c, tr(c, "dashboard.candidate.engine"), engineHint, &w.engine, engineOptions)
		selectField(c, tr(c, "dashboard.candidate.model"), "", &w.model, modelOptions)
	})
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
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			statusBadge(c, tr(c, "dashboard.candidate.revoked_badge"), stateColor(c, stateWarning))
			ui.Text(c, trf(c, "dashboard.candidate.revoked_reason", "engine", w.engine, "reason", human(el.Reason)))
		})
		button(c, tr(c, "dashboard.candidate.view_engines")).AlignSelf(ui.Start).OnClick(func() {
			w.libraryTab = 1
			w.navigateTo("library", "")
		})
		createDisabled = true
	} else if !hasEligible {
		if w.checks.Checking == 0 && !w.engineVerifyPending {
			if badName, badEl, ok := w.firstIneligibleEngine(); ok {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
					statusBadge(c, tr(c, "engine.badge.ineligible"), stateColor(c, stateWarning))
					ui.Text(c, badName+" · "+human(badEl.Reason))
				})
				button(c, tr(c, "dashboard.candidate.view_engines")).AlignSelf(ui.Start).OnClick(func() {
					w.libraryTab = 1
					w.navigateTo("library", "")
				})
			} else {
				ui.Text(c, tr(c, "dashboard.candidate.no_engines"))
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
					button(c, tr(c, "onboarding.assets.go_downloads")).OnClick(func() {
						w.libraryTab = 1
						w.navigateTo("library", "")
					})
					button(c, tr(c, "dashboard.candidate.register_engine")).OnClick(func() { w.openGeneralSettings() })
				})
			}
		}
		createDisabled = true
	}
	if w.model == "" && len(modelOptions) == 1 {
		ui.Text(c, tr(c, "dashboard.candidate.no_models"))
		button(c, tr(c, "dashboard.candidate.download_model")).OnClick(func() {
			w.libraryTab = 0
			w.navigateTo("library", "")
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
	if primaryButton(c, tr(c, "dashboard.candidate.start")).AlignSelf(ui.Start).Disabled(createDisabled).Clicked() {
		w.startBenchmark()
	}
}

func macOSBatteryWarning(raw json.RawMessage) bool {
	snapshot, err := parseDeviceSnapshot(raw)
	if err != nil || snapshot == nil {
		return false
	}
	hw := snapshot.object("hardware")
	if hw == nil {
		return false
	}
	platform, _ := hw["platform"].(string)
	if platform != "Darwin" {
		return false
	}
	online, ok := hw["ac_online"].(bool)
	return ok && !online
}

func failureView(c *ui.Context, title string, e *core.ApiError, next string) {
	ui.Text(c, title).Bold().TextColor(stateColor(c, stateDanger))
	if e != nil {
		errorView(c, e)
	} else {
		noticeBanner(c, noticeBannerOptions{Kind: stateDanger, Size: bannerCompact, Message: tr(c, "dashboard.failure.no_detail")})
	}
	if next != "" {
		ui.Text(c, next).TextColor(tokens(c).Color.FgSecondary)
	}
}

func (w *viewState) engineLogDiagnosticsView(c *ui.Context) {
	t := tokens(c)
	logPath := w.engineState.LogPath
	ui.Column(c.Key("engine-log-diagnostics")).FillWidth().Gap(t.Density.Tight).Children(func() {
		componentFoldKeyed(c, "engine-log-tail", tr(c, "dashboard.engine_log.tail_fold"), func() {
			if w.preview != nil {
				ui.Text(c, tr(c, "dashboard.engine_log.preview_none")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				return
			}
			var tail string
			var err error
			if w.service != nil {
				tail, err = w.service.EngineLogTail(logPath, 20)
			} else {
				tail, err = core.ReadLogTail(logPath, 20)
			}
			if err != nil {
				ui.Text(c, trf(c, "dashboard.engine_log.read_failed", "error", err.Error())).FontSize(t.Type.Caption).TextColor(stateColor(c, stateDanger))
				return
			}
			if tail == "" {
				ui.Text(c, tr(c, "dashboard.engine_log.empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				return
			}
			applyMaterial(c, ui.Scroll(c.Key("engine-log-tail-scroll")).FillWidth().MaxHeight(260), materialSunken).
				Label(tr(c, "dashboard.engine_log.tail_label")).Children(func() {
				ui.Column(c).FillWidth().Padding(t.Density.Inset).Children(func() {
					ui.Text(c, tail).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
				})
			})
		})
		outlineButton(c, tr(c, "dashboard.engine_log.open_button")).OnClick(func() {
			if w.preview != nil {
				return
			}
			target := logPath
			if w.service != nil && !filepath.IsAbs(target) {
				if resolved, err := w.service.ResolveEngineLogPath(target); err == nil {
					target = resolved
				}
			}
			if err := openLogPathFunc(target); err != nil {
				w.failure = &core.ApiError{Code: "io_error", Message: l10n.TRf("dashboard.engine_log.open_failed", "error", err.Error())}
				w.failurePage = w.selected
			}
		})
	})
}

func (w *viewState) serviceView(c *ui.Context) {
	heading(c, tr(c, "dashboard.service.heading"), 3)
	if w.bindingStale {
		noticeBanner(c, noticeBannerOptions{Kind: stateDanger, Size: bannerCompact, Message: tr(c, "dashboard.service.binding_stale")})
	}
	if w.stepFailed() {
		title := tr(c, "dashboard.service.failed_title")
		if w.step() == 3 {
			title = tr(c, "dashboard.service.bind_failed_title")
		}
		failureView(c, title, w.activeTaskFailure(),
			tr(c, "dashboard.service.failed_hint"))
	}
	if (w.stepFailed() || w.engineState.State == "failed") && w.engineState.LogPath != "" {
		w.engineLogDiagnosticsView(c)
	}
	switch w.engineState.State {
	case "orphaned":
		noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: tr(c, "dashboard.service.orphaned_notice")})
		errorView(c, w.engineState.Error)
		w.stopOrphanButton(c)
	case "starting":
		host := ui.Column(c.Key("engine-starting")).FillWidth().Gap(tokens(c).Density.Tight)
		showBar, elapsed := delayedElapsed(c, host, "engine-starting", true)
		host.Children(func() {
			ui.Text(c, trf(c, "dashboard.service.starting", "pid", w.engineState.Pid))
			if showBar {
				progressBar(c, -1, tr(c, "dashboard.service.starting_progress"))
				if elapsed >= loadingSlowThreshold {
					ui.Text(c, tr(c, "dashboard.service.starting_slow")).TextColor(tokens(c).Color.FgSecondary)
				}
			}
		})
	case "running":
		badge(c, trf(c, "dashboard.service.running_badge", "pid", w.engineState.Pid, "endpoint", w.engineState.Endpoint), stateSuccess, badgeOptions{Style: badgeDot})
	case "stopping":
		badge(c, tr(c, "dashboard.service.stopping_badge"), stateWarning, badgeOptions{Style: badgeDot})
	}
	candidateReady := w.candidate != "" || w.task.start.candidate != ""
	if !candidateReady {
		ui.Text(c, tr(c, "dashboard.service.need_candidate"))
	}
	if w.running() != nil {
		ui.Text(c, tr(c, "dashboard.service.locked_during_run"))
	}
	startable := w.engineState.State == "" || w.engineState.State == "stopped" || w.engineState.State == "failed"
	ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
		startButton := button
		if startable {
			startButton = primaryButton
		}
		label := tr(c, "dashboard.service.start_button")
		if startButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || !candidateReady || !startable || w.running() != nil).Clicked() {
			w.startEngine()
		}
		if w.engineState.State == "running" && w.failure != nil {
			label := tr(c, "dashboard.service.rebind_failed_button")
			if b := w.bindTarget(); b != nil && b.BoundPath != nil {
				label = tr(c, "dashboard.service.rebind_button")
			}
			if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil).Clicked() {
				if w.task.stage == "bind" {
					w.retryBenchmark()
				} else if w.bindingPhase() {
					w.bindDiscovered(w.engineState.Pid, w.engineState.Endpoint)
				} else {
					target := w.bindTargetBundle()
					command, e := w.managedBindCommand()
					w.executeBindingFor(target, command, e)
				}
			}
		}
		if w.engineState.State == "starting" || w.engineState.State == "running" {
			if w.stopEngineGate(c) {
				w.stopEngine()
			}
		}
	})

	if w.binding {
		ui.Text(c, trf(c, "dashboard.service.binding_bundle", "bundle", displayName(w.bindingBundle)))
	}
	componentFoldKeyed(c, "bind-discovered-services", tr(c, "dashboard.service.connect_existing_fold"), func() { w.discoveryBody(c, true) })
	componentFold(c, tr(c, "dashboard.service.manual_fold"), func() {
		w.ensureManualProcesses()
		heading(c, tr(c, "dashboard.service.manual_heading"), 3)
		options := []string{}
		for _, p := range w.processes {
			options = append(options, fmt.Sprintf("%d · %s", p.PID, p.Name))
		}
		searchableSelectField(c, tr(c, "dashboard.service.manual_process"), "", tr(c, "dashboard.service.manual_process_placeholder"), tr(c, "dashboard.service.manual_process_empty"), &w.process, options)
		field(c, tr(c, "dashboard.service.manual_endpoint"), &w.bindEndpoint).Description(tr(c, "dashboard.service.manual_endpoint_desc")).Error(urlError(w.bindEndpoint))
		if secondaryActionButton(c, tr(c, "dashboard.service.manual_connect_button"), w.binding).AlignSelf(ui.Start).Disabled(w.busy || w.running() != nil || w.process == "" || w.bindEndpoint == "").Clicked() {
			if urlError(w.bindEndpoint) != "" {
				return
			}
			pid, _, _ := strings.Cut(w.process, " · ")
			target := w.bindTargetBundle()
			command, e := w.bindCommand(pid, w.bindEndpoint)
			w.executeBindingFor(target, command, e)
		}
	})
}
func (w *viewState) runView(c *ui.Context) {
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
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "dashboard.run.heading"), 3).Grow(1)
			if value >= 0 {
				ui.Text(c, fmt.Sprintf("%.0f%%", value*100)).Bold().FontSize(t.Type.Section).FontFeatures("tnum").TextColor(stateColor(c, stateActive))
			}
		})
		if bundleName := emptyText(task.Bundle); bundleName != "" || w.model != "" || w.engine != "" {
			applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
				Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
				if w.model != "" {
					modelIconBox(c, w.modelIconHint(w.model), 28, t.Radius.SM)
				} else {
					components.Icon(c, lucide.Play).Size(15, 15).TextColor(stateColor(c, stateActive))
				}
				if bundleName != "" {
					ui.Text(c, bundleName).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				}
				if w.model != "" {
					ui.Text(c, w.model).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
				}
				if w.engine != "" {
					w.enginePill(c, w.engine)
				}
				ui.Box(c).Grow(1)
				if p != nil && p.CurrentCaseId != nil && *p.CurrentCaseId != "" {
					inlineBadge(c, trf(c, "detail.current_case", "case_id", *p.CurrentCaseId), stateActive, badgeOptions{Size: sizeSM, Style: badgeSoft})
				}
			})
		}
		progressBar(c, value, tr(c, "dashboard.run.progress_bar_label"))
		if task.State == "cancelling" {
			ui.Text(c, tr(c, "dashboard.run.cancelling_notice")).FontSize(t.Type.Caption).TextColor(stateColor(c, stateWarning))
		}
		row := ui.Row(c.Key("run-elapsed-" + task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row)
		elapsed := ""
		if task.State != "cancelling" && !task.AwaitingService {
			elapsed = w.formatTaskElapsed(c, emptyText(task.StartedAt))
		}
		row.Children(func() {
			ui.Text(c, label).FontFeatures("tnum")
			if elapsed != "" {
				ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			}
		})
		id := task.TaskId
		var cancelConfirmed bool
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			detailButton := primaryButton
			if task.AwaitingService {
				detailButton = outlineButton
			}
			detailButton(c, tr(c, "dashboard.run.view_live_detail")).AlignSelf(ui.Start).OnClick(func() { w.selectTask(id) })
			cancelConfirmed = w.cancelRunGate(c, id, task.State)
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
	if primaryButton(c, label).AlignSelf(ui.Start).Disabled(w.busy || !w.runServiceReady(*b)).Clicked() {
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

}

func (w *viewState) runServiceReady(b inferyard.BundleProgress) bool {
	if w.engineState.State == "running" {
		return true
	}
	return b.NextAction == "run" && b.BoundPath != nil
}
