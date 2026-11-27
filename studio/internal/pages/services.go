package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) servicesView(c *ui.Context) {
	t := tokens(c)
	needsSetup := s.environmentMissing()
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			s.header(c, tr(c, "services.page.title"))
			ui.Text(c, s.servicesSummaryLine(c)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		if needsSetup {
			return
		}
		canOpenLauncher := s.serveSupported || s.preview != nil
		blockedTip := ""
		if !canOpenLauncher && !s.serveProbing {
			blockedTip = tr(c, "models.serve.blocked.cli_outdated")
		}
		startModelBtn := withDisabledTooltip(c, blockedTip, func() ui.Element {
			return primaryActionButton(c, tr(c, "services.page.action.start_model"), false).
				Disabled(!canOpenLauncher || s.serveProbing || s.busy)
		})
		if startModelBtn.Clicked() {
			s.openServiceParamsDrawerForNew()
		}
	})

	if needsSetup {
		s.setupGuide(c)
		return
	}

	s.renderExitedServiceCard(c)

	section(c, tr(c, "services.section.managed"), func() {
		s.managedServiceSection(c)
	})

	if s.hasVisibleRecentServices() {
		section(c, tr(c, "services.section.recent"), func() {
			s.recentServicesSection(c)
		})
	}

	ui.Column(c).Gap(t.Density.CardGap).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "services.section.discovered"), 3).Grow(1)
			refreshIconButton(c, tr(c, "engine.discovery.refresh"), s.discovering).Disabled(s.discovering || s.busy).OnClick(s.refreshDiscovery)
		})
		s.discoveredServicesSection(c)
	})
}

func (s *viewState) servicesSummaryLine(c *ui.Context) string {
	var parts []string
	active := s.activeManagedServices()
	preparing := s.pendingStandalonePreparations()
	switch {
	case len(active) > 1:
		parts = append(parts, trf(c, "services.page.summary_running_count", "count", len(active)))
	case len(active) == 1:
		parts = append(parts, tr(c, "services.page.summary_running"))
	case len(preparing) > 0:
		parts = append(parts, tr(c, "sidebar.status.service_starting"))
	case s.hasOrphanService("") || s.engineState.State == "orphaned":
		parts = append(parts, tr(c, "services.page.summary_orphaned"))
	case s.engineState.State == "failed":
		parts = append(parts, tr(c, "services.page.summary_failed"))
	default:
		parts = append(parts, tr(c, "services.page.summary_idle"))
	}
	if mem := s.currentMemorySummary(); mem.Known {
		parts = append(parts, trf(c, "services.page.summary_memory_budget",
			"used", fmt.Sprintf("%.1f", mem.UsedGB),
			"remaining", fmt.Sprintf("%.1f", mem.RemainingGB),
		))
	} else if snapshot, err := parseDeviceSnapshot(s.device.Snapshot); snapshot != nil && err == nil {
		hw := snapshot.object("hardware")
		total := deviceBytesFor(c, hw["memory_total_bytes"])
		avail := deviceBytesFor(c, hw["memory_available_bytes"])
		missing := tr(c, "device.missing")
		if total != missing && avail != missing {
			parts = append(parts, trf(c, "services.page.summary_memory", "available", avail, "total", total))
		}
	}
	return joinParts(parts)
}

func (s *viewState) managedServiceSection(c *ui.Context) {
	services := s.effectiveManagedServices()
	preparing := s.pendingStandalonePreparations()
	if len(services) > 0 || len(preparing) > 0 {
		multi := len(services)+len(preparing) > 1
		for idx, svc := range services {
			if svc.State == "orphaned" {
				s.renderOrphanedServiceCard(c, svc)
			} else {
				s.renderRunningServiceCard(c, svc, idx, multi)
			}
		}
		for _, prep := range preparing {
			s.renderPreparingServiceCard(c, prep)
		}
		return
	}
	if s.engineState.State == "failed" {
		s.renderFailedManagedCard(c)
		return
	}
	s.renderIdleManagedCard(c)
}

