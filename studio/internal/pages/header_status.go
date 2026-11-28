package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

type queueActivity struct{ running, waiting int }

func (q queueActivity) active() bool { return q.running+q.waiting > 0 }

func (s *viewState) queueActivity() queueActivity {
	q := queueActivity{}
	speedListed := false
	for _, task := range s.tasks {
		if terminal(task.State) {
			continue
		}
		if task.Queued || task.State == "queued" {
			q.waiting++
		} else {
			q.running++
		}
		if s.speed != nil && s.speed.current != nil && task.TaskId == s.speed.current.id {
			speedListed = true
		}
	}
	if !speedListed && s.speed != nil && s.speed.current != nil && s.speed.current.active() {
		if s.speed.current.stage == speedQueued {
			q.waiting++
		} else {
			q.running++
		}
	}
	return q
}

type sidebarStatusSpec struct {
	key       string
	label     string
	ariaLabel string
	badgeText string
	kind      stateKind
	highlight bool
	progress  float64 // < 0 hides mini bar
	popWidth  float32
	leading   func()
	onClick   func()
	open      *bool
	pop       func(close func())
}

func (s *viewState) sidebarStatusArea(c *ui.Context) {
	s.sidebarServiceRow(c)
	s.sidebarQueueRow(c)
	s.sidebarDownloadRow(c)
}

func (s *viewState) shellContentTitle(c *ui.Context, leading func(*ui.Context)) {
	t := tokens(c)
	row := ui.Row(c.Key("content-title-bar")).Fill().AlignItems(ui.Center).Gap(t.Density.Row)
	availWidth := row.LayoutBounds().W
	studioTooltipGroup(c, row, func() {
		if s.shell != nil && !s.shell.LeftExpanded() {
			s.shellLeftHeader(c)
		}
		if leading != nil {
			leading(c)
		}
		ui.Spacer(c)
		s.updateHeaderBadge(c)
		s.notificationBell(c, availWidth)
	})
}

func (s *viewState) sidebarServiceRow(c *ui.Context) {
	t := tokens(c)
	q := s.queueActivity()
	active := s.activeManagedServices()
	exited := s.effectiveExitedNotices()
	state := s.engineState.State
	if state == "" {
		state = "stopped"
	}
	if len(active) > 0 {
		state = active[0].State
	} else if (state == "stopped" || state == "failed") && len(s.pendingStandalonePreparations()) > 0 {
		state = "starting"
	}
	exitedAlert := len(exited) > 0 && len(active) == 0
	kind := engineStateKind(state)
	label := tr(c, "sidebar.status.service_idle")
	highlight := false
	var onClick func()
	if exitedAlert {
		kind = stateDanger
		if len(exited) > 1 {
			label = trf(c, "sidebar.status.services_exited_count", "count", len(exited))
		} else {
			label = tr(c, "sidebar.status.service_exited")
		}
		highlight = true
		onClick = func() { s.navigateTo("services", "") }
	} else if len(active) > 1 {
		kind = stateSuccess
		label = trf(c, "sidebar.status.services_running_count", "count", len(active))
	} else {
		switch state {
		case "running":
			label = tr(c, "sidebar.status.service_running")
		case "starting":
			label = tr(c, "sidebar.status.service_starting")
		case "stopping":
			label = tr(c, "sidebar.status.service_stopping")
		case "orphaned":
			label = tr(c, "sidebar.status.service_orphaned")
			highlight = true
		case "failed":
			label = tr(c, "sidebar.status.service_failed")
			highlight = true
		}
	}
	aria := trf(c, "shell.header.engine_label", "status", stateLabelFor(contextLocale(c), state))
	if exitedAlert || len(active) > 1 {
		aria = label
	}
	animated := !exitedAlert && (len(active) > 0 || state == "running" || state == "starting" || state == "stopping")
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key:       "sidebar-engine-status",
		label:     label,
		ariaLabel: aria,
		kind:      kind,
		highlight: highlight,
		progress:  -1,
		popWidth:  c.Theme().Space(92),
		onClick:   onClick,
		leading: func() {
			cDot := stateColor(c, kind)
			if exitedAlert || state == "failed" || state == "orphaned" {
				components.Icon(c, reicon.CircleAlert).Size(14, 14).TextColor(cDot)
				return
			}
			outer := ui.Box(c).Size(12, 12).Radius(t.Radius.Full).Background(cDot.Alpha(.20)).
				AlignItems(ui.Center).Justify(ui.Center)
			if animated && !c.Preferences().ReduceMotion {
				phase := outer.Loop("sidebar-engine-pulse", 1400*time.Millisecond, ui.Linear)
				outer.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
			}
			outer.Children(func() {
				ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(cDot)
			})
		},
		open: &s.sidebarServiceOpen,
		pop: func(close func()) {
			if len(active) > 1 || (len(active) == 1 && active[0].Origin == core.EngineOriginStandalone) {
				s.sidebarManagedServicesPopover(c, active, q.active(), close)
			} else {
				s.engineStatusPanel(c, q.active(), close)
			}
			button(c, tr(c, "sidebar.status.open_services")).AlignSelf(ui.Start).OnClick(func() {
				s.navigateTo("services", "")
				close()
			})
		},
	})
}

