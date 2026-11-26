package pages

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) syncMinimizeToTrayIfNeeded() error {
	if s.appliedMinimizeToTray == s.minimizeToTray {
		return nil
	}
	if s.applyMinimizeToTray != nil {
		if err := s.applyMinimizeToTray(s.minimizeToTray); err != nil {
			s.minimizeToTray = s.appliedMinimizeToTray
			s.trayUnavailable = true
			return err
		}
		s.trayUnavailable = false
	}
	s.appliedMinimizeToTray = s.minimizeToTray
	return nil
}

func (s *viewState) loadTraySettings(v core.SettingsResponse) {
	if !s.keepServicesSaving {
		s.keepServicesOnQuit = core.EffectiveKeepServicesOnQuitValue(v.Settings)
		if v.Settings.KeepServicesOnQuit == nil {
			s.keepServicesOnQuit = v.Effective.KeepServicesOnQuit
		}
	}
	if s.minimizeToTraySaving {
		return
	}
	if v.Settings.MinimizeToTray && s.trayUnavailable {
		s.minimizeToTray = false
		return
	}
	s.minimizeToTray = v.Settings.MinimizeToTray
	_ = s.syncMinimizeToTrayIfNeeded()
}

func (s *viewState) traySettingsView(c *ui.Context) {
	t := tokens(c)
	card(c, tr(c, "settings.tray.title"), func() {
		enabled := s.minimizeToTray
		labeledSwitchRow(c, tr(c, "settings.tray.minimize_label"), tr(c, "settings.tray.minimize_desc"), tr(c, "settings.tray.switch_label"), &enabled)
		if enabled != s.minimizeToTray {
			s.changeMinimizeToTray(enabled)
		}
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		keep := s.keepServicesOnQuit
		labeledSwitchRow(c, tr(c, "settings.keep_services.label"), tr(c, "settings.keep_services.desc"), tr(c, "settings.keep_services.switch_label"), &keep)
		if keep != s.keepServicesOnQuit {
			s.changeKeepServicesOnQuit(keep)
		}
	})
}

func (s *viewState) currentMinimizeToTraySeq() uint64 {
	s.minimizeToTrayMu.Lock()
	defer s.minimizeToTrayMu.Unlock()
	return s.minimizeToTrayWantedSeq
}

func (s *viewState) changeMinimizeToTray(enabled bool) {
	if enabled == s.minimizeToTray {
		return
	}
	if s.applyMinimizeToTray != nil {
		if err := s.applyMinimizeToTray(enabled); err != nil {
			s.trayUnavailable = true
			s.failure = &core.ApiError{Code: "tray_unavailable", Message: l10n.TR("settings.tray.unavailable")}
			s.failurePage = s.selected
			return
		}
		s.trayUnavailable = false
	}
	s.minimizeToTray = enabled
	s.appliedMinimizeToTray = enabled
	raw, _ := json.Marshal(enabled)
	patch := map[string]json.RawMessage{"minimize_to_tray": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistMinimizeToTray
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.minimizeToTraySaving = true
	s.bumpSettingsEpoch()

	s.minimizeToTrayMu.Lock()
	s.minimizeToTrayWantedSeq++
	s.minimizeToTrayWanted = enabled
	startWorker := !s.minimizeToTrayWorkerRunning
	if startWorker {
		s.minimizeToTrayWorkerRunning = true
		s.minimizeToTrayWorkerDone = make(chan struct{})
	}
	s.minimizeToTrayMu.Unlock()

	if startWorker {
		go s.runMinimizeToTrayPersistWorker(update)
	}
}

func (s *viewState) waitMinimizeToTrayPersist() {
	s.minimizeToTrayMu.Lock()
	done := s.minimizeToTrayWorkerDone
	s.minimizeToTrayMu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *viewState) runMinimizeToTrayPersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.minimizeToTrayMu.Lock()
		enabled := s.minimizeToTrayWanted
		seq := s.minimizeToTrayWantedSeq
		s.minimizeToTrayMu.Unlock()

		raw, _ := json.Marshal(enabled)
		value, apiErr := update(map[string]json.RawMessage{"minimize_to_tray": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.minimizeToTrayMu.Lock()
		latest := seq == s.minimizeToTrayWantedSeq
		if latest {
			s.minimizeToTrayWorkerRunning = false
			done := s.minimizeToTrayWorkerDone
			s.minimizeToTrayWorkerDone = nil
			s.minimizeToTrayMu.Unlock()
			if s.ctx == nil || s.ctx.Err() == nil {
				s.runOnUI(func() { s.finishMinimizeToTrayPersist(seq, lastSaved, value, apiErr) })
			}
			if done != nil {
				close(done)
			}
			return
		}
		s.minimizeToTrayMu.Unlock()
	}
}

func (s *viewState) finishMinimizeToTrayPersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.MinimizeToTray = lastSaved.Settings.MinimizeToTray
		s.settings.Effective.MinimizeToTray = lastSaved.Settings.MinimizeToTray
		s.bumpSettingsEpoch()
	}
	if seq != s.currentMinimizeToTraySeq() {
		return
	}
	s.minimizeToTraySaving = false
	if apiErr != nil {
		s.minimizeToTray = s.settings.Settings.MinimizeToTray
		_ = s.syncMinimizeToTrayIfNeeded()
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.minimizeToTray = value.Settings.MinimizeToTray
	_ = s.syncMinimizeToTrayIfNeeded()
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}

