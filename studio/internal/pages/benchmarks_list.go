package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) openBenchmarksFlow() {
	s.benchFlowOpen = true
	s.navigateTo("benchmarks", "")
}

func (s *viewState) flowHeader(c *ui.Context, title string) {
	t := tokens(c)
	studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
		s.header(c, title)
		if s.benchFlowOpen && !s.newTaskPending && !s.environmentMissing() {
			ui.Box(c).Grow(1)
			if outlineIconButton(c, reicon.ArrowLeft, tr(c, "benchmarks.action.back_list")).Clicked() {
				s.benchFlowOpen = false
			}
			if outlineIconButton(c, reicon.List, tr(c, "reports.tab.history")).Clicked() {
				s.benchFlowOpen = false
				s.benchmarksTab = benchmarksTabHistory
			}
		}
	})
}

func (s *viewState) modelArchFor(name string) string {
	trimmed := strings.TrimSpace(name)
	for _, m := range s.registry.Models {
		if m.Name == trimmed || modelLabel(m) == trimmed {
			return m.Architecture
		}
	}
	return ""
}

func (s *viewState) benchmarksView(c *ui.Context) {
	if s.newTaskPending {
		s.dashboardView(c)
		return
	}
	if s.environmentMissing() {
		s.header(c, tr(c, "benchmarks.page.title"))
		s.setupGuide(c)
		return
	}
	defer s.historyOverlays(c)
	if s.benchFlowOpen {
		s.dashboardView(c)
		return
	}
	s.benchmarksListView(c)
}

func (s *viewState) benchmarksListView(c *ui.Context) {
	t := tokens(c)
	if s.benchmarksTab < benchmarksTabTasks || s.benchmarksTab > benchmarksTabHistory {
		s.benchmarksTab = benchmarksTabTasks
	}
	studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
		s.header(c, tr(c, "benchmarks.page.title"))
		ui.Box(c).Grow(1)
		if s.hasActiveBenchmarkFlow() {
			if outlineIconButton(c, reicon.Play, tr(c, "benchmarks.action.open_flow")).Clicked() {
				s.benchFlowOpen = true
				s.benchmarksTab = benchmarksTabTasks
			}
		}
		hint := s.newTaskEntryHintFor(contextLocale(c))
		label := tr(c, "benchmarks.action.new")
		if hint == "" {
			if primaryIconButton(c, reicon.Plus, label).Clicked() {
				s.newBenchmark()
			}
		} else {
			iconButtonWithTooltip(c, hint, func() ui.Element {
				return primaryIconButton(c, reicon.Plus, "").Label(label).Disabled(true)
			})
		}
	})

	ui.Column(c.Key("benchmarks-tabs")).FillWidth().Gap(t.Density.Section).Children(func() {
		components.Tabs(c, &s.benchmarksTab, components.TabsUnderline, tr(c, "benchmarks.tab.tasks"), tr(c, "reports.tab.history"))
		if s.benchmarksTab == benchmarksTabHistory {
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				errorView(c, s.historyError)
				s.historyView(c)
			})
			return
		}
		s.benchmarksTasksTabView(c)
	})
}

