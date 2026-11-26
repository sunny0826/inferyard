package pages

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) openBenchmarksFlow() {
	s.benchFlowOpen = true
	s.navigateTo("benchmarks", "")
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
		t := tokens(c)
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if outlineButton(c, tr(c, "benchmarks.action.back_list")).Clicked() {
				s.benchFlowOpen = false
			}
			ui.Box(c).Grow(1)
			if outlineButton(c, tr(c, "reports.tab.history")).Clicked() {
				s.benchFlowOpen = false
				s.benchmarksTab = benchmarksTabHistory
			}
		})
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
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		s.header(c, tr(c, "benchmarks.page.title"))
		ui.Box(c).Grow(1)
		if s.hasActiveBenchmarkFlow() {
			if outlineButton(c, tr(c, "benchmarks.action.open_flow")).Clicked() {
				s.benchFlowOpen = true
				s.benchmarksTab = benchmarksTabTasks
			}
		}
		hint := s.newTaskEntryHintFor(contextLocale(c))
		if hint == "" {
			if primaryButton(c, tr(c, "benchmarks.action.new")).Clicked() {
				s.newBenchmark()
			}
		} else {
			wrap := ui.Box(c)
			wrap.Children(func() {
				primaryButton(c, tr(c, "benchmarks.action.new")).Disabled(true).Description(hint)
			})
			components.Tooltip(c, wrap, hint)
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
			ui.Text(c, tr(c, "benchmarks.group.running_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
			ui.Text(c, tr(c, "benchmarks.group.queued_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
	})

	historyRuns := s.historyRuns()
	ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "benchmarks.group.recent"), 3).Grow(1)
			if len(historyRuns) > 0 {
				if inlineButton(c, components.ButtonGhost, tr(c, "benchmarks.recent.view_all_history")).Clicked() {
					s.benchmarksTab = benchmarksTabHistory
				}
			}
		})
		found := false
		seenRuns := map[string]bool{}
		for _, task := range s.tasks {
			if !terminal(task.State) {
				continue
			}
			found = true
			if p := cleanMatchPath(emptyText(task.RunPath)); p != "" {
				seenRuns[p] = true
			}
			if task.TaskId != "" {
				seenRuns[task.TaskId] = true
			}
			if rid := emptyText(task.RunId); rid != "" {
				seenRuns[rid] = true
			}
			s.renderBenchmarkTaskRow(c, task)
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
				found = true
				speedCount++
				seenRuns[r.id] = true
				s.renderBenchmarkTaskRow(c, r.toRunTask())
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
			found = true
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
			s.renderRecentReportRow(c, rep)
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
			found = true
			historyCount++
			if p != "" {
				seenRuns[p] = true
			}
			if r.RunId != "" {
				seenRuns[r.RunId] = true
			}
			s.renderRecentHistoryRunRow(c, r)
		}
		if !found {
			ui.Text(c, tr(c, "benchmarks.group.recent_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
	})
}

func (s *viewState) renderRecentHistoryRunRow(c *ui.Context, r core.RunSummary) {
	t := tokens(c)
	loc := contextLocale(c)
	modelTitle := historyModel(r)
	title := modelTitle
	if b := strings.TrimSpace(emptyText(r.Bundle)); b != "" {
		title = modelTitle + " · " + b
	}
	subParts := []string{runScore(r)}
	if started := historyStarted(r); started != "—" {
		subParts = append(subParts, started)
	}
	runPath := r.Path
	cardEl, rowR := compactCard(c, ui.Column(c.Key("bench-recent-run-"+runPath)).FillWidth(), t.Density.Tight)
	iconR := concentricRadius(rowR, t.Density.Row)
	cardEl.Children(func() {
		listRow(c, listRowOptions{
			Radius:   rowR,
			Leading:  func() { modelIconBox(c, modelTitle, 26, iconR) },
			Title:    title,
			Subtitle: strings.Join(subParts, " · "),
			Trailing: func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					if s.canResumeHistoryRun(r) {
						inlineBadge(c, tr(c, "dashboard.history.badge_resumable"), stateWarning, badgeOptions{Size: sizeSM})
					}
					inlineBadge(c, stateLabelFor(loc, r.Status), historyStatusKind(r.Status), badgeOptions{Size: sizeSM, Style: badgeDot})
				})
			},
			OnClick: func() { s.openHistoryRunDetail(runPath) },
		})
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
			Radius:   rowR,
			Leading:  func() { engineIconBox(c, s.engine, 26, iconR) },
			Title:    title,
			Subtitle: sub,
			Trailing: func() {
				inlineBadge(c, tr(c, "benchmarks.badge.active_flow"), stateActive, badgeOptions{Size: sizeSM, Style: badgeDot})
			},
			OnClick: func() { s.benchFlowOpen = true },
		})
	})
}

