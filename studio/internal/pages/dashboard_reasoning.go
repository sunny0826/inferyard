package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func reasoningEffortFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.ReasoningEffort) != "" {
		return core.NormalizeReasoningEffort(v.Effective.ReasoningEffort)
	}
	return core.NormalizeReasoningEffort(v.Settings.ReasoningEffort)
}

func (s *viewState) effectiveReasoningEffort() string {
	if strings.TrimSpace(s.reasoningEffort) != "" {
		return core.NormalizeReasoningEffort(s.reasoningEffort)
	}
	return reasoningEffortFromSettings(s.settings)
}

func reasoningModeFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.ReasoningMode) != "" {
		return core.NormalizeReasoningMode(v.Effective.ReasoningMode)
	}
	return core.NormalizeReasoningMode(v.Settings.ReasoningMode)
}

func (s *viewState) effectiveReasoningMode() string {
	if strings.TrimSpace(s.reasoningMode) != "" {
		return core.NormalizeReasoningMode(s.reasoningMode)
	}
	return reasoningModeFromSettings(s.settings)
}

func (s *viewState) loadReasoningSettings(v core.SettingsResponse) {
	if !s.reasoningSaving {
		s.reasoningEffort = reasoningEffortFromSettings(v)
	}
	if !s.reasoningModeSaving {
		s.reasoningMode = reasoningModeFromSettings(v)
	}
}

func reasoningModeSummaryLabel(mode string) string {
	if core.NormalizeReasoningMode(mode) == core.ReasoningModeOn {
		return l10n.TR("dashboard.exec.thinking_on")
	}
	return l10n.TR("dashboard.exec.thinking_none")
}

func (s *viewState) activeThinkingBadge(c *ui.Context) (string, stateKind) {
	if e := s.localEngine(); e != nil && e.Family == "polystrata" {
		effort := s.effectiveReasoningEffort()
		kind := stateNeutral
		if effort != core.ReasoningEffortNone {
			kind = stateInfo
		}
		return trf(c, "dashboard.exec.thinking_badge", "state", reasoningSummaryLabel(effort)), kind
	}
	mode := s.effectiveReasoningMode()
	kind := stateNeutral
	if mode == core.ReasoningModeOn {
		kind = stateInfo
	}
	return trf(c, "dashboard.exec.thinking_badge", "state", reasoningModeSummaryLabel(mode)), kind
}

func (s *viewState) benchmarkReasoningView(c *ui.Context) {
	if s.engine == "" {
		return
	}
	if e := s.localEngine(); e != nil && e.Family == "polystrata" {
		s.polystrataReasoningView(c)
		return
	}
	s.reasoningModeView(c)
}

func (s *viewState) reasoningModeView(c *ui.Context) {
	prev := s.effectiveReasoningMode()
	enabled := prev == core.ReasoningModeOn
	labeledSwitchRow(
		c,
		tr(c, "dashboard.candidate.reasoning_mode"),
		tr(c, "dashboard.candidate.reasoning_mode_hint"),
		tr(c, "dashboard.candidate.reasoning_mode_switch"),
		&enabled,
	)
	next := core.ReasoningModeOff
	if enabled {
		next = core.ReasoningModeOn
	}
	if next != prev {
		s.changeReasoningMode(next)
	}
}

func (s *viewState) changeReasoningMode(mode string) {
	next := core.NormalizeReasoningMode(mode)
	if next == s.effectiveReasoningMode() {
		return
	}
	s.reasoningMode = next
	s.settings.Settings.ReasoningMode = next
	s.settings.Effective.ReasoningMode = next
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"reasoning_mode": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	if s.service == nil {
		return
	}
	s.reasoningModeSaving = true
	s.bumpSettingsEpoch()

	s.reasoningModeMu.Lock()
	s.reasoningModeWantedSeq++
	s.reasoningModeWanted = next
	startWorker := !s.reasoningModeWorkerRunning
	s.reasoningModeWorkerRunning = true
	s.reasoningModeMu.Unlock()

	if startWorker {
		go s.runReasoningModePersistWorker()
	}
}

