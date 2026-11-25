package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

type quitServiceDialogState struct {
	open        bool
	bypassClose bool
	closeWindow func()
}

func (s *viewState) standaloneServiceRunning() bool {
	return len(s.activeStandaloneServices()) > 0
}

func (s *viewState) shouldShowQuitServiceDialog() bool {
	if s.quitDialog.bypassClose {
		return false
	}
	if s.minimizeToTray && !s.trayUnavailable {
		return false
	}
	return s.standaloneServiceRunning()
}

func (s *viewState) handleWindowClose(e *mygo.CloseEvent) {
	if !s.shouldShowQuitServiceDialog() {
		return
	}
	if e != nil {
		e.PreventDefault()
	}
	s.quitDialog.open = true
	if s.win != nil {
		s.win.Invalidate()
	}
}

func (s *viewState) confirmQuitWithServicePolicy(keepRunning bool) {
	if s.service != nil {
		s.service.SetKeepServicesOnShutdown(keepRunning)
	}
	s.quitDialog.open = false
	s.quitDialog.bypassClose = true
	if s.quitDialog.closeWindow != nil {
		s.quitDialog.closeWindow()
		return
	}
	if s.win != nil {
		s.win.Close()
	}
}

func (s *viewState) quitServiceDialog(c *ui.Context) {
	if !s.quitDialog.open {
		studioDialog(c, &s.quitDialog.open, components.DialogOptions{Label: tr(c, "services.quit_dialog.title")}, nil)
		return
	}
	t := tokens(c)
	title := tr(c, "services.quit_dialog.title")
	active := s.activeStandaloneServices()
	count := len(active)
	if count == 0 {
		count = 1
	}
	msg := trf(c, "services.quit_dialog.message_multi", "count", count)
	if count == 1 && len(active) == 1 {
		msg = trf(c, "services.quit_dialog.message", "model", s.serviceModelTitle(c, active[0]))
	}
	studioDialog(c.Key("quit-service-confirm-dialog"), &s.quitDialog.open, components.DialogOptions{Label: title}, func(panel ui.Element) {
		panel.Role(ui.RoleAlertDialog)
		components.Text(c, title).Bold()
		components.Text(c, msg).TextColor(t.Color.FgSecondary)
		ui.Row(c).FillWidth().Wrap().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if button(c, tr(c, "services.quit_dialog.cancel")).Clicked() {
				s.quitDialog.open = false
			}
			if dangerButton(c, tr(c, "services.quit_dialog.stop_all_and_quit")).Clicked() {
				s.confirmQuitWithServicePolicy(false)
			}
			if primaryButton(c, tr(c, "services.quit_dialog.keep_and_quit")).Clicked() {
				s.confirmQuitWithServicePolicy(true)
			}
		})
	})
}

func (s *viewState) serviceDrawersAndDialogs(c *ui.Context) {
	s.serviceUsageDrawer(c)
	s.serviceParamsDrawer(c)
	s.quitServiceDialog(c)
}
