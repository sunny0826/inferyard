package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func accentFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.Accent) != "" {
		return core.NormalizeAccent(v.Effective.Accent)
	}
	return core.NormalizeAccent(v.Settings.Accent)
}

func (s *viewState) loadAccentSettings(v core.SettingsResponse) {
	if s.accentSaving {
		return
	}
	s.accentMode = accentFromSettings(v)
	setActiveAccent(s.accentMode)
}

func (s *viewState) accentColorRow(c *ui.Context) {
	t := tokens(c)
	selected := core.NormalizeAccent(s.accentMode)
	spec := studioAccentSpecFor(selected)
	dark := c.Theme().Dark
	rimAlpha := float32(.12)
	if dark {
		rimAlpha = .20
	}
	rim := t.Color.FgPrimary.Alpha(rimAlpha)

	ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(t.Density.Row).Padding(t.Density.Tight, 0, 0, 0).Children(func() {
		ui.Column(c).Grow(1).MinWidth(140).Gap(t.Density.Hairline).Children(func() {
			ui.Text(c, tr(c, "settings.appearance.accent_label")).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
			ui.Text(c, tr(c, spec.labelKey)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})

		studioTooltipGroup(c, ui.Row(c).AlignItems(ui.Center).Gap(8), func() {
			for _, candidate := range studioAccentSpecs {
				candidate := candidate
				active := candidate.id == selected
				swatchColor := candidate.light.accent
				if dark {
					swatchColor = candidate.dark.accent
				}
				label := tr(c, candidate.labelKey)

				ring := ui.Box(c.Key("accent-"+candidate.id)).Size(30, 30).Radius(t.Radius.Full).
					AlignItems(ui.Center).Justify(ui.Center).
					Role(ui.RoleButton).Focusable().Label(label)
				switch {
				case active:
					ring.Border(2, swatchColor).Background(t.Color.Surface)
				case ring.Hovered():
					ring.Border(1.5, t.Color.BorderStrong)
				default:
					ring.Border(2, ui.Color{})
				}
				if ring.Clicked() {
					s.changeAccentMode(candidate.id)
				}
				ring.Children(func() {
					ui.Box(c).Size(22, 22).Radius(t.Radius.Full).Background(swatchColor).Border(1, rim)
				})
				studioTooltip(c, ring, label)
			}
		})
	})
}

func (s *viewState) currentAccentSeq() uint64 {
	s.accentMu.Lock()
	defer s.accentMu.Unlock()
	return s.accentWantedSeq
}

func (s *viewState) changeAccentMode(accent string) {
	next := core.NormalizeAccent(accent)
	if next == core.NormalizeAccent(s.accentMode) {
		return
	}
	s.accentMode = next
	setActiveAccent(next)
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"accent": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistAccent
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.accentSaving = true
	s.bumpSettingsEpoch()

	s.accentMu.Lock()
	s.accentWantedSeq++
	s.accentWanted = next
	startWorker := !s.accentWorkerRunning
	s.accentWorkerRunning = true
	s.accentMu.Unlock()

	if startWorker {
		go s.runAccentPersistWorker(update)
	}
}

func (s *viewState) runAccentPersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.accentMu.Lock()
		accent := s.accentWanted
		seq := s.accentWantedSeq
		s.accentMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.accentMu.Lock()
			s.accentWorkerRunning = false
			s.accentMu.Unlock()
			return
		}

		raw, _ := json.Marshal(accent)
		value, apiErr := update(map[string]json.RawMessage{"accent": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.accentMu.Lock()
		latest := seq == s.accentWantedSeq
		if latest {
			s.accentWorkerRunning = false
		}
		s.accentMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishAccentPersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishAccentPersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.Accent = lastSaved.Settings.Accent
		s.settings.Effective.Accent = accentFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentAccentSeq() {
		return
	}
	s.accentSaving = false
	if apiErr != nil {
		s.accentMode = accentFromSettings(s.settings)
		setActiveAccent(s.accentMode)
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.accentMode = accentFromSettings(value)
	setActiveAccent(s.accentMode)
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
