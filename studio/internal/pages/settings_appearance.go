package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

var appearanceOptions = []components.NativeSelectOption[string]{
	{Label: "跟随系统", Value: core.ThemeSystem},
	{Label: "浅色", Value: core.ThemeLight},
	{Label: "深色", Value: core.ThemeDark},
}

func themeFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.Theme) != "" {
		return normalizeThemeMode(v.Effective.Theme)
	}
	return normalizeThemeMode(v.Settings.Theme)
}

func (s *viewState) syncNativeThemeIfNeeded() {
	mode := normalizeThemeMode(s.themeMode)
	if s.appliedThemeMode == mode {
		return
	}
	if s.applyNativeTheme != nil {
		s.appliedThemeMode = mode
		s.applyNativeTheme(mode)
		return
	}
	if s.win == nil {
		return
	}
	s.appliedThemeMode = mode
	syncNativeTheme(mode)
}

func (s *viewState) themeFor(c *ui.Context) *ui.Theme {
	s.syncNativeThemeIfNeeded()
	return resolveStudioTheme(c, s.themeMode)
}

func (s *viewState) loadThemeSettings(v core.SettingsResponse) {
	if s.themeSaving {
		return
	}
	s.themeMode = themeFromSettings(v)
	s.syncNativeThemeIfNeeded()
}

func (s *viewState) appearanceSettingsView(c *ui.Context) {
	settingsFormSection(c, settingsFormOptions{
		Title:       "外观",
		Description: "选择浅色、深色或跟随系统外观，切换后立即生效。",
		Dirty:       false,
		Fields: func() {
			selected := normalizeThemeMode(s.themeMode)
			segmentedField(c, "外观模式", "", &selected, appearanceOptions)
			if selected != normalizeThemeMode(s.themeMode) {
				s.changeThemeMode(selected)
			}
		},
	})
}

func (s *viewState) bumpSettingsEpoch() {
	s.settingsEpochMu.Lock()
	s.settingsEpoch++
	s.settingsEpochMu.Unlock()
}

func (s *viewState) currentThemeSeq() uint64 {
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	return s.themeWantedSeq
}

func (s *viewState) runOnUI(fn func()) {
	if s.updateWindow != nil {
		s.updateWindow(fn)
		return
	}
	if s.win != nil {
		s.win.Update(fn)
		return
	}
	fn()
}

func (s *viewState) changeThemeMode(mode string) {
	next := normalizeThemeMode(mode)
	if next == normalizeThemeMode(s.themeMode) {
		return
	}
	s.themeMode = next
	s.syncNativeThemeIfNeeded()
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"theme": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistTheme
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.themeSaving = true
	s.bumpSettingsEpoch()

	s.themeMu.Lock()
	s.themeWantedSeq++
	s.themeWanted = next
	startWorker := !s.themeWorkerRunning
	s.themeWorkerRunning = true
	s.themeMu.Unlock()

	if startWorker {
		go s.runThemePersistWorker(update)
	}
}

func (s *viewState) runThemePersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.themeMu.Lock()
		mode := s.themeWanted
		seq := s.themeWantedSeq
		s.themeMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.themeMu.Lock()
			s.themeWorkerRunning = false
			s.themeMu.Unlock()
			return
		}

		raw, _ := json.Marshal(mode)
		value, apiErr := update(map[string]json.RawMessage{"theme": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.themeMu.Lock()
		latest := seq == s.themeWantedSeq
		if latest {
			s.themeWorkerRunning = false
		}
		s.themeMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishThemePersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishThemePersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.Theme = lastSaved.Settings.Theme
		s.settings.Effective.Theme = themeFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentThemeSeq() {
		return
	}
	s.themeSaving = false
	if apiErr != nil {
		s.themeMode = themeFromSettings(s.settings)
		s.syncNativeThemeIfNeeded()
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.themeMode = themeFromSettings(value)
	s.syncNativeThemeIfNeeded()
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
