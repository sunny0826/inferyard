package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type languageOption struct {
	value    string
	labelKey string
}

var languageOptions = []languageOption{
	{value: core.LanguageSystem, labelKey: "settings.language.system"},
	{value: core.LanguageEn, labelKey: "settings.language.en"},
	{value: core.LanguageZhHans, labelKey: "settings.language.zh_hans"},
}

func normalizeLanguageMode(mode string) string {
	return core.NormalizeLanguage(mode)
}

func languageFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.Language) != "" {
		return normalizeLanguageMode(v.Effective.Language)
	}
	return normalizeLanguageMode(v.Settings.Language)
}

func (s *viewState) syncNativeLocaleIfNeeded() {
	mode := normalizeLanguageMode(s.languageMode)
	if s.appliedLanguageMode == mode {
		return
	}
	if s.applyNativeLocale != nil {
		s.appliedLanguageMode = mode
		s.applyNativeLocale(mode)
		return
	}
	if s.win == nil {
		return
	}
	s.appliedLanguageMode = mode
	syncNativeLocale(mode)
}

func (s *viewState) loadLanguageSettings(v core.SettingsResponse) {
	if s.languageSaving {
		return
	}
	s.languageMode = languageFromSettings(v)
	s.syncNativeLocaleIfNeeded()
}

func (s *viewState) languageSettingsView(c *ui.Context) {
	t := tokens(c)
	selected := normalizeLanguageMode(s.languageMode)
	idx := 0
	for i, opt := range languageOptions {
		if opt.value == selected {
			idx = i
			break
		}
	}

	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				heading(c, tr(c, "settings.language.heading"), 2).Grow(1)
				if selected == core.LanguageSystem {
					current := trf(c, "settings.language.current", "language", l10n.ResolvedLanguageName(contextLocale(c)))
					ui.Text(c, current).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
				}
			})

			options := make([]components.NativeSelectOption[int], len(languageOptions))
			for i, opt := range languageOptions {
				options[i] = components.NativeSelectOption[int]{
					Label: tr(c, opt.labelKey),
					Value: i,
				}
			}
			prev := idx
			segmentedField(c, tr(c, "settings.language.label"), "", &idx, options)
			if idx != prev && idx >= 0 && idx < len(languageOptions) {
				s.changeLanguageMode(languageOptions[idx].value)
			}
		})
}

func (s *viewState) currentLanguageSeq() uint64 {
	s.languageMu.Lock()
	defer s.languageMu.Unlock()
	return s.languageWantedSeq
}

func (s *viewState) changeLanguageMode(mode string) {
	next := normalizeLanguageMode(mode)
	if next == normalizeLanguageMode(s.languageMode) {
		return
	}
	s.languageMode = next
	s.syncNativeLocaleIfNeeded()
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"language": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistLanguage
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.languageSaving = true
	s.bumpSettingsEpoch()

	s.languageMu.Lock()
	s.languageWantedSeq++
	s.languageWanted = next
	startWorker := !s.languageWorkerRunning
	s.languageWorkerRunning = true
	s.languageMu.Unlock()

	if startWorker {
		go s.runLanguagePersistWorker(update)
	}
}

func (s *viewState) runLanguagePersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.languageMu.Lock()
		mode := s.languageWanted
		seq := s.languageWantedSeq
		s.languageMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.languageMu.Lock()
			s.languageWorkerRunning = false
			s.languageMu.Unlock()
			return
		}

		raw, _ := json.Marshal(mode)
		value, apiErr := update(map[string]json.RawMessage{"language": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.languageMu.Lock()
		latest := seq == s.languageWantedSeq
		if latest {
			s.languageWorkerRunning = false
		}
		s.languageMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishLanguagePersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishLanguagePersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.Language = lastSaved.Settings.Language
		s.settings.Effective.Language = languageFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentLanguageSeq() {
		return
	}
	s.languageSaving = false
	if apiErr != nil {
		s.languageMode = languageFromSettings(s.settings)
		s.syncNativeLocaleIfNeeded()
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.languageMode = languageFromSettings(value)
	s.syncNativeLocaleIfNeeded()
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
