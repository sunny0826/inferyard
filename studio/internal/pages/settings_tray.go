package pages

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
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
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, tr(c, "settings.tray.minimize_label")).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
				ui.Text(c, tr(c, "settings.tray.minimize_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			enabled := s.minimizeToTray
			components.Switch(c, &enabled, tr(c, "settings.tray.switch_label"), components.SwitchOptions{HideLabel: true})
			if enabled != s.minimizeToTray {
				s.changeMinimizeToTray(enabled)
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, tr(c, "settings.keep_services.label")).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
				ui.Text(c, tr(c, "settings.keep_services.desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			keep := s.keepServicesOnQuit
			components.Switch(c, &keep, tr(c, "settings.keep_services.switch_label"), components.SwitchOptions{HideLabel: true})
			if keep != s.keepServicesOnQuit {
				s.changeKeepServicesOnQuit(keep)
			}
		})
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
	seq := s.keepServicesWantedSeq
	s.keepServicesWanted = enabled
	s.keepServicesMu.Unlock()

	go func() {
		value, apiErr := update(patch)
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		s.runOnUI(func() {
			s.keepServicesMu.Lock()
			latest := seq == s.keepServicesWantedSeq
			s.keepServicesMu.Unlock()
			if apiErr == nil {
				s.settings.Settings.KeepServicesOnQuit = value.Settings.KeepServicesOnQuit
				s.settings.Effective.KeepServicesOnQuit = value.Effective.KeepServicesOnQuit
				s.bumpSettingsEpoch()
			}
			if !latest {
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
		})
	}()
}
