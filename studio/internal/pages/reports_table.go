package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) reportsTableView(c *ui.Context) {
	t := tokens(c)
	if s.reportsError != nil {
		errorRecoveryBanner(c, s.reportsError, func() {
			refreshIconButton(c, tr(c, "reports.list.retry_load"), false).OnClick(s.refreshAction)
		})
		return
	}
	if !s.loaded || s.hydrating {
		applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
			activitySpinner(c, tr(c, "reports.list.loading"))
			ui.Text(c, tr(c, "reports.list.loading")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		return
	}
	reports := s.reportSummaries()
	if len(reports) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        reicon.FileText,
			Title:       tr(c, "reports.list.empty_title"),
			Description: tr(c, "reports.list.empty_description"),
			Surface:     emptyCard,
			Action: func() {
				primaryButton(c, tr(c, "reports.action.new_task")).OnClick(s.newBenchmark)
			},
		})
		return
	}
	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		for _, report := range reports {
			s.unifiedReportCard(c, report)
		}
	})
}

func reportRatioBar(c *ui.Context, ratio float64, width, height float32, color ui.Color) {
	t := tokens(c)
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1 {
		ratio = 1
	}
	ui.Box(c).Width(width).Height(height).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		rad := r.H / 2
		p.Fill(r, t.Color.BorderDefault, rad)
		fillW := r.W * float32(ratio)
		if fillW > 0 {
			fillW = min(r.W, max(r.H, fillW))
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: fillW, H: r.H}, color, rad)
		}
	})
}

func reportCard(c *ui.Context, report core.ReportSummary, modelHint, exclusivity string, open func()) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key(report.ReportId)).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Stack).Children(func() {
			if report.Unreadable {
				scale := t.Color.Danger
				ui.Box(c).Size(36, 36).Radius(t.Radius.Inset).Shrink(0).
					Background(scale.SubtleBg).Border(1, scale.Border).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					components.Icon(c, reicon.CircleAlert).Size(17, 17).TextColor(semanticAccentColor(c, scale))
				})
			} else {
				modelIconBox(c, modelHint, 36, t.Radius.Inset)
			}
			ui.Column(c).Grow(1).MinWidth(180).Gap(2).Children(func() {
				ui.Text(c, reportHeadingFor(c, report)).Bold().FontSize(t.Type.CardTitle).TextColor(t.Color.FgPrimary).SingleLine()
				ui.Text(c, reportSubheadingFor(c, report)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
			})
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if !report.Unreadable {
					exclusivityBadge(c, exclusivity)
				}
				outlineIconButton(c, reicon.Eye, tr(c, "reports.list.open")).
					Label(trf(c, "reports.list.open_aria", "id", report.ReportId)).
					OnClick(open)
			})
		})
		if report.Unreadable {
			applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
				Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
				components.Icon(c, reicon.CircleAlert).Size(14, 14).TextColor(semanticAccentColor(c, t.Color.Danger))
				ui.Text(c, tr(c, "reports.list.unreadable_body")).FontSize(t.Type.Caption).TextColor(t.Color.Danger.Fg)
			})
		} else if len(report.RunSummaries) > 0 {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(0).Padding(t.Density.Tight, t.Density.Stack), materialSunken).Children(func() {
				for i, run := range report.RunSummaries {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					q := run.Quality
					hasRatio := q.Numerator != nil && q.Denominator != nil && *q.Denominator > 0
					dotColor := t.Color.BorderStrong
					var ratio float64
					if hasRatio {
						ratio = float64(*q.Numerator) / float64(*q.Denominator)
						if q.Complete {
							dotColor = t.Color.Success.Solid
							if ratio < 0.5 {
								dotColor = t.Color.Warning.Solid
							}
						} else {
							dotColor = t.Color.Active.Solid
						}
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Tight+1, 0).Children(func() {
						ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Shrink(0).Background(dotColor)
						ui.Text(c, reportRunLineFor(c, report, run)).
							FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgPrimary).
							Grow(1).MinWidth(0).SingleLine().Selectable()
						if hasRatio {
							reportRatioBar(c, ratio, 68, 5, dotColor)
						}
					})
				}
			})
		}
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