func (s *viewState) currentKeepServicesSeq() uint64 {
	s.keepServicesMu.Lock()
	defer s.keepServicesMu.Unlock()
	return s.keepServicesWantedSeq
}

func (s *viewState) changeKeepServicesOnQuit(enabled bool) {
	if enabled == s.keepServicesOnQuit {
		return
	}
	s.keepServicesOnQuit = enabled
	raw, _ := json.Marshal(enabled)
	patch := map[string]json.RawMessage{"keep_services_on_quit": raw}
	if s.simulate(previewSettings{patch: patch}) {
		return
	}
	update := s.persistKeepServices
	if update == nil && s.service != nil {
		update = s.service.UpdateSettings
	}
	if update == nil {
		return
	}
	s.keepServicesSaving = true
	s.bumpSettingsEpoch()

	s.keepServicesMu.Lock()
	s.keepServicesWantedSeq++
	s.keepServicesWanted = enabled
	startWorker := !s.keepServicesWorkerRunning
	if startWorker {
		s.keepServicesWorkerRunning = true
		s.keepServicesWorkerDone = make(chan struct{})
	}
	s.keepServicesMu.Unlock()

	if startWorker {
		go s.runKeepServicesPersistWorker(update)
	}
}

func (s *viewState) waitKeepServicesPersist() {
	s.keepServicesMu.Lock()
	done := s.keepServicesWorkerDone
	s.keepServicesMu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *viewState) runKeepServicesPersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.keepServicesMu.Lock()
		enabled := s.keepServicesWanted
		seq := s.keepServicesWantedSeq
		s.keepServicesMu.Unlock()

		raw, _ := json.Marshal(enabled)
		value, apiErr := update(map[string]json.RawMessage{"keep_services_on_quit": raw})
		if apiErr == nil {
			v := value
			lastSaved = &v
		}

		s.keepServicesMu.Lock()
		latest := seq == s.keepServicesWantedSeq
		if latest {
			s.keepServicesWorkerRunning = false
			done := s.keepServicesWorkerDone
			s.keepServicesWorkerDone = nil
			s.keepServicesMu.Unlock()
			if s.ctx == nil || s.ctx.Err() == nil {
				s.runOnUI(func() { s.finishKeepServicesPersist(seq, lastSaved, value, apiErr) })
			}
			if done != nil {
				close(done)
			}
			return
		}
		s.keepServicesMu.Unlock()
	}
}

func (s *viewState) finishKeepServicesPersist(seq uint64, lastSaved *core.SettingsResponse, value core.SettingsResponse, apiErr *core.ApiError) {
	if lastSaved != nil {
		s.settings.Settings.KeepServicesOnQuit = lastSaved.Settings.KeepServicesOnQuit
		s.settings.Effective.KeepServicesOnQuit = lastSaved.Effective.KeepServicesOnQuit
		s.bumpSettingsEpoch()
	}
	if seq != s.currentKeepServicesSeq() {
		return
	}
	s.keepServicesSaving = false
	if apiErr != nil {
		s.keepServicesOnQuit = s.settings.Effective.KeepServicesOnQuit
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.keepServicesOnQuit = value.Effective.KeepServicesOnQuit
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
