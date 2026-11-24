package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) completionView(c *ui.Context) {
	runs := s.completionRuns()
	path := s.completedReportPath()
	if len(runs) == 0 {
		ui.Text(c, tr(c, "dashboard.completion.empty"))
		s.completionActions(c, path)
		return
	}
	for _, run := range runs {
		card(c, completionRunTitle(run), func() {
			deviceRow(c, tr(c, "dashboard.completion.pass_rate"), runPassRate(run))
			deviceRow(c, tr(c, "dashboard.completion.duration"), runDuration(run))
			deviceRow(c, tr(c, "dashboard.completion.status"), stateLabel(run.Status))
		})
	}
	s.completionActions(c, path)
}

func (s *viewState) completionActions(c *ui.Context, path string) {
	ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
		if path != "" {
			if primaryButton(c, tr(c, "dashboard.completion.view_report")).AlignSelf(ui.Start).Clicked() {
				s.openReport(path)
			}
		} else if primaryButton(c, tr(c, "dashboard.completion.view_reports")).AlignSelf(ui.Start).Clicked() {
			s.selected = "reports"
		}
		if button(c, tr(c, "dashboard.completion.run_again")).AlignSelf(ui.Start).Clicked() {
			s.newBenchmark()
		}
	})
}

func (s *viewState) completedReportPath() string {
	if s.task.report != "" {
		return s.task.report
	}
	if s.progress != nil {
		return emptyText(s.progress.Report.Path)
	}
	return ""
}

func (s *viewState) completionRuns() []core.RunSummary {
	keys := map[string]bool{}
	for _, path := range s.task.runs {
		keys[path] = true
	}
	if len(keys) == 0 && s.progress != nil {
		for _, id := range s.progress.Report.Runs {
			keys[id] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}
	var runs []core.RunSummary
	for _, run := range s.historyRuns() {
		if keys[run.Path] || keys[run.RunId] {
			runs = append(runs, run)
		}
	}
	return runs
}

func completionRunTitle(run core.RunSummary) string {
	name := text(run.Bundle)
	if model := emptyText(run.ModelName); model != "" {
		name = model + " · " + name
	}
	return name
}

func runPassRate(run core.RunSummary) string {
	if run.Score == nil || run.Score.Valid == 0 {
		return "—"
	}
	if gap := run.Score.RateGap(); gap != "" {
		return l10n.TRf("dashboard.completion.pass_rate_gap", "passed", run.Score.Passed, "valid", run.Score.Valid, "gap", gapDescription(*run.Score, gap))
	}
	line := fmt.Sprintf("%d / %d", run.Score.Passed, run.Score.Valid)
	if run.Score.Rate != nil {
		line += fmt.Sprintf(" · %.1f%%", *run.Score.Rate*100)
	}
	return line + failedNote(*run.Score)
}

func failedNote(score core.RunScore) string {
	if score.Failed == 0 {
		return ""
	}
	return l10n.TRf("dashboard.completion.failed_note", "count", score.Failed)
}

func gapDescription(score core.RunScore, gap string) string {
	switch {
	case score.Unscorable > 0:
		return l10n.TRf("dashboard.score.gap_unscorable", "count", score.Unscorable)
	case score.Missing > 0:
		return l10n.TRf("dashboard.score.gap_missing", "count", score.Missing)
	default:
		return l10n.TR("dashboard.score.gap_incomplete")
	}
}

func runDuration(run core.RunSummary) string {
	start, finish := emptyText(run.StartedAt), emptyText(run.FinishedAt)
	if start == "" || finish == "" {
		return "—"
	}
	from, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return "—"
	}
	to, err := time.Parse(time.RFC3339Nano, finish)
	if err != nil || to.Before(from) {
		return "—"
	}
	return formatDuration(to.Sub(from))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return l10n.TRf("dashboard.duration.seconds", "seconds", fmt.Sprintf("%.2f", d.Seconds()))
	}
	d = d.Round(time.Second)
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	seconds := int(d/time.Second) % 60
	if hours > 0 {
		return l10n.TRf("dashboard.duration.hours_minutes", "hours", hours, "minutes", minutes)
	}
	return l10n.TRf("dashboard.duration.minutes_seconds", "minutes", minutes, "seconds", seconds)
}
