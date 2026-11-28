package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) loadReportComparison() {
	left, right := s.nodes.compareLeftRef, s.nodes.compareRightRef
	if left == "" || right == "" || left == right {
		s.nodes.compareErr = &core.ApiError{Code: "invalid_request", Message: l10n.TR("nodes.compare.select_two")}
		return
	}
	compare := s.nodes.compareReportsFn
	if compare == nil && s.service != nil {
		compare = s.service.CompareReports
	}
	if compare == nil {
		return
	}
	s.nodes.comparePickerOpen = false
	s.nodes.compareOpen = true
	s.nodes.compareLoading = true
	s.nodes.compareErr = nil
	if s.preview != nil {
		comparison, err := compare(left, right)
		s.nodes.comparison, s.nodes.compareErr, s.nodes.compareLoading = comparison, err, false
		return
	}
	s.workAs("compare-reports", func() (string, *core.ApiError) {
		comparison, err := compare(left, right)
		s.updateView(func() {
			s.nodes.comparison, s.nodes.compareErr, s.nodes.compareLoading = comparison, err, false
		})
		return "", err
	})
}

func (s *viewState) reportCompareView(c *ui.Context) {
	s.subpageHeader(c, tr(c, "nodes.compare.title"), func() {
		outlineIconButton(c, reicon.Layers, tr(c, "nodes.compare.action")).
			Disabled(s.nodes.compareLoading || len(s.reportSummaries()) < 2).
			OnClick(s.openReportComparePicker)
	})
	defer s.reportComparePicker(c)
	if s.nodes.compareLoading {
		pageLoadingSkeleton(c)
		return
	}
	if s.nodes.compareErr != nil {
		errorView(c, s.nodes.compareErr)
		return
	}
	comparison := s.nodes.comparison
	if comparison == nil {
		return
	}
	leftHeader := s.comparisonReportNameFor(c, comparison.LeftRef)
	rightHeader := s.comparisonReportNameFor(c, comparison.RightRef)
	s.compareQualitySummary(c, comparison, leftHeader, rightHeader)
	for _, run := range comparison.Runs {
		s.compareRunCard(c, run, leftHeader, rightHeader)
	}
	s.compareCasesCard(c, comparison.Cases, leftHeader, rightHeader)
}

func compareColumnWidth(c *ui.Context) float32 {
	if compactLayout(c) {
		return c.Theme().Space(36)
	}
	return c.Theme().Space(48)
}

func compareThreeColRow(c *ui.Context, leading, left, right func()) {
	t := tokens(c)
	colW := compareColumnWidth(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).Padding(t.Density.Tight+1, 0).Children(func() {
		ui.Box(c).Grow(1).MinWidth(0).Children(leading)
		ui.Box(c).Width(colW).Shrink(0).AlignItems(ui.End).Children(left)
		ui.Box(c).Width(colW).Shrink(0).AlignItems(ui.End).Children(right)
	})
}

func compareHeaderStrip(c *ui.Context, titleEl func(), leftHeader, rightHeader string) {
	t := tokens(c)
	colW := compareColumnWidth(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).Children(func() {
		ui.Box(c).Grow(1).MinWidth(0).Children(titleEl)
		ui.Box(c).Width(colW).Shrink(0).AlignItems(ui.End).Children(func() {
			ui.Text(c, leftHeader).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).MaxWidth(colW).SingleLine().TruncateMiddle()
		})
		ui.Box(c).Width(colW).Shrink(0).AlignItems(ui.End).Children(func() {
			ui.Text(c, rightHeader).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).MaxWidth(colW).SingleLine().TruncateMiddle()
		})
	})
}

func (s *viewState) compareQualitySummary(c *ui.Context, comparison *core.ReportComparison, leftHeader, rightHeader string) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		compareHeaderStrip(c, func() {
			heading(c, tr(c, "nodes.compare.quality"), 2)
		}, leftHeader, rightHeader)
		reportKVDivider(c)
		compareThreeColRow(c,
			func() {
				ui.Text(c, tr(c, "nodes.compare.quality")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
			},
			func() {
				val := formatCompareQuality(comparison.LeftQuality)
				col := t.Color.FgPrimary
				if val == compareMissing {
					col = t.Color.FgTertiary
				}
				ui.Text(c, val).Bold().FontSize(t.Type.Metric).FontFeatures("tnum").TextColor(col).SingleLine()
			},
			func() {
				val := formatCompareQuality(comparison.RightQuality)
				col := t.Color.FgPrimary
				if val == compareMissing {
					col = t.Color.FgTertiary
				}
				ui.Text(c, val).Bold().FontSize(t.Type.Metric).FontFeatures("tnum").TextColor(col).SingleLine()
			},
		)
	})
}

