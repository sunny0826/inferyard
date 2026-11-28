package pages

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

var openLogPathFunc = core.OpenPathInOS

func (w *viewState) openEngineLogFile(logPath string) {
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
	})
}

func (w *viewState) serviceView(c *ui.Context) {
	w.serviceViewWithBar(c, true)
}

func (w *viewState) serviceViewWithBar(c *ui.Context, embedBar bool) {
	t := tokens(c)
	showLogIcon := (w.stepFailed() || w.engineState.State == "failed") && w.engineState.LogPath != ""
	showStopIcon := w.engineState.State == "starting" || w.engineState.State == "running"
	studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
		heading(c, tr(c, "dashboard.service.heading"), 3)
		switch w.engineState.State {
		case "running":
			badge(c, trf(c, "dashboard.service.running_badge", "pid", w.engineState.Pid, "endpoint", w.engineState.Endpoint), stateSuccess, badgeOptions{Style: badgeDot})
		case "stopping":
			badge(c, tr(c, "dashboard.service.stopping_badge"), stateWarning, badgeOptions{Style: badgeDot})
		}
		ui.Box(c).Grow(1)
		if showLogIcon {
			outlineIconButton(c, reicon.FileText, tr(c, "dashboard.engine_log.open_button")).OnClick(func() {
				w.openEngineLogFile(w.engineState.LogPath)
			})
		}
		if showStopIcon {
			if w.stopEngineGate(c) {
				w.stopEngine()
			}
		}
	})
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
	if showLogIcon {
		w.engineLogDiagnosticsView(c)
	}
	switch w.engineState.State {
	case "orphaned":
		noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: tr(c, "dashboard.service.orphaned_notice")})
		errorView(c, w.engineState.Error)
		w.stopOrphanButton(c)
	case "starting":
		host := ui.Column(c.Key("engine-starting")).FillWidth().Gap(t.Density.Tight)
		showBar, elapsed := delayedElapsed(c, host, "engine-starting", true)
		host.Children(func() {
			ui.Text(c, trf(c, "dashboard.service.starting", "pid", w.engineState.Pid))
			if showBar {
				if embedBar {
					progressBar(c, -1, tr(c, "dashboard.service.starting_progress"))
				}
				if elapsed >= loadingSlowThreshold {
					ui.Text(c, tr(c, "dashboard.service.starting_slow")).TextColor(t.Color.FgSecondary)
				}
			}
		})
	}
	candidateReady := w.candidate != "" || w.task.start.candidate != ""
	if !candidateReady {
		ui.Text(c, tr(c, "dashboard.service.need_candidate"))
	}
	if w.running() != nil {
		ui.Text(c, tr(c, "dashboard.service.locked_during_run"))
	}
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
		ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if secondaryActionButton(c, tr(c, "dashboard.service.manual_connect_button"), w.binding).Disabled(w.busy || w.running() != nil || w.process == "" || w.bindEndpoint == "").Clicked() {
				if urlError(w.bindEndpoint) != "" {
					return
				}
				pid, _, _ := strings.Cut(w.process, " · ")
				target := w.bindTargetBundle()
				command, e := w.bindCommand(pid, w.bindEndpoint)
				w.executeBindingFor(target, command, e)
			}
		})
	})

	startable := w.engineState.State == "" || w.engineState.State == "stopped" || w.engineState.State == "failed"
	showRebind := w.engineState.State == "running" && w.failure != nil
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		startButton := button
		if startable {
			startButton = primaryButton
		}
		if startButton(c, tr(c, "dashboard.service.start_button")).Disabled(w.busy || !candidateReady || !startable || w.running() != nil).Clicked() {
			w.startEngine()
		}
		if showRebind {
			label := tr(c, "dashboard.service.rebind_failed_button")
			if b := w.bindTarget(); b != nil && b.BoundPath != nil {
				label = tr(c, "dashboard.service.rebind_button")
			}
			if primaryButton(c, label).Disabled(w.busy || w.running() != nil).Clicked() {
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
	})
}
