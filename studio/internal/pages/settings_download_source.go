package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

type downloadSourceOption struct {
	value string
	label string
}

var downloadSourceOptions = []downloadSourceOption{
	{value: core.DownloadSourceModelScope, label: "ModelScope"},
	{value: core.DownloadSourceHuggingFace, label: "HuggingFace"},
}

func downloadSourceFromSettings(v core.SettingsResponse) string {
	if strings.TrimSpace(v.Effective.DownloadSource) != "" {
		return core.NormalizeDownloadSource(v.Effective.DownloadSource)
	}
	return core.NormalizeDownloadSource(v.Settings.DownloadSource)
}

func (s *viewState) effectiveDownloadSource() string {
	if strings.TrimSpace(s.downloadSource) != "" {
		return core.NormalizeDownloadSource(s.downloadSource)
	}
	return downloadSourceFromSettings(s.settings)
}

func (s *viewState) loadDownloadSourceSettings(v core.SettingsResponse) {
	if s.downloadSourceSaving {
		return
	}
	next := downloadSourceFromSettings(v)
	s.downloadSource = next
	if s.downloadsUI != nil && !s.downloadsUI.sourceCustomized {
		s.downloadsUI.applyDefaultModelSource(next)
	}
}

func (s *viewState) downloadSourceSettingsView(c *ui.Context) {
	t := tokens(c)
	selected := s.effectiveDownloadSource()
	idx := 0
	for i, opt := range downloadSourceOptions {
		if opt.value == selected {
			idx = i
			break
		}
	}

	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).
		Children(func() {
			heading(c, tr(c, "settings.download_source.heading"), 2)

			options := make([]components.NativeSelectOption[int], len(downloadSourceOptions))
			for i, opt := range downloadSourceOptions {
				options[i] = components.NativeSelectOption[int]{
					Label: opt.label,
					Value: i,
				}
			}
			prev := idx
			segmentedField(c, tr(c, "settings.download_source.label"), "", &idx, options)
			if idx != prev && idx >= 0 && idx < len(downloadSourceOptions) {
				s.changeDownloadSource(downloadSourceOptions[idx].value)
			}
		})
}

func (s *viewState) currentDownloadSourceSeq() uint64 {
	s.downloadSourceMu.Lock()
	defer s.downloadSourceMu.Unlock()
	return s.downloadSourceWantedSeq
}

func (s *viewState) changeDownloadSource(source string) {
	next := core.NormalizeDownloadSource(source)
	if next == s.effectiveDownloadSource() {
		return
	}
	s.downloadSource = next
	if s.downloadsUI != nil {
		s.downloadsUI.applyDefaultModelSource(next)
	}
	raw, _ := json.Marshal(next)
	patch := map[string]json.RawMessage{"download_source": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistDownloadSource
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.downloadSourceSaving = true
	s.bumpSettingsEpoch()

	s.downloadSourceMu.Lock()
	s.downloadSourceWantedSeq++
	s.downloadSourceWanted = next
	startWorker := !s.downloadSourceWorkerRunning
	s.downloadSourceWorkerRunning = true
	s.downloadSourceMu.Unlock()

	if startWorker {
		go s.runDownloadSourcePersistWorker(update)
	}
}

func (s *viewState) runDownloadSourcePersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.downloadSourceMu.Lock()
		source := s.downloadSourceWanted
		seq := s.downloadSourceWantedSeq
		s.downloadSourceMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.downloadSourceMu.Lock()
			s.downloadSourceWorkerRunning = false
			s.downloadSourceMu.Unlock()
			return
		}

		raw, _ := json.Marshal(source)
		value, apiErr := update(map[string]json.RawMessage{"download_source": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.downloadSourceMu.Lock()
		latest := seq == s.downloadSourceWantedSeq
		if latest {
			s.downloadSourceWorkerRunning = false
		}
		s.downloadSourceMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishDownloadSourcePersist(seq, lastSaved, value, apiErr) })
			return
		}
	}
}

func (s *viewState) finishDownloadSourcePersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.DownloadSource = lastSaved.Settings.DownloadSource
		s.settings.Effective.DownloadSource = downloadSourceFromSettings(*lastSaved)
		s.bumpSettingsEpoch()
	}
	if seq != s.currentDownloadSourceSeq() {
		return
	}
	s.downloadSourceSaving = false
	if apiErr != nil {
		s.downloadSource = downloadSourceFromSettings(s.settings)
		if s.downloadsUI != nil && !s.downloadsUI.sourceCustomized {
			s.downloadsUI.applyDefaultModelSource(s.downloadSource)
		}
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.downloadSource = downloadSourceFromSettings(value)
	if s.downloadsUI != nil && !s.downloadsUI.sourceCustomized {
		s.downloadsUI.applyDefaultModelSource(s.downloadSource)
	}
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
