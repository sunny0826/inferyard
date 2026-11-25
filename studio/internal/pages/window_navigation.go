package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) guardLeaveSettings(targetPage string, action func()) bool {
	target := canonicalPageID(targetPage)
	if canonicalPageID(s.selected) == "settings" && target != "settings" && s.settingsUnsaved() {
		s.pendingNavAction = action
		s.settingsLeaveConfirm = true
		return true
	}
	return false
}

func (s *viewState) resolvePageRedirect(page string) string {
	raw := strings.TrimPrefix(strings.TrimSpace(page), "/")
	switch raw {
	case "dashboard":
		return "benchmarks"
	case "models":
		s.libraryTab = 0
		return "library"
	case "downloads":
		if s.downloadsUI != nil && s.downloadsUI.activeTab == 0 {
			s.libraryTab = 1
		} else {
			s.libraryTab = 0
		}
		return "library"
	case "device":
		s.libraryHwExpanded = true
		return "library"
	default:
		return canonicalPageID(raw)
	}
}

func (s *viewState) navigateToNow(page, detail string) {
	resolved := s.resolvePageRedirect(page)
	if resolved != "reports" && s.openedReport != nil {
		s.openedReport.releaseSVGStage()
	}
	s.pendingDestructive = destructiveRequest{}
	s.selected, s.detailID = resolved, detail
	s.promptPage = resolved
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
	resolved := s.resolvePageRedirect(id)
	detail := s.detailID
	if resolved != "benchmarks" {
		detail = ""
	}
	s.navigateToNow(resolved, detail)
	if resolved != "library" || s.service == nil {
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
	resolved := canonicalPageID(id)
	if canonicalPageID(s.selected) == resolved && id == resolved {
		if resolved == "benchmarks" {
			s.detailID = ""
		}
		return
	}
	if s.guardLeaveSettings(resolved, func() { s.selectPageNow(id) }) {
		return
	}
	s.selectPageNow(id)
}

func (s *viewState) leftNav(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).Fill().MinWidth(0).Padding(sidebar.HeaderHeight(c), 0, t.Density.Row, 0).Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(t.Density.Tight, t.Density.Row).Gap(t.Density.Row).Children(func() {
				if s.service != nil {
					if paths := s.service.BoundPaths(); paths != nil {
						ui.Column(c).FillWidth().Padding(0, t.Density.Row, t.Density.Tight, t.Density.Row).Gap(t.Density.Hairline).Children(func() {
							components.Text(c, tr(c, "shell.nav.dev_env")).Bold().FontSize(t.Type.ShellLabel)
							components.Text(c, paths.SourceLabel).FontSize(t.Type.ShellDetail).TextColor(t.Color.FgSecondary)
						})
					}
				}
				ui.Column(c).FillWidth().Gap(t.Density.Hairline).Children(func() {
					for _, n := range navigation {
						n := n
						var trailing func()
						if n.id == "benchmarks" {
							trailing = func() { s.newTaskPlusButton(c) }
						}
						s.sidebarNavRow(c, n.id, tr(c, n.labelKey), n.icon, trailing)
					}
				})
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Column(c).FillWidth().Padding(t.Density.Tight, t.Density.Row, 0, t.Density.Row).Gap(2).Children(func() {
			s.sidebarStatusArea(c)
			ui.Box(c).FillWidth().Padding(t.Density.Tight, 0).Children(func() {
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
			})
			s.sidebarNavRow(c, "settings", tr(c, "nav.settings"), lucide.Settings, nil)
		})
	})
}

func (s *viewState) sidebarNavRow(c *ui.Context, id, label string, icon func() *ui.SVG, trailing func()) {
	t := tokens(c)
	active := canonicalPageID(s.selected) == id
	onSelect := func() {
		s.pendingDestructive = destructiveRequest{}
		if id == "benchmarks" {
			s.detailID = ""
		}
		s.selectPage(id)
	}
	fg := t.Color.FgSecondary
	if active {
		fg = t.Color.FgPrimary
	}
	if trailing == nil {
		row := ui.Row(c.Key("nav-"+id)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Inset, t.Density.Row).Radius(t.Radius.Control).
			Role(ui.RoleButton).Focusable().Label(label)
		if active {
			row.Background(t.Color.AccentSelection)
		} else if row.Hovered() {
			row.Background(t.Color.SurfaceHover)
		}
		if row.Clicked() {
			onSelect()
		}
		row.Children(func() {
			sidebarIconSlot(c, func() {
				components.Icon(c, icon).Size(15, 15).TextColor(fg)
			})
			txt := ui.Text(c, label).FontSize(t.Type.Body).TextColor(fg).Grow(1).SingleLine()
			if active {
				txt.Bold()
			}
		})
		return
	}
	row := ui.Row(c.Key("nav-"+id)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
		Padding(t.Density.Inset, t.Density.Row).Radius(t.Radius.Control)
	if active {
		row.Background(t.Color.AccentSelection)
	} else if row.Hovered() {
		row.Background(t.Color.SurfaceHover)
	}
	row.Children(func() {
		btn := ui.Row(c.Key("nav-btn-"+id)).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(t.Density.Row).
			Role(ui.RoleButton).Focusable().Label(label)
		if btn.Clicked() {
			onSelect()
		}
		btn.Children(func() {
			sidebarIconSlot(c, func() {
				components.Icon(c, icon).Size(15, 15).TextColor(fg)
			})
			txt := ui.Text(c, label).FontSize(t.Type.Body).TextColor(fg).Grow(1).SingleLine()
			if active {
				txt.Bold()
			}
		})
		trailing()
	})
}

func (s *viewState) newTaskPlusButton(c *ui.Context) {
	hint := s.newTaskEntryHintFor(contextLocale(c))
	label := tr(c, "shell.nav.new_task")
	if hint == "" {
		plus := iconButton(c, lucide.Plus, label, components.IconButtonCompact).Size(22, 22).Padding(0).AlignSelf(ui.Center)
		if plus.Clicked() {
			if s.guardLeaveSettings("benchmarks", func() { s.newBenchmark() }) {
				return
			}
			s.newBenchmark()
		}
		return
	}
	wrap := ui.Box(c).AlignSelf(ui.Center)
	wrap.Children(func() {
		iconButton(c, lucide.Plus, label, components.IconButtonCompact).Size(22, 22).Padding(0).Disabled(true).Description(hint)
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
	case canonicalPageID(s.selected) == "benchmarks" && s.benchFlowOpen && s.detailID == "" && s.resumeSource == nil &&
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