func (s *viewState) sidebarManagedServicesPopover(c *ui.Context, active []core.EngineState, runActive bool, close func()) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
		heading(c, trf(c, "sidebar.popover.title", "count", len(active)), 3)
		for _, svc := range active {
			key := serviceKey(svc)
			modelName := s.serviceModelTitle(c, svc)
			uiURL, canOpen, _ := serviceWebUIInfo(svc)
			_, hasEndpoint := serviceEndpointDisplay(svc)
			portText := ""
			if svc.Port > 0 {
				portText = fmt.Sprintf(":%d", svc.Port)
			}
			applyMaterial(c, ui.Column(c.Key("sidebar-svc-"+key)).FillWidth().Padding(t.Density.Row).Gap(t.Density.Tight), materialSunken).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Text(c, modelName).Bold().FontSize(t.Type.Caption).Grow(1).MinWidth(0).SingleLine()
					if svc.LANAccess {
						inlineBadge(c, tr(c, "services.card.badge.lan"), stateWarning, badgeOptions{Size: sizeSM})
					}
					if portText != "" {
						ui.Text(c, portText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
					}
				})
				studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight), func() {
					if canOpen {
						if outlineIconButton(c, reicon.ExternalLink, tr(c, "sidebar.popover.web")).
							Label(tr(c, "sidebar.popover.web") + " · " + modelName).Clicked() {
							s.openEngineWebUI(c, uiURL)
						}
					}
					if hasEndpoint {
						if outlineIconButton(c, reicon.Copy, tr(c, "sidebar.popover.copy")).
							Label(tr(c, "sidebar.popover.copy") + " · " + modelName).Clicked() {
							s.copyServiceEndpoint(c, svc)
						}
					}
					ui.Box(c).Grow(1)
					stopDisabled := !s.serviceStopEnabled(svc, runActive)
					if dangerIconButton(c, reicon.Square, tr(c, "sidebar.popover.stop")).
						Label(tr(c, "sidebar.popover.stop") + " · " + modelName).
						Disabled(stopDisabled).Clicked() {
						if svc.ServiceID != "" {
							s.pendingDestructive = s.destructiveRequestFor(destructiveOption{kind: destructiveStopService, id: svc.ServiceID})
						} else {
							s.pendingDestructive = s.destructiveRequestFor(destructiveOption{kind: destructiveStopEngine, id: "header"})
						}
						close()
					}
				})
			})
		}
	})
}

func (s *viewState) sidebarQueueRow(c *ui.Context) {
	q := s.queueActivity()
	if !q.active() && !s.busy {
		return
	}
	label := tr(c, "sidebar.status.queue_preparing")
	if q.active() {
		if q.waiting > 0 {
			label = trf(c, "sidebar.status.queue_running_waiting", "running", q.running, "waiting", q.waiting)
		} else {
			label = trf(c, "sidebar.status.queue_running", "running", q.running)
		}
	}
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key:       "sidebar-queue-status",
		label:     label,
		ariaLabel: label,
		kind:      stateActive,
		progress:  -1,
		leading: func() {
			components.Icon(c, reicon.Activity).Size(14, 14).TextColor(stateColor(c, stateActive))
		},
		open: &s.sidebarQueueOpen,
		pop: func(close func()) {
			t := tokens(c)
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, tr(c, "shell.header.task_queue"), 3).Grow(1)
					inlineBadge(c, label, stateActive, badgeOptions{Size: sizeSM})
				})
				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					for _, task := range s.tasks {
						if !terminal(task.State) {
							name := tr(c, "shell.header.context_speed")
							if task.Bundle != nil {
								name = *task.Bundle
							}
							dotKind := stateActive
							if task.Queued || task.State == "queued" {
								dotKind = stateNeutral
							}
							applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
								Padding(t.Density.Inset, t.Density.Row), materialSunken).Children(func() {
								ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Shrink(0).Background(stateColor(c, dotKind))
								ui.Text(c, name+" · "+taskLabelFor(contextLocale(c), task)).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).SingleLine()
							})
						}
					}
				})
				outlineButton(c, tr(c, "sidebar.status.open_benchmarks")).AlignSelf(ui.Start).OnClick(func() {
					s.navigateTo("benchmarks", "")
					close()
				})
			})
		},
	})
}