func (s *viewState) renderRunningServiceCard(c *ui.Context, state core.EngineState, idx int, multi bool) {
	t := tokens(c)
	q := s.queueActivity()
	outerR := t.Radius.Surface
	pad := t.Card.Padding
	innerR := concentricRadius(outerR, pad)

	svcKey := serviceKey(state)
	expanded := s.isServiceExpanded(svcKey, idx)
	modelName := s.serviceModelTitle(c, state)
	engineTitle := serviceEngineTitle(state)
	ctxStr := formatContextTokens(state.ContextSize)
	if ctxStr == "" && !multi && s.startup.ContextSize > 0 {
		ctxStr = formatContextTokens(s.startup.ContextSize)
	}

	uiURL, canOpen, _ := serviceWebUIInfo(state)
	localURL, hasLocal := serviceLocalURL(state)
	displayURL, hasEndpoint := serviceEndpointDisplay(state)

	cardEl := applyMaterial(c, ui.Column(c.Key("managed-service-card-"+svcKey)).FillWidth().Padding(pad).Gap(t.Density.Row), materialCard).Radius(outerR)
	cardIntent := cardEl.Hovered() || cardEl.FocusWithin()
	cardEl.Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			toggleRow := ui.Row(c.Key("toggle-" + svcKey)).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).
				Role(ui.RoleButton).Focusable().Label(modelName)
			if toggleRow.Clicked() {
				s.toggleServiceExpanded(svcKey, idx)
			}
			toggleRow.Children(func() {
				chevronBox(c, expanded)
				modelIconBox(c, s.modelIconHint(modelName)+" "+state.Model, 28, innerR)
				ui.Text(c, modelName).FontWeight(600).FontSize(t.Type.Primary).SingleLine()
				if engineTitle != "" {
					enginePillWithHint(c, engineTitle, state.Family+" "+state.Adapter+" "+s.engineIconHint(engineTitle)+" "+state.Executable)
				}
				if !expanded && ctxStr != "" {
					inlineBadge(c, ctxStr, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
				}
				inlineBadge(c, stateLabelFor(contextLocale(c), state.State), engineStateKind(state.State), badgeOptions{Size: sizeSM, Style: badgeDot})
				if !expanded && state.Adopted {
					inlineBadge(c, tr(c, "services.card.badge.adopted"), stateInfo, badgeOptions{Size: sizeSM})
				}
				if !expanded && state.LANAccess && !hasEndpoint {
					inlineBadge(c, tr(c, "services.card.badge.lan"), stateWarning, badgeOptions{Size: sizeSM})
				}
			})
			if hasEndpoint {
				shortAddr := strings.TrimPrefix(strings.TrimPrefix(displayURL, "http://"), "https://")
				pillLabel := shortAddr
				if !expanded && state.LANAccess {
					pillLabel = trf(c, "services.card.lan_listen", "addr", shortAddr)
				}
				copyPill := ui.Row(c).AlignSelf(ui.Center).AlignItems(ui.Center).Gap(t.Density.Hairline).
					Padding(t.Density.Hairline, t.Density.Tight).Radius(innerR).
					Role(ui.RoleButton).Focusable().Label(tr(c, "services.card.copy_endpoint"))
				if copyPill.Hovered() {
					copyPill.Background(t.Color.SurfaceHover)
				}
				if copyPill.Clicked() {
					s.copyServiceEndpoint(c, state)
				}
				copyPill.Children(func() {
					ui.Text(c, pillLabel).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
					studioIcon(c, reicon.Copy, t.Color.FgTertiary)
				})
			}
		})

		if !expanded {
			return
		}

		cols := 3
		if compactLayout(c) {
			cols = 2
		}
		memVal := serviceMemoryDisplay(c, state)
		bestSpeed, _ := s.bestSpeedForService(state)
		execSub := displayName(state.Executable)
		if execSub == engineTitle {
			execSub = ""
		}
		var portCtxParts []string
		if state.Port > 0 {
			portCtxParts = append(portCtxParts, trf(c, "services.recent.port_fixed", "port", fmt.Sprintf("%d", state.Port)))
		}
		if ctxStr != "" {
			portCtxParts = append(portCtxParts, ctxStr)
		}
		portCtxVal := nonEmptyOrMissing(c, joinParts(portCtxParts))
		sourceSub := tr(c, "services.card.source.managed")
		if state.Adopted {
			sourceSub = tr(c, "services.card.source.adopted")
		}
		pidVal := sourceSub
		if state.Pid > 0 {
			pidVal = fmt.Sprintf("PID %d", state.Pid)
		} else {
			sourceSub = ""
		}
		ui.Grid(c).Columns(cols).Gap(t.Density.Section).Children(func() {
			var portSubParts []string
			if bestSpeed != nil {
				var openBest func()
				if bestSpeed.ReportRef != "" {
					ref := bestSpeed.ReportRef
					openBest = func() { s.openReport(ref) }
				}
				serviceModelSpeedTile(c, innerR, tr(c, "reports.throughput.title"), modelName, "", bestSpeed, openBest)
				if state.Pid > 0 {
					portSubParts = append(portSubParts, fmt.Sprintf("PID %d", state.Pid))
				}
				if state.Adopted {
					portSubParts = append(portSubParts, tr(c, "services.card.badge.adopted"))
				}
			} else {
				serviceMetricTile(c, innerR, tr(c, "services.card.metric.status"), pidVal, sourceSub, true)
			}
			if state.LANAccess {
				portSubParts = append(portSubParts, tr(c, "services.card.badge.lan"))
			}
			serviceMetricTile(c, innerR, tr(c, "services.card.metric.memory"), memVal, execSub, true)
			serviceMetricTile(c, innerR, tr(c, "services.card.metric.port_context"), portCtxVal, joinParts(portSubParts), true)
		})

		if state.State == "starting" || state.State == "stopping" {
			progressBar(c, -1, tr(c, "engine.status.transitioning"))
		}

		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			if primaryIconButton(c, reicon.Gauge, tr(c, "services.card.action.benchmark")).Clicked() {
				s.startBenchmarkForManagedService(state)
			}
			secRow := ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row)
			if !cardIntent {
				secRow.Opacity(0)
			}
			secRow.Children(func() {
				if canOpen {
					if outlineIconButton(c, reicon.ExternalLink, tr(c, "services.card.action.open_web")).Clicked() {
						s.openEngineWebUI(c, uiURL)
					}
				}
				if hasLocal {
					usageLabel := tr(c, "services.card.action.usage")
					usageBtn := outlineIconButton(c, reicon.Code, usageLabel)
					if multi {
						usageBtn.Label(usageLabel + " · " + modelName)
					}
					if usageBtn.Clicked() {
						apiModel := strings.TrimSpace(state.ServedModel)
						if apiModel == "" {
							apiModel = modelName
						}
						s.openServiceUsageDrawer(localURL, apiModel, engineTitle)
					}
				}
				if state.Origin == core.EngineOriginStandalone || s.serveSupported || s.preview != nil {
					paramsLabel := tr(c, "services.card.action.params")
					paramsBtn := outlineIconButton(c, reicon.SlidersHorizontal, paramsLabel)
					if multi {
						paramsBtn.Label(paramsLabel + " · " + modelName)
					}
					if paramsBtn.Clicked() {
						s.openServiceParamsDrawerForService(state)
					}
				}
				if state.LogPath != "" {
					logLabel := tr(c, "services.card.action.log")
					logBtn := outlineIconButton(c, reicon.FileText, logLabel)
					if multi {
						logBtn.Label(logLabel + " · " + modelName)
					}
					if logBtn.Clicked() {
						s.openServiceLog(state)
					}
				}
			})
			ui.Box(c).Grow(1)
			stopDisabled := !s.serviceStopEnabled(state, q.active())
			stopLabel := tr(c, "services.card.action.stop")
			stopReason := ""
			if stopDisabled && q.active() {
				stopReason = tr(c, "engine.tooltip.stop_busy_run")
			}
			renderStop := func() ui.Element {
				tooltipLabel := stopLabel
				if stopReason != "" {
					tooltipLabel = ""
				}
				btn := dangerIconButton(c, reicon.Square, tooltipLabel).Label(stopLabel).Disabled(stopDisabled)
				if multi {
					btn.Label(stopLabel + " · " + modelName)
				}
				return btn
			}
			var stopBtn ui.Element
			if stopReason != "" {
				stopBtn = iconButtonWithTooltip(c, stopReason, renderStop)
			} else {
				stopBtn = renderStop()
			}
			if stopBtn.Clicked() {
				if state.ServiceID != "" {
					s.pendingDestructive = s.destructiveRequestFor(destructiveOption{kind: destructiveStopService, id: state.ServiceID})
				} else {
					s.pendingDestructive = s.destructiveRequestFor(destructiveOption{kind: destructiveStopEngine, id: "header"})
				}
			}
		})
	})
}

