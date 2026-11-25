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
	if s.benchFlowOpen {
		t := tokens(c)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if outlineButton(c, tr(c, "benchmarks.action.back_list")).Clicked() {
				s.benchFlowOpen = false
			}
		})
		s.dashboardView(c)
		return
	}
	s.benchmarksListView(c)
}

func (s *viewState) benchmarksListView(c *ui.Context) {
	t := tokens(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		s.header(c, tr(c, "benchmarks.page.title"))
		ui.Box(c).Grow(1)
		if s.hasActiveBenchmarkFlow() {
			if outlineButton(c, tr(c, "benchmarks.action.open_flow")).Clicked() {
				s.benchFlowOpen = true
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
		if !found && s.hasActiveBenchmarkFlow() {
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

	section(c, tr(c, "benchmarks.group.recent"), func() {
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
			s.renderBenchmarkTaskRow(c, task)
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
			s.renderRecentReportRow(c, rep)
		}
		if !found {
			ui.Text(c, tr(c, "benchmarks.group.recent_empty")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
