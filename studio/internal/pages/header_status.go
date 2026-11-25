package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
	t := tokens(c)
	s.sidebarServiceRow(c)
	s.sidebarQueueRow(c)
	s.sidebarDownloadRow(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Padding(2, t.Density.Row).Children(func() {
		s.notificationBell(c)
		s.updateHeaderBadge(c)
	})
}

func (s *viewState) sidebarServiceRow(c *ui.Context) {
	t := tokens(c)
	q := s.queueActivity()
	state := s.engineState.State
	if state == "" {
		state = "stopped"
	}
	exitedAlert := s.hasActiveExitNotice() && state != "running" && state != "starting" && state != "stopping"
	kind := engineStateKind(state)
	label := tr(c, "sidebar.status.service_idle")
	highlight := false
	var onClick func()
	if exitedAlert {
		kind = stateDanger
		label = tr(c, "sidebar.status.service_exited")
		highlight = true
		onClick = func() { s.navigateTo("services", "") }
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
	if exitedAlert {
		aria = label
	}
	animated := !exitedAlert && (state == "running" || state == "starting" || state == "stopping")
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key:       "sidebar-engine-status",
		label:     label,
		ariaLabel: aria,
		kind:      kind,
		highlight: highlight,
		progress:  -1,
		popWidth:  c.Theme().Space(88),
		onClick:   onClick,
		leading: func() {
			cDot := stateColor(c, kind)
			if exitedAlert || state == "failed" || state == "orphaned" {
				components.Icon(c, lucide.CircleAlert).Size(14, 14).TextColor(cDot)
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
			s.engineStatusPanel(c, q.active(), close)
			button(c, tr(c, "sidebar.status.open_services")).AlignSelf(ui.Start).OnClick(func() {
				s.navigateTo("services", "")
				close()
			})
		},
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
			components.Icon(c, lucide.Activity).Size(14, 14).TextColor(stateColor(c, stateActive))
		},
		open: &s.sidebarQueueOpen,
		pop: func(close func()) {
			heading(c, tr(c, "shell.header.task_queue"), 3)
			ui.Text(c, label)
			for _, task := range s.tasks {
				if !terminal(task.State) {
					name := tr(c, "shell.header.context_speed")
					if task.Bundle != nil {
						name = *task.Bundle
					}
					ui.Text(c, name+" · "+taskLabelFor(contextLocale(c), task))
				}
			}
			button(c, tr(c, "sidebar.status.open_benchmarks")).AlignSelf(ui.Start).OnClick(func() {
				s.navigateTo("benchmarks", "")
				close()
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
			components.Icon(c, lucide.Download).Size(14, 14).TextColor(stateColor(c, stateActive))
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
		Padding(t.Density.Tight+1, t.Density.Row).Radius(t.Radius.Control).Label(aria)
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
