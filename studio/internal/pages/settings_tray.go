package pages

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) syncMinimizeToTrayIfNeeded() {
	if s.appliedMinimizeToTray == s.minimizeToTray {
		return
	}
	s.appliedMinimizeToTray = s.minimizeToTray
	if s.applyMinimizeToTray != nil {
		s.applyMinimizeToTray(s.minimizeToTray)
	}
}

func (s *viewState) loadTraySettings(v core.SettingsResponse) {
	if s.minimizeToTraySaving {
		return
	}
	s.minimizeToTray = v.Settings.MinimizeToTray
	s.syncMinimizeToTrayIfNeeded()
}

func (s *viewState) traySettingsView(c *ui.Context) {
	t := tokens(c)
	card(c, "系统托盘", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "关闭窗口时最小化到托盘").FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
				ui.Text(c, "保持后台测评与下载继续运行，可通过托盘菜单恢复窗口或退出").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			enabled := s.minimizeToTray
			components.Switch(c, &enabled, "最小化到系统托盘", components.SwitchOptions{HideLabel: true})
			if enabled != s.minimizeToTray {
				s.changeMinimizeToTray(enabled)
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
	s.minimizeToTray = enabled
	s.syncMinimizeToTrayIfNeeded()
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
	s.minimizeToTrayWorkerRunning = true
	s.minimizeToTrayMu.Unlock()

	if startWorker {
		go s.runMinimizeToTrayPersistWorker(update)
	}
}

func (s *viewState) runMinimizeToTrayPersistWorker(update func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)) {
	var lastSaved *core.SettingsResponse
	for {
		s.minimizeToTrayMu.Lock()
		enabled := s.minimizeToTrayWanted
		seq := s.minimizeToTrayWantedSeq
		s.minimizeToTrayMu.Unlock()

		if s.ctx != nil && s.ctx.Err() != nil {
			s.minimizeToTrayMu.Lock()
			s.minimizeToTrayWorkerRunning = false
			s.minimizeToTrayMu.Unlock()
			return
		}

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
		}
		s.minimizeToTrayMu.Unlock()

		if latest {
			s.runOnUI(func() { s.finishMinimizeToTrayPersist(seq, lastSaved, value, apiErr) })
			return
		}
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
		s.syncMinimizeToTrayIfNeeded()
		s.failure = apiErr
		s.failurePage = s.selected
		return
	}
	s.minimizeToTray = value.Settings.MinimizeToTray
	s.syncMinimizeToTrayIfNeeded()
	if s.failurePage == "settings" {
		s.failure = nil
		s.failurePage = ""
	}
}