func (s *viewState) renderBenchmarkTaskRow(c *ui.Context, task core.RunTask) {
	t := tokens(c)
	title := tr(c, "shell.header.context_speed")
	if task.Bundle != nil && *task.Bundle != "" {
		title = *task.Bundle
	}
	if rp := emptyText(task.RunPath); rp != "" {
		base := filepath.Base(rp)
		if base != "" && base != "." {
			title = base + " · " + title
		}
	}
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
	cardEl, rowR := compactCard(c, ui.Column(c.Key("bench-task-"+task.TaskId)).FillWidth(), t.Density.Tight)
	iconR := concentricRadius(rowR, t.Density.Row)
	cardEl.Children(func() {
		listRow(c, listRowOptions{
			Radius:   rowR,
			Leading:  func() { engineIconBox(c, s.engine, 26, iconR) },
			Title:    title,
			Subtitle: sub,
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
	})
}

func (s *viewState) renderRecentReportRow(c *ui.Context, rep core.ReportSummary) {
	t := tokens(c)
	repCopy := rep
	cardEl, rowR := compactCard(c, ui.Column(c.Key("bench-recent-rep-"+rep.ReportId)).FillWidth(), t.Density.Tight)
	iconR := concentricRadius(rowR, t.Density.Row)
	cardEl.Children(func() {
		listRow(c, listRowOptions{
			Radius: rowR,
			Leading: func() {
				ui.Box(c).Size(26, 26).Radius(iconR).Shrink(0).AlignSelf(ui.Center).
					Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					components.Icon(c, lucide.FileText).Size(14, 14).TextColor(t.Color.FgSecondary)
				})
			},
			Title:    reportHeadingFor(c, repCopy),
			Subtitle: reportSubheadingFor(c, repCopy),
			Trailing: func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					if !repCopy.Unreadable {
						exclusivityBadge(c, s.reportExclusivity(repCopy.ReportId))
					}
					inlineBadge(c, tr(c, "benchmarks.badge.report"), stateSuccess, badgeOptions{Size: sizeSM})
				})
			},
			OnClick: func() { s.openReport(repCopy.ReportId) },
		})
	})
}

func (s *viewState) benchmarkTaskSubtitle(c *ui.Context, task core.RunTask) string {
	sub := taskLabelFor(contextLocale(c), task)
	if terminal(task.State) || task.Queued || task.State == "queued" {
		if task.StartedAt != nil && *task.StartedAt != "" {
			sub += " · " + formatTimestamp(*task.StartedAt)
		}
		return sub
	}
	if task.ContextSpeed != nil && task.ContextSpeed.Stage == "measuring" {
		sub += " · " + trf(c, "speed.task.completed_count", "completed", task.ContextSpeed.Completed)
	} else if active := s.running(); active != nil && active.TaskId == task.TaskId && s.runProgress != nil && s.runProgress.Total != nil && *s.runProgress.Total > 0 {
		sub += " · " + trf(c, "dashboard.run.cases_progress", "done", s.runProgress.Done, "total", *s.runProgress.Total)
	}
	if task.State != "cancelling" && !task.AwaitingService {
		if elapsed := s.formatTaskElapsed(c, emptyText(task.StartedAt)); elapsed != "" {
			return sub + " · " + elapsed
		}
	}
	if task.StartedAt != nil && *task.StartedAt != "" {
		sub += " · " + formatTimestamp(*task.StartedAt)
	}
	return sub
}

func reportAlreadyListed(rep core.ReportSummary, seen map[string]bool) bool {
	if rep.ReportId != "" && seen[rep.ReportId] {
		return true
	}
	for _, run := range rep.RunSummaries {
		if run.RunID != "" && seen[run.RunID] {
			return true
		}
		if strings.TrimSpace(run.RunID) != "" {
			for k := range seen {
				if strings.Contains(k, run.RunID) {
					return true
				}
			}
		}
	}
	return false
}
