package pages

import (
	"math"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) polystrataInstallHeaderAction(c *ui.Context, task core.DownloadTask) {
	if s.destructiveActionWithTrigger(c, destructiveOption{
		kind:         destructiveCancelDownload,
		id:           task.TaskId,
		label:        tr(c, "downloads.polystrata.cancel_install"),
		confirmLabel: tr(c, "downloads.polystrata.cancel_install_confirm"),
		dismissLabel: tr(c, "downloads.polystrata.resume_install"),
		prompt:       tr(c, "downloads.polystrata.cancel_install_prompt"),
		disabled:     s.busy,
	}, true, func(open bool) ui.Element {
		return polystrataInstallStatusButton(c, open, s.busy)
	}) {
		if s.simulate(previewCancelDownload{id: task.TaskId}) {
			return
		}
		s.workDownload(func() (string, *core.ApiError) {
			_, e := s.service.CancelDownload(task.TaskId)
			return l10n.TR("downloads.polystrata.toast.cancel_requested"), e
		})
	}
}

func polystrataInstallStatusButton(c *ui.Context, confirmOpen, disabled bool) ui.Element {
	t := tokens(c)
	theme := c.Theme()
	b := ui.ButtonBase(c).MinWidth(100).MinHeight(t.Control.SM).Padding(0, theme.Space(3), 0, theme.Space(2.25)).Radius(t.Radius.Full).Label(tr(c, "downloads.polystrata.cancel_install"))
	b.Disabled(disabled)

	showCancel := !disabled && (b.Hovered() || b.Pressed() || confirmOpen)
	var bg, border, fg ui.Color
	borderWidth := float32(1)
	if c.Preferences().HighContrast {
		borderWidth = 1.5
	}

	if showCancel {
		danger := t.Color.Danger
		fg = danger.Fg
		border = danger.Border
		bg = danger.SubtleBg
		if b.Pressed() {
			bg = t.Color.Surface.Mix(danger.Solid, .20)
			border = danger.Solid
		}
		if c.Preferences().HighContrast {
			bg = t.Color.Surface
			border = danger.Solid
		}
	} else {
		active := t.Color.Active
		pulse := float32(0.5)
		if !disabled && !c.Preferences().ReduceMotion {
			phase := b.Loop("polystrata-install-pulse", 1800*time.Millisecond, ui.Linear)
			pulse = float32((math.Sin(float64(phase)*2*math.Pi) + 1) / 2)
		}
		fg = active.Fg
		bg = t.Color.Surface.Mix(active.Solid, 0.08+0.07*pulse)
		border = active.Solid.Alpha(0.20 + 0.24*pulse)
		if disabled {
			fg = active.Fg.Alpha(.65)
			bg = active.SubtleBg
			border = active.Border
		} else if c.Preferences().HighContrast {
			bg = t.Color.Surface
			border = active.Solid
		}
	}

	b.Background(bg).Border(borderWidth, border).TextColor(fg)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(1.5)).Children(func() {
			if showCancel {
				components.Icon(c, lucide.X).Size(12, 12).TextColor(fg).Role(ui.RoleNone)
				ui.Text(c, tr(c, "downloads.polystrata.cancel_install")).Bold().FontSize(t.Type.Caption).TextColor(fg).SingleLine()
			} else {
				components.Spinner(c, components.SpinnerRing).Size(12, 12).TextColor(fg).Label(tr(c, "downloads.polystrata.installing_label"))
				ui.Text(c, tr(c, "downloads.badge.installing")).Bold().FontSize(t.Type.Caption).TextColor(fg).SingleLine()
			}
		})
	})
	if !confirmOpen && !disabled {
		components.Tooltip(c, b, tr(c, "downloads.polystrata.click_cancel_tooltip"))
	}
	return b
}

func (s *viewState) polystrataInstallLogSection(c *ui.Context, taskID, logPath string) {
	t := tokens(c)
	container := ui.Column(c.Key("polystrata-live-log-" + taskID)).FillWidth().Gap(t.Density.Tight)
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() {
		labelText := tr(c, "downloads.polystrata.log.view")
		if *open {
			labelText = tr(c, "downloads.polystrata.log.hide")
		}
		toggle := ui.ButtonBase(c).AlignSelf(ui.Start).
			Padding(t.Density.Hairline, t.Density.Tight).
			Radius(t.Radius.XS).
			Label(tr(c, "downloads.polystrata.log.view"))
		fg := t.Color.FgTertiary
		if *open || toggle.Hovered() {
			fg = t.Color.FgSecondary
		}
		if toggle.Hovered() {
			toggle.Background(t.Color.SurfaceHover)
		}
		toggle.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				chevron := lucide.ChevronRight
				if *open {
					chevron = lucide.ChevronDown
				}
				components.Icon(c, chevron).Size(12, 12).TextColor(fg).Role(ui.RoleNone)
				ui.Text(c, labelText).FontSize(t.Type.Caption).TextColor(fg).SingleLine()
			})
		})
		if toggle.Clicked() {
			*open = !*open
		}
		if !*open {
			return
		}
		snippet := s.readDownloadLogSnippet(taskID, logPath)
		ui.Column(c).FillWidth().Gap(t.Density.Tight).
			Padding(t.Density.Row, t.Density.Stack).
			Radius(t.Radius.SM).
			Background(t.Color.Sunken).
			Border(1, t.Color.BorderSubtle).
			Children(func() {
				if snippet == "" {
					ui.Text(c, tr(c, "downloads.polystrata.log.empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
					return
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, filepath.Base(logPath)).FontSize(max(11, t.Type.Caption-1)).TextColor(t.Color.FgTertiary).SingleLine()
					copyBtn := ui.ButtonBase(c).Padding(t.Density.Hairline, t.Density.Tight).Radius(t.Radius.XS).Label(tr(c, "downloads.polystrata.log.copy_label"))
					copyFg := t.Color.FgTertiary
					if copyBtn.Hovered() {
						copyFg = t.Color.FgSecondary
						copyBtn.Background(t.Color.SurfaceHover)
					}
					copyBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
							components.Icon(c, lucide.Copy).Size(11, 11).TextColor(copyFg).Role(ui.RoleNone)
							ui.Text(c, tr(c, "downloads.action.copy")).FontSize(max(11, t.Type.Caption-1)).TextColor(copyFg).SingleLine()
						})
					})
					if copyBtn.Clicked() {
						s.copyDownloadText(c, tr(c, "downloads.copy_subject.install_log"), snippet)
					}
				})
				ui.Scroll(c).FillWidth().MaxHeight(168).Children(func() {
					ui.Text(c, snippet).Selectable().
						FontSize(max(11, t.Type.Caption-0.5)).
						FontFeatures("tnum").
						TextColor(t.Color.FgSecondary)
				})
			})
	})
}

func (s *viewState) readDownloadLogSnippet(taskID, logPath string) string {
	if s.preview != nil {
		return s.preview.downloadLogTail(taskID)
	}
	return readLogTail(logPath, 4096)
}
