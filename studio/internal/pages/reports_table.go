package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) reportsTableView(c *ui.Context) {
	if s.reportsError != nil {
		errorRecoveryBanner(c, s.reportsError, func() {
			button(c, tr(c, "reports.list.retry_load")).OnClick(s.refreshAction)
		})
		return
	}
	if !s.loaded {
		ui.Text(c, tr(c, "reports.list.loading"))
		return
	}
	if len(s.reports) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.FileText,
			Title:       tr(c, "reports.list.empty_title"),
			Description: tr(c, "reports.list.empty_description"),
			Surface:     emptyCard,
			Action: func() {
				primaryButton(c, tr(c, "reports.action.new_task")).OnClick(s.newBenchmark)
			},
		})
		return
	}
	for _, report := range s.reports {
		reportCard(c, report, func() { s.openReport(report.ReportId) })
	}
}

func reportCard(c *ui.Context, report core.ReportSummary, open func()) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key(report.ReportId)).MinWidth(0).Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Text(c, reportHeadingFor(c, report)).Bold()
		ui.Text(c, reportSubheadingFor(c, report)).TextColor(c.Theme().TextMuted)
		if report.Unreadable {
			ui.Text(c, tr(c, "reports.list.unreadable_body")).TextColor(stateColor(c, stateDanger))
		} else {
			for _, run := range report.RunSummaries {
				ui.Text(c, reportRunLineFor(c, report, run)).Selectable()
			}
		}
		primaryButton(c, tr(c, "reports.list.open")).Label(trf(c, "reports.list.open_aria", "id", report.ReportId)).OnClick(open)
	})
}

func reportHeading(report core.ReportSummary) string {
	return reportHeadingFor(nil, report)
}

func reportHeadingFor(c *ui.Context, report core.ReportSummary) string {
	if report.Unreadable {
		return trf(c, "reports.list.unreadable_heading", "id", report.ReportId)
	}
	if len(report.RunSummaries) == 0 {
		return report.ReportId
	}
	first := report.RunSummaries[0]
	if title := emptyText(first.Title); title != "" {
		return title
	}
	return reportText(first.Model)
}

func reportSubheading(report core.ReportSummary) string {
	return reportSubheadingFor(nil, report)
}

func reportSubheadingFor(c *ui.Context, report core.ReportSummary) string {
	parts := []string{}
	if len(report.RunSummaries) <= 1 {
		if label := reportBundleLabel(report); label != "" {
			parts = append(parts, label)
		}
	}
	parts = append(parts, trf(c, "reports.list.run_count", "count", len(report.RunIds)))
	if report.ModifiedAt != nil {
		parts = append(parts, trf(c, "reports.list.updated_inline", "time", displayValue(formatTimestamp(*report.ModifiedAt))))
	}
	return joinParts(parts)
}

func reportBundleLabel(report core.ReportSummary) string {
	for _, run := range report.RunSummaries {
		if run.BundleLabel != "" {
			return displayValue(run.BundleLabel)
		}
	}
	return ""
}

func reportRunLine(report core.ReportSummary, run core.ReportListRun) string {
	return reportRunLineFor(nil, report, run)
}

func reportRunLineFor(c *ui.Context, report core.ReportSummary, run core.ReportListRun) string {
	prefix := ""
	if run.Model != "" && (len(report.RunSummaries) > 1 || reportText(run.Model) != reportHeadingFor(c, report)) {
		prefix = reportText(run.Model) + " · "
	}
	if run.BundleLabel != "" && len(report.RunSummaries) > 1 {
		prefix += displayValue(run.BundleLabel) + " · "
	}
	quality := run.Quality
	if quality.Numerator == nil || quality.Denominator == nil {
		return prefix + tr(c, "reports.list.pass_unrecorded")
	}
	line := prefix + trf(c, "reports.list.pass_rate", "rate", reportRate(quality.StudioCounts))
	if !quality.Complete {
		line = trf(c, "reports.list.pass_rate_partial", "line", line)
	}
	return line
}