func (s *viewState) renderOrphanedServiceCard(c *ui.Context, state core.EngineState) {
	t := tokens(c)
	svcKey := serviceKey(state)
	noticeBanner(c, noticeBannerOptions{
		Kind:    stateWarning,
		Icon:    reicon.CircleAlert,
		Title:   tr(c, "services.orphan.title"),
		Message: tr(c, "engine.service.orphan_notice"),
		Details: func() {
			ui.Column(c.Key("orphan-details-" + svcKey)).FillWidth().Gap(t.Density.Tight).Children(func() {
				if state.ModelName != "" {
					keyValueRow(c, tr(c, "services.card.metric.model"), state.ModelName, false)
				} else if state.Candidate != "" {
					keyValueRow(c, tr(c, "engine.service.candidate_label"), displayName(state.Candidate), false)
				}
				if state.Pid > 0 {
					keyValueRow(c, "PID", fmt.Sprintf("%d", state.Pid), true)
				}
				if ep, ok := serviceEndpointDisplay(state); ok {
					keyValueRow(c, tr(c, "engine.service.endpoint_label"), ep, false)
				}
			})
		},
		Actions: func() {
			if state.ServiceID != "" {
				if s.destructiveDialog(c, destructiveOption{
					kind: destructiveStopOrphan, id: state.ServiceID,
					label: tr(c, "engine.orphan.stop"), confirmLabel: tr(c, "engine.orphan.stop_confirm"),
					prompt: tr(c, "engine.orphan.stop_prompt"), disabled: s.busy || s.running() != nil,
				}) {
					s.stopOrphanServiceByID(state.ServiceID)
				}
				if orphanRecordRecoverable(state) {
					if s.destructiveDialog(c, destructiveOption{
						kind: destructiveDiscardOrphan, id: state.ServiceID,
						label: tr(c, "engine.orphan.discard"), confirmLabel: tr(c, "engine.orphan.discard_confirm"),
						prompt: tr(c, "engine.orphan.discard_prompt"), disabled: s.busy || s.running() != nil,
					}) {
						s.discardOrphanServiceByID(state.ServiceID)
					}
				}
				return
			}
			s.stopOrphanButton(c)
		},
	})
}

