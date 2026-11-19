package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

// navigateTo is the single page-switch entry: it drops an armed confirmation,
// because a prompt belongs to the page that opened it. Sidebar items and every
// header popover jump go through it, so a jump never leaves an armed click on
// the page that appears next.
func (s *viewState) navigateTo(page, detail string) {
	s.pendingDestructive = destructiveRequest{}
	s.selected, s.detailID = page, detail
	s.promptPage = page
}

// Page navigation also requests engine verification on an otherwise quiet workspace.
func (s *viewState) selectPage(id string) {
	if s.selected == id {
		return
	}
	s.navigateTo(id, s.detailID)
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

func (s *viewState) leftNav(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).FillWidth().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		components.Sidebar(c).Children(func() {
			components.Text(c, "InferYard Studio").Bold().FontSize(t.Type.Body).
				Padding(t.Density.Row, t.Density.Row, t.Density.Tight, t.Density.Row).SingleLine()
			if s.service != nil {
				if paths := s.service.BoundPaths(); paths != nil {
					components.Text(c, "开发环境").Bold().FontSize(t.Type.ShellLabel).
						Padding(0, t.Density.Row, 0, t.Density.Row)
					components.Text(c, paths.SourceLabel).FontSize(t.Type.ShellDetail).
						Padding(0, t.Density.Row, t.Density.Row, t.Density.Row)
				}
			}
			ui.Column(c).FillWidth().Padding(t.Density.Tight, t.Density.Row, t.Density.Row, t.Density.Row).Children(func() {
				s.newTaskEntry(c)
			})
			for _, n := range navigation {
				if components.SidebarItem(c, n.label, n.icon(), s.selected == n.id).Clicked() {
					s.pendingDestructive = destructiveRequest{}
					s.selectPage(n.id)
				}
			}
		})
	})
}

// newTaskEntry draws the sidebar creation entry at one full-width geometry in
// every state and never turns it into a destructive control. The 205 DIP column
// cannot host an inline confirmation without pushing the navigation items down,
// so a state that has nothing to create disables the entry with a hint instead:
// environment setup happens on the task page, and an active run is cancelled
// there before a new round starts.
func (s *viewState) newTaskEntry(c *ui.Context) {
	if hint := s.newTaskEntryHint(); hint != "" {
		button(c, "新任务").FillWidth().Disabled(true).Description(hint)
		return
	}
	button(c, "新任务").FillWidth().OnClick(func() { s.newBenchmark() })
}

// newTaskEntryHint explains why the entry cannot start a round right now; an
// empty hint means it can.
func (s *viewState) newTaskEntryHint() string {
	switch {
	case s.environmentMissing():
		return "先完成环境准备，才能进行测评"
	case s.workInFlight():
		return "测评进行中，请在任务页取消后再新建任务"
	case s.selected == "dashboard" && s.detailID == "" && s.resumeSource == nil &&
		(s.speed == nil || s.speed.current == nil) && s.preparingTask():
		return "已在任务准备页，请在页面内开始测评"
	}
	return ""
}
