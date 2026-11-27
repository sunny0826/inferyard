package pages

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

type studioFontSizeSpec struct {
	id       string
	labelKey string
	px       float32
}

var (
	studioFontSizeSpecs = []studioFontSizeSpec{
		{id: core.FontSizeSmaller, labelKey: "settings.appearance.font_size.smaller", px: 12},
		{id: core.FontSizeSmall, labelKey: "settings.appearance.font_size.small", px: 13},
		{id: core.FontSizeDefault, labelKey: "settings.appearance.font_size.default", px: 14},
		{id: core.FontSizeLarge, labelKey: "settings.appearance.font_size.large", px: 15},
		{id: core.FontSizeLarger, labelKey: "settings.appearance.font_size.larger", px: 16},
	}

	activeFontSizeMu sync.RWMutex
	activeFontSizeID = core.FontSizeDefault
)

func setActiveFontSize(size string) {
	normalized := core.NormalizeFontSize(size)
	activeFontSizeMu.Lock()
	activeFontSizeID = normalized
	activeFontSizeMu.Unlock()
}

func activeFontSize() string {
	activeFontSizeMu.RLock()
	id := activeFontSizeID
	activeFontSizeMu.RUnlock()
	return core.NormalizeFontSize(id)
}

func studioFontSizeSpecFor(id string) studioFontSizeSpec {
	normalized := core.NormalizeFontSize(id)
	for _, spec := range studioFontSizeSpecs {
		if spec.id == normalized {
			return spec
		}
	}
	return studioFontSizeSpecs[2]
}

func studioBaseFontSize() float32 {
	return studioFontSizeSpecFor(activeFontSize()).px
}

func fontSizeFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.FontSize) != "" {
		return core.NormalizeFontSize(v.Effective.FontSize)
	}
	return core.NormalizeFontSize(v.Settings.FontSize)
}

func (s *viewState) loadFontSizeSettings(v core.SettingsResponse) {
	if s.fontSizeSaving {
		return
	}
	s.fontSizeMode = fontSizeFromSettings(v)
	setActiveFontSize(s.fontSizeMode)
}

func (s *viewState) fontSizeRow(c *ui.Context) {
	t := tokens(c)
	selected := core.NormalizeFontSize(s.fontSizeMode)
	chosen := selected
	currentSpec := studioFontSizeSpecFor(selected)
	currentLabel := tr(c, currentSpec.labelKey)
	fieldLabel := tr(c, "settings.appearance.font_size_label")

	ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(t.Density.Row).Padding(t.Density.Hairline, 0, 0, 0).Children(func() {
		ui.Text(c, fieldLabel).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(120).SingleLine()

		sel := studioSelect(c, &chosen)
		sel.Trigger.Width(188).Label(fieldLabel).Value(currentLabel).Children(func() {
			ui.Text(c, currentLabel).SingleLine()
		})
		sel.Popup(func(panel ui.Element) {
			panel.MinWidth(188)
			for _, spec := range studioFontSizeSpecs {
				itemLabel := tr(c, spec.labelKey)
				active := spec.id == selected
				sel.Item(spec.id).Label(itemLabel).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						txt := ui.Text(c, itemLabel).Grow(1).SingleLine()
						if active {
							txt.FontWeight(500).TextColor(t.Color.FgPrimary)
							studioIcon(c, reicon.Check, t.Color.FgPrimary)
						}
					})
				})
			}
		})
		if sel.Trigger.Changed() && chosen != selected {
			s.changeFontSizeMode(chosen)
		}
	})
}

func (s *viewState) currentFontSizeSeq() uint64 {
	s.fontSizeMu.Lock()
	defer s.fontSizeMu.Unlock()
	return s.fontSizeWantedSeq
}

func (s *viewState) changeFontSizeMode(size string) {
	next := core.NormalizeFontSize(size)
	if next == core.NormalizeFontSize(s.fontSizeMode) {
		return
	}
	s.fontSizeMode = next
	setActiveFontSize(next)
	if s.win != nil {
		s.win.Invalidate()
	}
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"font_size": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistFontSize
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.fontSizeSaving = true
	s.bumpSettingsEpoch()

	s.fontSizeMu.Lock()
	s.fontSizeWantedSeq++
	s.fontSizeWanted = next
	startWorker := !s.fontSizeWorkerRunning
	s.fontSizeWorkerRunning = true
	s.fontSizeMu.Unlock()

	if startWorker {
		go s.runFontSizePersistWorker(update)
	}
}

func (s *viewState) runFontSizePersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.fontSizeMu.Lock()
		size := s.fontSizeWanted
		seq := s.fontSizeWantedSeq
		s.fontSizeMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.fontSizeMu.Lock()
			s.fontSizeWorkerRunning = false
			s.fontSizeMu.Unlock()
			return
		}

		raw, _ := json.Marshal(size)
		value, apiErr := update(map[string]json.RawMessage{"font_size": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.fontSizeMu.Lock()
		latest := seq == s.fontSizeWantedSeq
		if latest {
			s.fontSizeWorkerRunning = false
		}
		s.fontSizeMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishFontSizePersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishFontSizePersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.FontSize = lastSaved.Settings.FontSize
		s.settings.Effective.FontSize = fontSizeFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentFontSizeSeq() {
		return
	}
	s.fontSizeSaving = false
	if apiErr != nil {
		s.fontSizeMode = fontSizeFromSettings(s.settings)
		setActiveFontSize(s.fontSizeMode)
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.fontSizeMode = fontSizeFromSettings(value)
	setActiveFontSize(s.fontSizeMode)
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