func (s *viewState) renderFailedManagedCard(c *ui.Context) {
	state := s.engineState
	noticeBanner(c, noticeBannerOptions{
		Kind:  stateDanger,
		Icon:  reicon.CircleAlert,
		Title: tr(c, "services.failed.title"),
		Details: func() {
			errorView(c, state.Error)
		},
		Actions: func() {
			if s.canStartManagedEngine() {
				if primaryIconButton(c, reicon.RotateCw, tr(c, "services.card.action.retry")).Clicked() {
					s.startManagedEngine()
				}
			}
			if state.LogPath != "" {
				if outlineIconButton(c, reicon.FileText, tr(c, "services.card.action.log")).Clicked() {
					s.openEngineCardLog(state.LogPath)
				}
			}
		},
	})
}

func (s *viewState) renderIdleManagedCard(c *ui.Context) {
	t := tokens(c)
	emptyGuideCard(c, emptyGuideOptions{
		Icon:        reicon.Server,
		Title:       tr(c, "services.empty.title"),
		Description: tr(c, "services.empty.desc"),
		Surface:     emptySunken,
		Action: func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if s.canStartManagedEngine() {
					if primaryActionButton(c, tr(c, "services.card.action.start"), s.actionBusy("start-engine")).Disabled(s.busy).Clicked() {
						s.startManagedEngine()
					}
				}
				if button(c, tr(c, "services.empty.go_library")).Clicked() {
					s.navigateTo("library", "")
				}
				if button(c, tr(c, "services.empty.go_benchmarks")).Clicked() {
					s.navigateTo("benchmarks", "")
				}
			})
		},
	})
}
