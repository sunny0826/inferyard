package pages

import (
	"github.com/egoist/mygo/ui"
)

// destructiveKind names an action that discards in-flight work. The same
// builder serves the production window and the preview, so a confirmation is
// never an unmodeled operation: the preview only changes window memory after
// the confirmed action dispatches its usual preview intent.
type destructiveKind string

const (
	destructiveNewTask        destructiveKind = "new_task"
	destructiveCancelRun      destructiveKind = "cancel_run"
	destructiveStopEngine     destructiveKind = "stop_engine"
	destructiveStopOrphan     destructiveKind = "stop_orphan"
	destructiveCancelDownload destructiveKind = "cancel_download"
)

type destructiveRequest struct {
	kind, id string
}

type destructiveOption struct {
	kind         destructiveKind
	id           string
	label        string
	confirmLabel string
	prompt       string
	disabled     bool
	compact      bool
}

// destructiveAction renders a danger-styled action and asks for one more
// click before it reports true. The first click only opens the inline
// confirmation; the prompt states what the action cancels and what stays.
func (s *viewState) destructiveAction(c *ui.Context, option destructiveOption) bool {
	t := tokens(c)
	request := destructiveRequest{kind: string(option.kind), id: option.id}
	pending := s.pendingDestructive == request
	if !pending {
		if dangerButton(c, option.label).AlignSelf(ui.Start).Disabled(option.disabled).Clicked() {
			s.pendingDestructive = request
		}
		return false
	}
	confirmed, cancelled := false, false
	ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Inset).Background(c.Theme().SurfaceHover).Radius(t.Radius.Inset).Children(func() {
		prompt := ui.Text(c, option.prompt)
		if option.compact {
			prompt.FontSize(t.Type.Caption)
		}
		ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
			if primaryButton(c, option.confirmLabel).AlignSelf(ui.Start).Clicked() {
				confirmed = true
			}
			if button(c, "返回").AlignSelf(ui.Start).Clicked() {
				cancelled = true
			}
		})
	})
	if confirmed || cancelled {
		s.pendingDestructive = destructiveRequest{}
	}
	return confirmed
}

// workInFlight reports whether starting a new task would cancel work the user
// has not finished. Idle preparation screens keep the single-click entry.
func (s *viewState) workInFlight() bool {
	if s.running() != nil || s.busy || s.newTaskPending {
		return true
	}
	if s.speed != nil && s.speed.current != nil && s.speed.current.active() {
		return true
	}
	switch s.task.stage {
	case "", "prepare_candidate", "done":
		return false
	default:
		return true
	}
}
