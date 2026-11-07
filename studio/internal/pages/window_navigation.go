package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

// Page navigation also requests engine verification on an otherwise quiet workspace.
func (s *viewState) selectPage(id string) {
	if s.selected == id {
		return
	}
	s.selected = id
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
			button(c, "新任务").OnClick(func() { s.newBenchmark() })
			for _, n := range navigation {
				if components.SidebarItem(c, n.label, n.icon(), s.selected == n.id).Clicked() {
					s.selectPage(n.id)
				}
			}
		})
	})
}