func (s *viewState) runReasoningModePersistWorker() {
	for {
		s.reasoningModeMu.Lock()
		mode := s.reasoningModeWanted
		seq := s.reasoningModeWantedSeq
		s.reasoningModeMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.reasoningModeMu.Lock()
			s.reasoningModeWorkerRunning = false
			s.reasoningModeMu.Unlock()
			return
		}

		raw, _ := json.Marshal(mode)
		value, apiErr := s.service.UpdateSettings(map[string]json.RawMessage{"reasoning_mode": raw})

		s.reasoningModeMu.Lock()
		latest := seq == s.reasoningModeWantedSeq
		if latest {
			s.reasoningModeWorkerRunning = false
		}
		s.reasoningModeMu.Unlock()

		if latest {
			s.runOnUI(func() {
				s.reasoningModeSaving = false
				if apiErr != nil {
					s.failure = apiErr
					s.failurePage = s.selected
					return
				}
				resolved := reasoningModeFromSettings(value)
				s.reasoningMode = resolved
				s.settings.Settings.ReasoningMode = value.Settings.ReasoningMode
				s.settings.Effective.ReasoningMode = resolved
				s.bumpSettingsEpoch()
			})
			return
		}
	}
}

func reasoningSummaryLabel(effort string) string {
	switch core.NormalizeReasoningEffort(effort) {
	case core.ReasoningEffortLow:
		return l10n.TR("dashboard.exec.thinking_low")
	case core.ReasoningEffortMedium:
		return l10n.TR("dashboard.exec.thinking_medium")
	case core.ReasoningEffortHigh:
		return l10n.TR("dashboard.exec.thinking_high")
	default:
		return l10n.TR("dashboard.exec.thinking_none")
	}
}

func (s *viewState) polystrataReasoningView(c *ui.Context) {
	e := s.localEngine()
	if e == nil || e.Family != "polystrata" {
		return
	}
	prev := s.effectiveReasoningEffort()
	selected := prev
	options := []components.NativeSelectOption[string]{
		{Value: core.ReasoningEffortNone, Label: tr(c, "dashboard.candidate.reasoning_none")},
		{Value: core.ReasoningEffortLow, Label: tr(c, "dashboard.candidate.reasoning_low")},
		{Value: core.ReasoningEffortMedium, Label: tr(c, "dashboard.candidate.reasoning_medium")},
		{Value: core.ReasoningEffortHigh, Label: tr(c, "dashboard.candidate.reasoning_high")},
	}
	selectField(
		c,
		tr(c, "dashboard.candidate.reasoning_effort"),
		tr(c, "dashboard.candidate.reasoning_effort_hint"),
		&selected,
		options,
	)
	if next := core.NormalizeReasoningEffort(selected); next != prev {
		s.changeReasoningEffort(next)
	}
}

func (s *viewState) changeReasoningEffort(effort string) {
	next := core.NormalizeReasoningEffort(effort)
	if next == s.effectiveReasoningEffort() {
		return
	}
	s.reasoningEffort = next
	s.settings.Settings.ReasoningEffort = next
	s.settings.Effective.ReasoningEffort = next
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"reasoning_effort": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	if s.service == nil {
		return
	}
	s.reasoningSaving = true
	s.bumpSettingsEpoch()

	s.reasoningMu.Lock()
	s.reasoningWantedSeq++
	s.reasoningWanted = next
	startWorker := !s.reasoningWorkerRunning
	s.reasoningWorkerRunning = true
	s.reasoningMu.Unlock()

	if startWorker {
		go s.runReasoningPersistWorker()
	}
}

func (s *viewState) runReasoningPersistWorker() {
	for {
		s.reasoningMu.Lock()
		effort := s.reasoningWanted
		seq := s.reasoningWantedSeq
		s.reasoningMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.reasoningMu.Lock()
			s.reasoningWorkerRunning = false
			s.reasoningMu.Unlock()
			return
		}

		raw, _ := json.Marshal(effort)
		value, apiErr := s.service.UpdateSettings(map[string]json.RawMessage{"reasoning_effort": raw})

		s.reasoningMu.Lock()
		latest := seq == s.reasoningWantedSeq
		if latest {
			s.reasoningWorkerRunning = false
		}
		s.reasoningMu.Unlock()

		if latest {
			s.runOnUI(func() {
				s.reasoningSaving = false
				if apiErr != nil {
					s.failure = apiErr
					s.failurePage = s.selected
					return
				}
				resolved := reasoningEffortFromSettings(value)
				s.reasoningEffort = resolved
				s.settings.Settings.ReasoningEffort = value.Settings.ReasoningEffort
				s.settings.Effective.ReasoningEffort = resolved
				s.bumpSettingsEpoch()
			})
			return
		}
	}
}