func (s *viewState) sidebarDownloadRow(c *ui.Context) {
	n := s.activeDownloadCount()
	if n <= 0 {
		return
	}
	label := trf(c, "sidebar.status.downloads_active", "count", n)
	pct := s.activeDownloadProgress()
	badgeText := ""
	if pct >= 0 {
		badgeText = fmt.Sprintf("%d%%", int(pct*100))
	}
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key:       "sidebar-download-status",
		label:     label,
		ariaLabel: label,
		badgeText: badgeText,
		kind:      stateActive,
		progress:  pct,
		leading: func() {
			components.Icon(c, reicon.Download).Size(14, 14).TextColor(stateColor(c, stateActive))
		},
		open: &s.sidebarDownloadOpen,
		pop: func(close func()) {
			ui.Text(c, label)
			button(c, tr(c, "sidebar.status.open_library")).AlignSelf(ui.Start).OnClick(func() {
				s.navigateTo("library", "")
				close()
			})
		},
	})
}

func (s *viewState) activeDownloadProgress() float64 {
	var total, done int64
	hasBytes := false
	for _, task := range s.downloads {
		if task.State != "downloading" {
			continue
		}
		if task.TotalBytes != nil && *task.TotalBytes > 0 {
			hasBytes = true
			total += *task.TotalBytes
			done += task.DownloadedBytes
		}
	}
	if !hasBytes || total <= 0 {
		return -1
	}
	ratio := float64(done) / float64(total)
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

func (s *viewState) sidebarStatusRow(c *ui.Context, sp sidebarStatusSpec) {
	t := tokens(c)
	scale := stateScale(c, sp.kind)
	aria := sp.ariaLabel
	if aria == "" {
		aria = sp.label
	}
	anchor := ui.Column(c.Key(sp.key)).FillWidth().Gap(4).
		Padding(t.Density.Tight+1, t.Density.Row).Radius(t.Radius.Control).
		Role(ui.RoleButton).Focusable().Label(aria)
	if sp.highlight {
		anchor.Background(scale.SubtleBg)
	} else if anchor.Hovered() || *sp.open {
		anchor.Background(t.Color.SurfaceHover)
	}
	if anchor.Clicked() {
		if sp.onClick != nil {
			sp.onClick()
		} else {
			*sp.open = !*sp.open
		}
	}
	anchor.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			sidebarIconSlot(c, sp.leading)
			txtColor := t.Color.FgSecondary
			if sp.highlight {
				txtColor = scale.Fg
			} else if anchor.Hovered() || *sp.open {
				txtColor = t.Color.FgPrimary
			}
			txt := ui.Text(c, sp.label).FontSize(t.Type.Caption).TextColor(txtColor).SingleLine().Grow(1)
			if sp.highlight {
				txt.Bold()
			}
			if sp.badgeText != "" {
				valColor := t.Color.FgSecondary
				if sp.highlight {
					valColor = scale.Fg
				}
				ui.Text(c, sp.badgeText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(valColor).SingleLine()
			}
		})
		if sp.progress >= 0 {
			ui.Box(c).FillWidth().Padding(0, 0, 0, 24).Children(func() {
				progressBar(c, sp.progress, sp.label).Height(2)
			})
		}
	})
	studioPopover(c, anchor, sp.open, func(panel ui.Element) {
		width := sp.popWidth
		if width <= 0 {
			width = c.Theme().Space(78)
		}
		panel.AttachTo(anchor, ui.AnchorTopRight, ui.AnchorBottomLeft).
			Margin(0, 0, 0, t.Density.Stack).
			Width(width).MaxWidthPercent(100).
			Label(trf(c, "shell.header.status_panel_label", "label", aria))
		panel.Children(func() { sp.pop(func() { *sp.open = false }) })
	})
}

func (s *viewState) closeEngineStatusPanel() {
	if destructiveKind(s.pendingDestructive.kind) == destructiveStopEngine {
		s.pendingDestructive = destructiveRequest{}
	}
}

func engineStatusText(state string) string {
	return engineStatusTextFor("", state)
}

func engineStatusTextFor(locale, state string) string {
	switch state {
	case "running":
		return l10n.T(locale, "common.state.running")
	case "starting":
		return l10n.T(locale, "common.state.starting")
	case "stopping":
		return l10n.T(locale, "common.state.stopping")
	case "failed":
		return l10n.T(locale, "common.state.failed")
	case "orphaned":
		return l10n.T(locale, "shell.header.engine_orphaned")
	default:
		return l10n.T(locale, "common.state.stopped")
	}
}
