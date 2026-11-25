package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) guardLeaveSettings(targetPage string, action func()) bool {
	if s.selected == "settings" && targetPage != "settings" && s.settingsUnsaved() {
		s.pendingNavAction = action
		s.settingsLeaveConfirm = true
		return true
	}
	return false
}

func (s *viewState) navigateToNow(page, detail string) {
	if page != "reports" && s.openedReport != nil {
		s.openedReport.releaseSVGStage()
	}
	s.pendingDestructive = destructiveRequest{}
	s.selected, s.detailID = page, detail
	s.promptPage = page
}

func (s *viewState) navigateTo(page, detail string) {
	if s.guardLeaveSettings(page, func() { s.navigateToNow(page, detail) }) {
		return
	}
	s.navigateToNow(page, detail)
}

func (s *viewState) openGeneralSettings() {
	s.settingsTab = settingsTabGeneral
	s.navigateTo("settings", "")
}

func (s *viewState) selectPageNow(id string) {
	s.navigateToNow(id, s.detailID)
	if id != "models" || s.service == nil {
		return
	}
	go func() {
		s.service.VerifyEngines()
		if s.win != nil {
			s.win.Update(func() { s.checks = s.service.EngineChecks() })
		}
	}()
}

func (s *viewState) selectPage(id string) {
	if s.selected == id {
		return
	}
	if s.guardLeaveSettings(id, func() { s.selectPageNow(id) }) {
		return
	}
	s.selectPageNow(id)
}

func (s *viewState) leftNav(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).FillWidth().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		components.Sidebar(c).Children(func() {
			if s.service != nil {
				if paths := s.service.BoundPaths(); paths != nil {
					components.Text(c, tr(c, "shell.nav.dev_env")).Bold().FontSize(t.Type.ShellLabel).
						Padding(t.Density.Tight, t.Density.Row, 0, t.Density.Row)
					components.Text(c, paths.SourceLabel).FontSize(t.Type.ShellDetail).
						Padding(0, t.Density.Row, t.Density.Row, t.Density.Row)
				}
			}
			ui.Column(c).FillWidth().Padding(t.Density.Tight, t.Density.Row, t.Density.Row, t.Density.Row).Children(func() {
				s.newTaskEntry(c)
			})
			for _, n := range navigation {
				if components.SidebarItem(c, tr(c, n.labelKey), n.icon(), s.selected == n.id).Clicked() {
					s.pendingDestructive = destructiveRequest{}
					s.selectPage(n.id)
				}
			}
		})
	})
}

func newTaskButton(c *ui.Context, disabled bool) ui.Element {
	t := tokens(c)
	variant := components.ButtonPrimary
	fg := t.Color.FgOnAccent
	if disabled {
		variant = components.ButtonOutline
		fg = t.Color.FgSecondary
	}
	label := tr(c, "shell.nav.new_task")
	b := components.Button(c, "", variant).FillWidth().Label(label).
		Padding(t.Density.Row, t.Density.Stack).Radius(t.Radius.Control)
	if disabled {
		b.Background(t.Color.Surface.Alpha(.65)).Border(1, t.Color.BorderDefault).Disabled(true)
	}
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, lucide.Plus).Size(14, 14).TextColor(fg)
			ui.Text(c, label).Bold().FontSize(t.Type.Body).TextColor(fg).SingleLine()
		})
	})
	return b
}

func (s *viewState) newTaskEntry(c *ui.Context) {
	hint := s.newTaskEntryHintFor(contextLocale(c))
	if hint == "" {
		newTaskButton(c, false).OnClick(func() {
			if s.guardLeaveSettings("dashboard", func() { s.newBenchmark() }) {
				return
			}
			s.newBenchmark()
		})
		return
	}
	wrap := ui.Column(c).FillWidth()
	wrap.Children(func() {
		newTaskButton(c, true).Description(hint)
	})
	components.Tooltip(c, wrap, hint)
}

func (s *viewState) newTaskEntryHint() string {
	return s.newTaskEntryHintFor("")
}

func (s *viewState) newTaskEntryHintFor(locale string) string {
	switch {
	case s.environmentMissing():
		return l10n.T(locale, "shell.nav.new_task_hint.env_missing")
	case s.roundWorkActive():
		return l10n.T(locale, "shell.nav.new_task_hint.round_active")
	case s.selected == "dashboard" && s.detailID == "" && s.resumeSource == nil &&
		(s.speed == nil || s.speed.current == nil) && s.preparingTask():
		return l10n.T(locale, "shell.nav.new_task_hint.already_preparing")
	}
	return ""
}

func (s *viewState) roundWorkActive() bool {
	if s.busy || s.newTaskPending || s.binding || s.running() != nil || s.queueActivity().active() {
		return true
	}
	if s.engineState.Origin != core.EngineOriginStandalone && (s.engineState.State == "starting" || s.engineState.State == "stopping") {
		return true
	}
	if s.speed != nil && s.speed.current != nil && s.speed.current.active() {
		return true
	}
	switch s.task.stage {
	case "", "prepare_candidate", "done":
		return false
	}
	return s.task.failure == nil && s.engineState.State != "failed"
}
