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

// newTaskEntry keeps the sidebar creation entry at one full-width geometry in
// every state. Starting a task while work is in flight still asks for
// confirmation, but the confirm prompt never reshapes the entry into a
// half-width outline button inside the 205px sidebar. The idle setup screen
// already offers the same start, so the entry stays disabled there instead of
// duplicating it.
func (s *viewState) newTaskEntry(c *ui.Context) {
	if s.newTaskEntryDisabled() {
		button(c, "新任务").FillWidth().Disabled(true).
			Description("当前已在任务准备页，请在页面内开始测评")
		return
	}
	if s.workInFlight() {
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveNewTask, label: "新任务", confirmLabel: "确认开始新任务",
			prompt:  "将取消正在进行的测评；证据保留在结果目录，本轮不会生成报告。",
			compact: true, fillWidth: true, persist: true,
		}) {
			s.newBenchmark()
		}
		return
	}
	button(c, "新任务").FillWidth().OnClick(func() { s.newBenchmark() })
}

// newTaskEntryDisabled reports whether the entry has nothing to create: the
// idle setup screen is already open and is the place to start the task.
func (s *viewState) newTaskEntryDisabled() bool {
	if s.workInFlight() || s.selected != "dashboard" || s.detailID != "" || s.resumeSource != nil {
		return false
	}
	if s.speed != nil && s.speed.current != nil {
		return false
	}
	return s.preparingTask()
}
