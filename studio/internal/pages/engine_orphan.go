package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) stopOrphanButton(c *ui.Context) {
	if s.destructiveDialog(c, destructiveOption{
		kind: destructiveStopOrphan, label: tr(c, "engine.orphan.stop"), confirmLabel: tr(c, "engine.orphan.stop_confirm"),
		prompt:   tr(c, "engine.orphan.stop_prompt"),
		disabled: s.busy || s.running() != nil,
	}) {
		if s.simulate(previewStopEngine) {
			return
		}
		s.work(func() (string, *core.ApiError) {
			state, e := s.service.StopOrphanEngine()
			if state.State == "stopped" {
				return l10n.TR("engine.orphan.stopped_cleared"), e
			}
			return l10n.TR("engine.orphan.stop_requested"), e
		})
	}
	if orphanRecordRecoverable(s.engineState) {
		if s.destructiveDialog(c, destructiveOption{
			kind: destructiveDiscardOrphan, label: tr(c, "engine.orphan.discard"), confirmLabel: tr(c, "engine.orphan.discard_confirm"),
			prompt:   tr(c, "engine.orphan.discard_prompt"),
			disabled: s.busy || s.running() != nil,
		}) {
			if s.simulate(previewStopEngine) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.DiscardOrphanRecord()
				return l10n.TR("engine.orphan.discarded"), e
			})
		}
	}
}

func orphanRecordRecoverable(state core.EngineState) bool {
	if state.State != "orphaned" || state.Error == nil {
		return false
	}
	switch state.Error.Code {
	case "engine_owner_invalid", "service_identity_unreadable", "engine_owner_remove_failed":
		return true
	}
	return false
}
