package pages

import (
	"math"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
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
	bp := paletteFor(dark).buttons
	switch kind {
	case stateDanger:
		if pressed && bp.dangerSolidPressed.A > 0 {
			return bp.dangerSolidPressed
		}
		if !pressed && bp.dangerSolid.A > 0 {
			return bp.dangerSolid
		}
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
		if pressed && bp.engineRunningPressed.A > 0 {
			return bp.engineRunningPressed
		}
		if !pressed && bp.engineRunning.A > 0 {
			return bp.engineRunning
		}
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
	return withDisabledTooltip(c, opts.tooltip, func() ui.Element {
		t := tokens(c)
		theme := c.Theme()
		scale := stateScale(c, opts.kind)
		actionText := tr(c, "engine.action.start_short")
		if opts.action == "stop" {
			actionText = tr(c, "engine.action.stop_short")
		} else if opts.kind == stateDanger {
			actionText = tr(c, "engine.action.retry_short")
		}
		wStatus, _ := c.MeasureText(0, ui.Span{Text: opts.statusText, Size: t.Type.Caption, Weight: 700})
		wAction, _ := c.MeasureText(0, ui.Span{Text: actionText, Size: t.Type.Caption, Weight: 700})
		pillMinW := max(float32(88), float32(math.Ceil(float64(max(wStatus, wAction)+12+theme.Space(2)+theme.Space(6)+4))))
		b := ui.ButtonBase(c).MinWidth(pillMinW).MinHeight(t.Control.SM).Padding(0, theme.Space(3), 0, theme.Space(3)).Radius(t.Radius.Full).Label(opts.actionLabel)
		b.Disabled(opts.disabled)

		showSpinner := opts.busy
		if opts.action == "start" {
			showSpinner = delayedVisible(c, b, "engine-start-spinner", opts.busy)
		}

		labelText := opts.statusText
		hovered := !b.IsDisabled() && (b.Hovered() || b.Pressed() || b.FocusVisible())
		if hovered {
			labelText = actionText
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
			} else if opts.kind == stateDanger {
				bg, border, fg = scale.SubtleBg, scale.Solid.Alpha(.34), scale.Fg
				iconFg = fg
			} else {
				bg, border, fg = t.Color.Surface, t.Color.BorderStrong, t.Color.FgPrimary
				iconFg = t.Color.Success.Fg
			}
		}

		b.Background(bg).Border(borderWidth, border).TextColor(fg).NoShadow()
		b.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(2)).Children(func() {
				switch {
				case showSpinner:
					components.Spinner(c, components.SpinnerRing).Size(12, 12).TextColor(iconFg).Label(trf(c, "engine.action.in_progress", "action", opts.actionLabel))
				case opts.action == "stop" && b.IsDisabled():
					ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(iconFg)
				case opts.action == "stop":
					ui.Box(c).Size(8, 8).Radius(2).Background(iconFg)
				default:
					studioIcon(c, reicon.Play, iconFg)
				}
				ui.Text(c, labelText).Bold().FontSize(t.Type.Caption).TextColor(fg).SingleLine()
			})
		})
		return b
	})
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