func (s *viewState) compareRunCard(c *ui.Context, run core.ReportCompareRun, leftHeader, rightHeader string) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.Tight).Padding(t.Card.Padding), materialCard).Children(func() {
		compareHeaderStrip(c, func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).MinWidth(0).Children(func() {
				modelIconBox(c, s.modelIconHint(run.Model), 28, t.Radius.Inset)
				ui.Text(c, run.Model).Bold().FontSize(t.Type.CardTitle).MinWidth(0).SingleLine().TruncateMiddle()
			})
		}, leftHeader, rightHeader)
		rightRatio := compareMissing
		if run.SpeedupRatio != nil {
			rightRatio = trf(c, "nodes.compare.ratio", "ratio", fmt.Sprintf("%.2f", *run.SpeedupRatio))
		}
		rows := [][3]string{
			{tr(c, "nodes.compare.models"), compareSideModel(run.Left != nil, run.LeftModel, run.Model), compareSideModel(run.Right != nil, run.RightModel, run.Model)},
			{tr(c, "nodes.compare.quality"), formatCompareQuality(ptrMetrics(run.Left)), formatCompareQuality(ptrMetrics(run.Right))},
			{tr(c, "nodes.compare.latency_p50"), formatCompareNumber(metricValue(run.Left, "latency_p50"), "ms"), formatCompareNumber(metricValue(run.Right, "latency_p50"), "ms")},
			{tr(c, "nodes.compare.latency_p95"), formatCompareNumber(metricValue(run.Left, "latency_p95"), "ms"), formatCompareNumber(metricValue(run.Right, "latency_p95"), "ms")},
			{tr(c, "nodes.compare.ttft_p50"), formatCompareNumber(metricValue(run.Left, "ttft_p50"), "ms"), formatCompareNumber(metricValue(run.Right, "ttft_p50"), "ms")},
			{tr(c, "nodes.compare.ttft_p95"), formatCompareNumber(metricValue(run.Left, "ttft_p95"), "ms"), formatCompareNumber(metricValue(run.Right, "ttft_p95"), "ms")},
			{tr(c, "nodes.compare.output"), formatCompareNumber(metricValue(run.Left, "output"), "tok/s"), formatCompareNumber(metricValue(run.Right, "output"), "tok/s")},
			{tr(c, "nodes.compare.speedup"), compareMissing, rightRatio},
			{tr(c, "nodes.compare.memory"), formatCompareBytes(metricValue(run.Left, "memory")), formatCompareBytes(metricValue(run.Right, "memory"))},
			{tr(c, "nodes.compare.working_set"), formatCompareBytes(metricValue(run.Left, "working_set")), formatCompareBytes(metricValue(run.Right, "working_set"))},
			{tr(c, "nodes.compare.temperature"), formatCompareNumber(metricValue(run.Left, "temperature"), "°C"), formatCompareNumber(metricValue(run.Right, "temperature"), "°C")},
		}
		for _, row := range rows {
			reportKVDivider(c)
			compareMetricRow(c, row[0], row[1], row[2])
		}
	})
}

func compareMetricRow(c *ui.Context, label, left, right string) {
	t := tokens(c)
	colW := compareColumnWidth(c)
	renderCell := func(val string) {
		col := t.Color.FgPrimary
		if val == compareMissing {
			col = t.Color.FgTertiary
		}
		ui.Text(c, val).FontSize(t.Type.Body).FontFeatures("tnum").TextColor(col).
			MaxWidth(colW).SingleLine().TruncateMiddle().Selectable()
	}
	compareThreeColRow(c,
		func() { ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine() },
		func() { renderCell(left) },
		func() { renderCell(right) },
	)
}

func (s *viewState) compareCasesCard(c *ui.Context, cases []core.ReportCompareCase, leftHeader, rightHeader string) {
	if len(cases) == 0 {
		return
	}
	t := tokens(c)
	colW := compareColumnWidth(c)
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.Tight).Padding(t.Card.Padding), materialCard).Children(func() {
		compareHeaderStrip(c, func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				components.Icon(c, reicon.FileText).Size(15, 15).Shrink(0).TextColor(t.Color.FgSecondary)
				heading(c, tr(c, "nodes.compare.cases"), 2)
			})
		}, leftHeader, rightHeader)
		for _, item := range cases {
			reportKVDivider(c)
			leftVal := compareRequestSummaryFor(c, item.Left)
			rightVal := compareRequestSummaryFor(c, item.Right)
			compareThreeColRow(c,
				func() { ui.Text(c, item.CaseID).FontSize(t.Type.Caption).FontFeatures("tnum").SingleLine() },
				func() {
					col := t.Color.FgPrimary
					if leftVal == compareMissing {
						col = t.Color.FgTertiary
					}
					ui.Text(c, leftVal).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(col).MaxWidth(colW).SingleLine()
				},
				func() {
					col := t.Color.FgPrimary
					if rightVal == compareMissing {
						col = t.Color.FgTertiary
					}
					ui.Text(c, rightVal).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(col).MaxWidth(colW).SingleLine()
				},
			)
		}
	})
}
