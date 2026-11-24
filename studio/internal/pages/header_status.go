package pages

import (
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

func (s *viewState) cornerStatus(c *ui.Context) {
	t := tokens(c)
	q := s.queueActivity()
	ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row).Padding(0, t.Density.Page).Children(func() {
		state := s.engineState.State
		if state == "" {
			state = "stopped"
		}
		color, animated := stateColor(c, stateNeutral), false
		switch state {
		case "running":
			color, animated = stateColor(c, stateSuccess), true
		case "starting", "stopping":
			color, animated = stateColor(c, stateActive), true
		case "failed":
			color = stateColor(c, stateDanger)
		case "orphaned":
			color = stateColor(c, stateWarning)
		}
		statusPanelParts(c, statusPanelOptions{
			key:      "engine-status",
			label:    trf(c, "shell.header.engine_label", "status", stateLabelFor(contextLocale(c), state)),
			visible:  trf(c, "shell.header.engine_label", "status", engineStatusTextFor(contextLocale(c), state)),
			icon:     lucide.Server,
			color:    color,
			animated: animated,
			width:    c.Theme().Space(88),
		}, func(close func()) {
			s.engineStatusPanel(c, q.active(), close)
		})
		if s.destructiveDialogModal(c, destructiveOption{
			kind: destructiveStopEngine, id: "header", label: tr(c, "shell.header.stop_engine.label"), confirmLabel: tr(c, "shell.header.stop_engine.confirm"),
			prompt:   tr(c, "common.confirm.stop_engine.prompt"),
			disabled: !s.stopEnabled(true, q.active()),
		}) {
			s.stopEngine()
		}
		label := tr(c, "shell.header.tasks_idle")
		if q.active() {
			label = trf(c, "shell.header.tasks_active", "running", q.running, "waiting", q.waiting)
		} else if s.busy {
			label = tr(c, "shell.header.tasks_preparing")
		}
		queueColor := stateColor(c, stateNeutral)
		if q.active() || s.busy {
			queueColor = stateColor(c, stateActive)
		}
		statusPanelParts(c, statusPanelOptions{key: "queue-status", label: label, visible: label, icon: lucide.ListTodo, color: queueColor, animated: q.active() || s.busy}, func(close func()) {
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
			button(c, tr(c, "shell.header.view_current_tasks")).AlignSelf(ui.Start).OnClick(func() {
				s.navigateTo("dashboard", "")
				close()
			})
		})
		if n := s.activeDownloadCount(); n > 0 {
			label := trf(c, "shell.header.downloading_count", "count", n)
			statusPanelParts(c, statusPanelOptions{key: "download-status", label: label, visible: label, icon: lucide.Download, color: stateColor(c, stateActive), animated: true}, func(close func()) {
				ui.Text(c, label)
				button(c, tr(c, "shell.header.open_downloads")).AlignSelf(ui.Start).OnClick(func() {
					s.navigateTo("downloads", "")
					close()
				})
			})
		}
		s.updateHeaderBadge(c)
		s.notificationBell(c)
	})
}

type statusPanelState struct {
	open, wasOpen bool
}

func (s *viewState) closeEngineStatusPanel() {
	if destructiveKind(s.pendingDestructive.kind) == destructiveStopEngine {
		s.pendingDestructive = destructiveRequest{}
	}
}

type statusPanelOptions struct {
	key, label, visible string
	icon                func() *ui.SVG
	color               ui.Color
	animated            bool
	width               float32
	onClose             func()
}

func statusPanelParts(c *ui.Context, o statusPanelOptions, content func(close func())) {
	c.Key(o.key)
	ui.Column(c).Children(func() {
		theme := c.Theme()
		t := tokens(c)
		anchor := components.Button(c, "", components.ButtonGhost).
			Padding(t.Density.Tight, t.Density.Row).Radius(t.Radius.Full).
			Label(o.label)
		local := ui.Local(anchor, "status-panel", func() statusPanelState { return statusPanelState{} })
		anchor.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				el := components.Icon(c, o.icon).Size(theme.Space(4.5), theme.Space(4.5)).TextColor(o.color)
				if o.animated && !c.Preferences().ReduceMotion {
					phase := el.Loop("status-activity", 1400*time.Millisecond, ui.Linear)
					el.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
				}
				ui.Text(c, o.visible).FontSize(t.Type.Caption).SingleLine().TextColor(o.color)
			})
		})
		if anchor.Clicked() {
			local.open = !local.open
		}
		studioPopover(c, anchor, &local.open, func(panel ui.Element) {
			width := o.width
			if width <= 0 {
				width = theme.Space(78)
			}
			panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).
				Width(width).MaxWidthPercent(100).Label(trf(c, "shell.header.status_panel_label", "label", o.label))
			panel.Children(func() { content(func() { local.open = false }) })
		})
		if local.wasOpen && !local.open && o.onClose != nil {
			o.onClose()
		}
		local.wasOpen = local.open
	})
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
