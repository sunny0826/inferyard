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
			Description: "可新建任务，或从历史运行生成报告。",
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
			ui.Text(c, "报告文件无法读取").TextColor(stateColor(c, stateDanger))
		} else {
			for _, run := range report.RunSummaries {
				ui.Text(c, reportRunLine(report, run)).Selectable()
			}
		}
		primaryButton(c, "打开报告").Label("打开报告 · " + report.ReportId).OnClick(open)
		componentFoldKeyed(c, "report-"+report.ReportId, "报告详情", func() {
			ui.Text(c, "报告 ID："+report.ReportId).Selectable()
			ui.Text(c, "更新时间："+configuredValue(formatTimestampUTC(emptyText(report.ModifiedAt)))).Selectable()
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
	parts := []string{}
	if len(report.RunSummaries) <= 1 {
		if label := reportBundleLabel(report); label != "" {
			parts = append(parts, label)
		}
	}
	parts = append(parts, fmt.Sprintf("%d 次运行", len(report.RunIds)))
	if report.ModifiedAt != nil {
		parts = append(parts, "更新于 "+displayValue(formatTimestamp(*report.ModifiedAt)))
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
		line += "（部分评分）"
	}
	return line
}

func reportSource(path *string) string {
	if path == nil {
		return "来源不可达"
	}
	return displayName(*path)
}
