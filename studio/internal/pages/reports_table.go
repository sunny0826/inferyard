package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) reportsTableView(c *ui.Context) {
	if s.reportsError != nil {
		errorRecoveryBanner(c, s.reportsError, func() {
			button(c, "重试读取报告").OnClick(s.refreshAction)
		})
		return
	}
	if !s.loaded {
		ui.Text(c, "正在读取报告列表…")
		return
	}
	if len(s.reports) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.FileText,
			Title:       "暂无报告",
			Description: "暂无报告。可新建任务，或从历史运行生成报告。",
			Surface:     emptyCard,
			Action: func() {
				primaryButton(c, "新任务").OnClick(s.newBenchmark)
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
		ui.Text(c, reportHeading(report)).Bold()
		ui.Text(c, reportSubheading(report)).TextColor(c.Theme().TextMuted)
		if report.Unreadable {
			ui.Text(c, "无法读取；打开后可重试或返回列表。").TextColor(stateColor(c, stateDanger))
		} else {
			for _, run := range report.RunSummaries {
				ui.Text(c, reportRunLine(report, run)).Selectable()
			}
		}
		primaryButton(c, "打开报告").Label("打开报告 · " + report.ReportId).OnClick(open)
		componentFoldKeyed(c, "report-"+report.ReportId, "报告详情", func() {
			ui.Text(c, "报告 ID："+report.ReportId).Selectable()
			ui.Text(c, "报告文件修改时间："+configuredValue(formatTimestampUTC(emptyText(report.ModifiedAt)))).Selectable()
			for _, id := range report.RunIds {
				ui.Text(c, "来源运行："+id).Selectable()
			}
			for _, path := range report.RunPaths {
				ui.Text(c, "来源路径："+reportSource(path)).Selectable()
			}
			ui.Text(c, displayName(report.Path)).Selectable().TextColor(c.Theme().TextMuted)
		})
	})
}

func reportHeading(report core.ReportSummary) string {
	if report.Unreadable {
		return "无法读取的报告 · " + report.ReportId
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
	modified := "报告文件修改时间未记录"
	if report.ModifiedAt != nil {
		modified = "报告文件修改时间 " + displayValue(formatTimestamp(*report.ModifiedAt))
	}
	runs := fmt.Sprintf("%d 次运行", len(report.RunIds))
	if len(report.RunSummaries) > 1 {
		return runs + " · " + modified
	}
	if label := reportBundleLabel(report); label != "" {
		return label + " · " + runs + " · " + modified
	}
	return runs + " · " + modified
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
	prefix := ""
	if run.Model != "" && (len(report.RunSummaries) > 1 || reportText(run.Model) != reportHeading(report)) {
		prefix = reportText(run.Model) + " · "
	}
	if run.BundleLabel != "" && len(report.RunSummaries) > 1 {
		prefix += displayValue(run.BundleLabel) + " · "
	}
	quality := run.Quality
	if quality.Numerator == nil || quality.Denominator == nil {
		return prefix + "答案通过未记录"
	}
	line := prefix + fmt.Sprintf("答案通过 %s", reportRate(quality.StudioCounts))
	if !quality.Complete {
		line += "（仅已记录通过数）"
	}
	return line
}

func reportSource(path *string) string {
	if path == nil {
		return "来源不可达"
	}
	return displayName(*path)
}
