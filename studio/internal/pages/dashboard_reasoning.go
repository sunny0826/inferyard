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

func (s *viewState) loadReasoningSettings(v core.SettingsResponse) {
	if s.reasoningSaving {
		return
	}
	s.reasoningEffort = reasoningEffortFromSettings(v)
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
