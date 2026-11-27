package pages

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

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
	rowR := concentricRadius(t.Radius.Surface, t.Density.Tight)
	ui.Box(c.Key("bench-recent-run-" + runPath)).FillWidth().Children(func() {
		listRow(c, listRowOptions{
			Radius:    rowR,
			BoldTitle: true,
			Leading:   func() { modelIconBox(c, s.modelIconHint(modelTitle), 32, t.Radius.Control) },
			Title:     title,
			Subtitle:  strings.Join(subParts, " · "),
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

func (s *viewState) renderRecentReportRow(c *ui.Context, rep core.ReportSummary) {
	t := tokens(c)
	repCopy := rep
	rowR := concentricRadius(t.Radius.Surface, t.Density.Tight)
	ui.Column(c.Key("bench-recent-rep-" + rep.ReportId)).FillWidth().Children(func() {
		listRow(c, listRowOptions{
			Radius:    rowR,
			BoldTitle: true,
			Leading: func() {
				ui.Box(c).Size(32, 32).Radius(t.Radius.Control).Shrink(0).AlignSelf(ui.Center).
					Background(t.Color.Sunken).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					studioIcon(c, reicon.FileText, t.Color.FgSecondary)
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
	var parts []string
	if speed := task.ContextSpeed; task.Kind == "context_speed" && speed != nil {
		if engine := cmp.Or(strings.TrimSpace(speed.DisplayEngine), strings.TrimSpace(speed.Engine)); engine != "" {
			parts = append(parts, engine)
		}
	}
	if terminal(task.State) {
		if task.StartedAt != nil && *task.StartedAt != "" {
			parts = append(parts, formatTimestamp(*task.StartedAt))
		}
		return strings.Join(parts, " · ")
	}
	parts = append(parts, taskLabelFor(contextLocale(c), task))
	if task.Queued || task.State == "queued" {
		if task.StartedAt != nil && *task.StartedAt != "" {
			parts = append(parts, formatTimestamp(*task.StartedAt))
		}
		return strings.Join(parts, " · ")
	}
	sub := strings.Join(parts, " · ")
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
