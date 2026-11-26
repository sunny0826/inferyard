package pages

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type engineHeaderActionOptions struct {
	statusText  string
	actionLabel string
	kind        stateKind
	action      string
	busy        bool
	disabled    bool
	tooltip     string
}

func enginePillSolidFill(dark, pressed bool, kind stateKind) ui.Color {
	switch kind {
	case stateDanger:
		if dark {
			if pressed {
				return ui.Hex("#b02c3b")
			}
			return ui.Hex("#c93647")
		}
		if pressed {
			return ui.Hex("#9f1c2c")
		}
		return ui.Hex("#be2436")
	case stateActive:
		if dark {
			return ui.Hex("#2e6db0")
		}
		return ui.Hex("#255b98")
	default:
		if dark {
			if pressed {
				return ui.Hex("#197449")
			}
			return ui.Hex("#208a58")
		}
		if pressed {
			return ui.Hex("#0f5434")
		}
		return ui.Hex("#16794b")
	}
}

func engineHeaderActionButton(c *ui.Context, opts engineHeaderActionOptions) ui.Element {
	t := tokens(c)
	theme := c.Theme()
	scale := stateScale(c, opts.kind)
	b := ui.ButtonBase(c).MinWidth(86).MinHeight(t.Control.SM).Padding(0, theme.Space(3)).Radius(t.Radius.Full).Label(opts.actionLabel)
	b.Disabled(opts.disabled)

	showSpinner := opts.busy
	if opts.action == "start" {
		showSpinner = delayedVisible(c, b, "engine-start-spinner", opts.busy)
	}

	labelText := opts.statusText
	hovered := !b.IsDisabled() && (b.Hovered() || b.Pressed())
	if hovered {
		if opts.action == "stop" {
			labelText = tr(c, "engine.action.stop_short")
		} else if opts.kind == stateDanger {
			labelText = tr(c, "engine.action.retry_short")
		} else {
			labelText = tr(c, "engine.action.start_short")
		}
	}

	var bg, border, fg, iconFg ui.Color
	borderWidth := float32(1)
	switch {
	case c.Preferences().HighContrast:
		borderWidth = 1.5
		if b.IsDisabled() {
			bg, border, fg = t.Color.Sunken, t.Color.BorderSubtle, t.Color.FgDisabled
			iconFg = fg
		} else {
			bg, border, fg = t.Color.Surface, scale.Border, t.Color.FgPrimary
			iconFg = fg
			if b.Pressed() {
				bg = t.Color.SurfacePressed
			} else if b.Hovered() {
				bg = t.Color.SurfaceHover
			}
			if hovered {
				if opts.action == "stop" {
					border = t.Color.Danger.Solid
				} else {
					border = t.Color.Success.Solid
				}
			}
		}
	case opts.action == "stop":
		if b.IsDisabled() {
			bg, border, fg = scale.SubtleBg, scale.Solid.Alpha(.16), scale.Fg.Alpha(.58)
			iconFg = scale.Solid.Alpha(.55)
		} else {
			fg = ui.Hex("#ffffff")
			iconFg = fg
			fillKind := opts.kind
			if hovered {
				fillKind = stateDanger
			}
			bg = enginePillSolidFill(c.Theme().Dark, b.Pressed(), fillKind)
			border = bg
			b.Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, .14))
		}
	default: // "start"
		if b.IsDisabled() {
			bg, border, fg = t.Color.Sunken, t.Color.BorderSubtle, t.Color.FgDisabled
			iconFg = fg
		} else if hovered {
			fg = ui.Hex("#ffffff")
			iconFg = fg
			bg = enginePillSolidFill(c.Theme().Dark, b.Pressed(), stateSuccess)
			border = bg
			b.Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, .14))
		} else if opts.kind == stateDanger {
			bg, border, fg = scale.SubtleBg, scale.Solid.Alpha(.34), scale.Fg
			iconFg = fg
			b.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .06))
		} else {
			bg, border, fg = t.Color.Surface, t.Color.BorderStrong, t.Color.FgPrimary
			iconFg = t.Color.Success.Fg
			b.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .07))
		}
	}

	b.Background(bg).Border(borderWidth, border).TextColor(fg)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(1.5)).Children(func() {
			switch {
			case showSpinner:
				components.Spinner(c, components.SpinnerRing).Size(12, 12).TextColor(iconFg).Label(trf(c, "engine.action.in_progress", "action", opts.actionLabel))
			case opts.action == "stop" && b.IsDisabled():
				ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(iconFg)
			case opts.action == "stop":
				ui.Box(c).Size(8, 8).Radius(2).Background(iconFg)
			default:
				components.Icon(c, lucide.Play).Size(11, 11).TextColor(iconFg)
			}
			ui.Text(c, labelText).Bold().FontSize(t.Type.Caption).TextColor(fg).SingleLine()
		})
	})
	if opts.tooltip != "" {
		components.Tooltip(c, b, opts.tooltip)
	}
	return b
}

func (s *viewState) openEngineWebUI(c *ui.Context, rawURL string) {
	u, ok := validLoopbackURL(rawURL)
	if !ok {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "engine.toast.webui_opened_preview"))
		return
	}
	c.OpenURLThen(u, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("engine.toast.webui_open_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("engine.toast.webui_opened"))
	})
}

func (s *viewState) copyEngineEndpoint(c *ui.Context, rawURL string) {
	u, ok := validLoopbackURL(rawURL)
	if !ok {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "engine.toast.endpoint_copied_preview"))
		return
	}
	c.WriteClipboardThen(u, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("engine.toast.endpoint_copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("engine.toast.endpoint_copied"))
	})
}

func (s *viewState) openEngineCardLog(logPath string) {
	if s.preview != nil {
		s.enqueueStatusToast(l10n.TR("engine.toast.no_log_preview"))
		return
	}
	target := logPath
	if s.service != nil && !filepath.IsAbs(target) {
		if resolved, err := s.service.ResolveEngineLogPath(target); err == nil {
			target = resolved
		}
	}
	if err := openLogPathFunc(target); err != nil {
		s.failure = &core.ApiError{Code: "io_error", Message: l10n.TRf("engine.error.open_log_failed", "error", err.Error())}
		s.failurePage = s.selected
	}
}

func (s *viewState) managedEngineCandidate() string {
	if s.task.start.candidate != "" {
		return s.task.start.candidate
	}
	return s.candidate
}

func (s *viewState) canStartManagedEngine() bool {
	state := s.engineState.State
	return (state == "" || state == "stopped" || state == "failed") &&
		!s.busy && !s.newTaskPending && !s.queueActivity().active() && s.managedEngineCandidate() != ""
}

func (s *viewState) startManagedEngine() {
	if !s.canStartManagedEngine() {
		return
	}
	candidate := s.managedEngineCandidate()
	if s.simulate(previewManagedEngine{candidate: candidate}) {
		return
	}
	const toastID = "start-managed-engine"
	s.engineStartToastID = toastID
	s.enqueueProgressToast(toastID, l10n.TR("engine.toast.starting"))
	s.guideWorkAs("start-engine", func() (string, *core.ApiError) {
		state, e := s.service.StartEngine(candidate)
		if e != nil {
			s.engineStartToastID = ""
			s.dismissToast(toastID)
			return "", e
		}
		if state.State == "running" {
			s.engineStartToastID = ""
			s.resolveProgressToast(toastID, l10n.TR("engine.toast.ready"), "", nil)
		}
		return "", nil
	})
}
