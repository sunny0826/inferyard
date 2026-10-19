package pages

import (
	"os"
	"path/filepath"
	"strings"

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

func studioOrigin() string {
	exe, err := os.Executable()
	if err != nil {
		return "Studio 来源未知"
	}
	if strings.Contains(filepath.ToSlash(exe), "/go-build") {
		return "Studio 源码运行 · " + exe
	}
	return "Studio 发行包 · " + exe
}

func (s *viewState) leftNav(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		components.Sidebar(c).Children(func() {
			components.Text(c, "InferYard Studio").Bold().FontSize(t.Rem(1)).
				Padding(t.Space(2), t.Space(2), t.Space(1), t.Space(2)).SingleLine()
			if s.service != nil {
				if paths := s.service.BoundPaths(); paths != nil {
					components.Text(c, "开发环境").Bold().FontSize(t.Rem(0.75)).
						Padding(0, t.Space(2), 0, t.Space(2))
					components.Text(c, paths.SourceLabel).FontSize(t.Rem(0.625)).
						Padding(0, t.Space(2), t.Space(2), t.Space(2))
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