func (s *viewState) benchmarksTasksTabView(c *ui.Context) {
	t := tokens(c)
	extraSpeed, hasExtraSpeed := s.unlistedCurrentSpeedTask()

	section(c, tr(c, "benchmarks.group.running"), func() {
		found := false
		for _, task := range s.tasks {
			if terminal(task.State) || task.Queued || task.State == "queued" {
				continue
			}
			found = true
			s.renderBenchmarkTaskRow(c, task)
		}
		if hasExtraSpeed && !terminal(extraSpeed.State) && !extraSpeed.Queued {
			found = true
			s.renderBenchmarkTaskRow(c, extraSpeed)
		}
		if !found && s.shouldRenderActiveFlowRow() {
			found = true
			s.renderActiveFlowRow(c)
		}
		if !found {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
				Padding(t.Density.Hairline, 0).Children(func() {
				studioIcon(c, reicon.Play, t.Color.FgTertiary)
				ui.Text(c, tr(c, "benchmarks.group.running_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
		}
	})

	section(c, tr(c, "benchmarks.group.queued"), func() {
		found := false
		for _, task := range s.tasks {
			if terminal(task.State) || (!task.Queued && task.State != "queued") {
				continue
			}
			found = true
			s.renderBenchmarkTaskRow(c, task)
		}
		if hasExtraSpeed && extraSpeed.Queued {
			found = true
			s.renderBenchmarkTaskRow(c, extraSpeed)
		}
		if !found {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
				Padding(t.Density.Hairline, 0).Children(func() {
				studioIcon(c, reicon.Clock, t.Color.FgTertiary)
				ui.Text(c, tr(c, "benchmarks.group.queued_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
		}
	})

	historyRuns := s.historyRuns()
	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "benchmarks.group.recent"), 3).Grow(1)
			if len(historyRuns) > 0 {
				if iconButton(c, reicon.List, tr(c, "benchmarks.recent.view_all_history"), components.IconButtonCompact).Clicked() {
					s.benchmarksTab = benchmarksTabHistory
				}
			}
		})
		var rowFns []func()
		seenRuns := map[string]bool{}
		for _, task := range s.tasks {
			if !terminal(task.State) {
				continue
			}
			tCopy := task
			if p := cleanMatchPath(emptyText(task.RunPath)); p != "" {
				seenRuns[p] = true
			}
			if task.TaskId != "" {
				seenRuns[task.TaskId] = true
			}
			if rid := emptyText(task.RunId); rid != "" {
				seenRuns[rid] = true
			}
			rowFns = append(rowFns, func() { s.renderBenchmarkTaskRowMode(c, tCopy, false) })
		}
		if s.speed != nil {
			speedCount := 0
			for _, r := range s.speed.history {
				if speedCount >= 5 {
					break
				}
				if r == nil || r.active() || seenRuns[r.id] {
					continue
				}
				speedCount++
				seenRuns[r.id] = true
				rt := r.toRunTask()
				rowFns = append(rowFns, func() { s.renderBenchmarkTaskRowMode(c, rt, false) })
			}
		}
		reportCount := 0
		for _, rep := range s.reports {
			if reportCount >= 5 {
				break
			}
			if reportAlreadyListed(rep, seenRuns) {
				continue
			}
			reportCount++
			for _, rid := range rep.RunIds {
				if rid != "" {
					seenRuns[rid] = true
				}
			}
			for _, rp := range rep.RunPaths {
				if p := cleanMatchPath(emptyText(rp)); p != "" {
					seenRuns[p] = true
				}
			}
			repCopy := rep
			rowFns = append(rowFns, func() { s.renderRecentReportRow(c, repCopy) })
		}
		historyCount := 0
		for _, r := range historyRuns {
			if historyCount >= 5 {
				break
			}
			p := cleanMatchPath(r.Path)
			if (p != "" && seenRuns[p]) || (r.RunId != "" && seenRuns[r.RunId]) {
				continue
			}
			historyCount++
			if p != "" {
				seenRuns[p] = true
			}
			if r.RunId != "" {
				seenRuns[r.RunId] = true
			}
			rCopy := r
			rowFns = append(rowFns, func() { s.renderRecentHistoryRunRow(c, rCopy) })
		}
		if len(rowFns) == 0 {
			ui.Text(c, tr(c, "benchmarks.group.recent_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		} else {
			applyMaterial(c, ui.Column(c.Key("bench-recent-list")).FillWidth().Gap(t.Density.Hairline).Padding(t.Density.Tight), materialCard).Children(func() {
				for _, fn := range rowFns {
					fn()
				}
			})
		}
	})
}

func (s *viewState) unlistedCurrentSpeedTask() (core.RunTask, bool) {
	if s.speed == nil || s.speed.current == nil || !s.speed.current.active() {
		return core.RunTask{}, false
	}
	cur := s.speed.current
	for _, task := range s.tasks {
		if task.TaskId == cur.id {
			return core.RunTask{}, false
		}
	}
	return cur.toRunTask(), true
}

func (s *viewState) shouldRenderActiveFlowRow() bool {
	if s.speed != nil && s.speed.current != nil {
		return false
	}
	if s.binding {
		return true
	}
	if s.task.stage != "" && s.task.stage != "done" {
		for _, task := range s.tasks {
			if !terminal(task.State) {
				return false
			}
		}
		return true
	}
	return false
}

func (s *viewState) hasActiveBenchmarkFlow() bool {
	if s.speed != nil && s.speed.current != nil {
		return true
	}
	if s.binding || s.running() != nil {
		return true
	}
	if s.task.stage != "" && s.task.stage != "done" {
		return true
	}
	return false
}

func (s *viewState) renderActiveFlowRow(c *ui.Context) {
	t := tokens(c)
	title := tr(c, "dashboard.title")
	if s.model != "" {
		title = s.model
	}
	sub := tr(c, "benchmarks.badge.active_flow")
	if s.engine != "" {
		sub = s.engine + " · " + sub
	}
	cardEl, rowR := compactCard(c, ui.Column(c.Key("bench-active-flow")).FillWidth(), t.Density.Tight)
	iconR := concentricRadius(rowR, t.Density.Row)
	cardEl.Children(func() {
		listRow(c, listRowOptions{
			Radius:    rowR,
			BoldTitle: true,
			Leading:   func() { engineIconBox(c, s.engineIconHint(s.engine), 32, iconR) },
			Title:     title,
			Subtitle:  sub,
			Trailing: func() {
				inlineBadge(c, tr(c, "benchmarks.badge.active_flow"), stateActive, badgeOptions{Size: sizeSM, Style: badgeDot})
			},
			OnClick: func() { s.benchFlowOpen = true },
		})
	})
}

func (s *viewState) renderBenchmarkTaskRow(c *ui.Context, task core.RunTask) {
	s.renderBenchmarkTaskRowMode(c, task, true)
}

func (s *viewState) renderBenchmarkTaskRowMode(c *ui.Context, task core.RunTask, wrapCard bool) {
	t := tokens(c)
	title := benchmarkTaskTitle(c, task)
	sub := s.benchmarkTaskSubtitle(c, task)
	kind := stateNeutral
	switch task.State {
	case "running", "starting":
		kind = stateActive
	case "done":
		kind = stateSuccess
	case "failed":
		kind = stateDanger
	case "cancelled":
		kind = stateWarning
	}
	engName := s.engine
	if task.ContextSpeed != nil && task.ContextSpeed.Engine != "" {
		engName = task.ContextSpeed.Engine
	}
	isLive := !terminal(task.State) && !task.Queued && task.State != "queued"
	liveProgress := -1.0
	if isLive {
		if task.ContextSpeed != nil && task.ContextSpeed.Stage == "measuring" {
			liveProgress = float64(task.ContextSpeed.Completed) / 5
		} else if active := s.running(); active != nil && active.TaskId == task.TaskId && s.runProgress != nil && s.runProgress.Total != nil && *s.runProgress.Total > 0 {
			if s.runProgress.Done > 0 || s.runProgress.CurrentCaseId != nil {
				liveProgress = progressValue(s.runProgress.Done, s.runProgress.Total)
			}
		}
	}
	var cardEl ui.Element
	rowR := concentricRadius(t.Radius.Surface, t.Density.Tight)
	if wrapCard {
		cardEl, rowR = compactCard(c, ui.Column(c.Key("bench-task-"+task.TaskId)).FillWidth(), t.Density.Tight)
	} else {
		cardEl = ui.Column(c.Key("bench-task-" + task.TaskId)).FillWidth()
	}
	cardEl.Children(func() {
		listRow(c, listRowOptions{
			Radius:    rowR,
			BoldTitle: true,
			Leading:   func() { engineIconBox(c, s.engineIconHint(engName), 32, t.Radius.Control) },
			Title:     title,
			Subtitle:  sub,
			Trailing: func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					if task.Exclusivity != "" {
						exclusivityBadge(c, task.Exclusivity)
					}
					inlineBadge(c, taskLabelFor(contextLocale(c), task), kind, badgeOptions{Size: sizeSM})
				})
			},
			OnClick: func() {
				if task.Kind == "context_speed" {
					if terminal(task.State) {
						s.openSpeed(task.TaskId)
					} else {
						if s.speed != nil && (s.speed.current == nil || s.speed.current.id != task.TaskId) {
							s.speed.current = speedFromTask(task)
						}
						s.benchFlowOpen = true
					}
					return
				}
				s.selectTask(task.TaskId)
			},
		})
		if isLive {
			ui.Box(c).FillWidth().Padding(0, t.Density.Stack, t.Density.Tight, t.Density.Stack).Children(func() {
				progressBar(c, liveProgress, title).Height(4)
			})
		}
	})
}
