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
	s.subpageHeader(c, tr(c, "nodes.compare.title"))
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
	s.compareQualitySummary(c, comparison)
	for _, run := range comparison.Runs {
		s.compareRunCard(c, run)
	}
	s.compareCasesCard(c, comparison.Cases)
}

func (s *viewState) compareQualitySummary(c *ui.Context, comparison *core.ReportComparison) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "nodes.compare.quality")).Bold().Grow(1)
			ui.Text(c, s.comparisonReportName(comparison.LeftRef)).FontSize(t.Type.Caption).SingleLine()
			ui.Text(c, s.comparisonReportName(comparison.RightRef)).FontSize(t.Type.Caption).SingleLine()
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Box(c).Grow(1)
			ui.Text(c, formatCompareQuality(comparison.LeftQuality)).FontSize(t.Type.Metric).FontFeatures("tnum")
			ui.Text(c, formatCompareQuality(comparison.RightQuality)).FontSize(t.Type.Metric).FontFeatures("tnum")
		})
	})
}

func (s *viewState) compareRunCard(c *ui.Context, run core.ReportCompareRun) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Text(c, run.Model).Bold().FontSize(t.Type.CardTitle)
		compareMetricRow(c, tr(c, "nodes.compare.quality"), formatCompareQuality(ptrMetrics(run.Left)), formatCompareQuality(ptrMetrics(run.Right)))
		compareMetricRow(c, tr(c, "nodes.compare.ttft_p50"), formatCompareNumber(metricValue(run.Left, "ttft_p50"), "ms"), formatCompareNumber(metricValue(run.Right, "ttft_p50"), "ms"))
		compareMetricRow(c, tr(c, "nodes.compare.ttft_p95"), formatCompareNumber(metricValue(run.Left, "ttft_p95"), "ms"), formatCompareNumber(metricValue(run.Right, "ttft_p95"), "ms"))
		compareMetricRow(c, tr(c, "nodes.compare.decode"), formatCompareNumber(metricValue(run.Left, "decode"), "tok/s"), formatCompareNumber(metricValue(run.Right, "decode"), "tok/s"))
		rightRatio := "—"
		if run.SpeedupRatio != nil {
			rightRatio = trf(c, "nodes.compare.ratio", "ratio", fmt.Sprintf("%.2f", *run.SpeedupRatio))
		}
		compareMetricRow(c, tr(c, "nodes.compare.speedup"), "—", rightRatio)
		compareMetricRow(c, tr(c, "nodes.compare.memory"), formatCompareBytes(metricValue(run.Left, "memory")), formatCompareBytes(metricValue(run.Right, "memory")))
		compareMetricRow(c, tr(c, "nodes.compare.working_set"), formatCompareBytes(metricValue(run.Left, "working_set")), formatCompareBytes(metricValue(run.Right, "working_set")))
		compareMetricRow(c, tr(c, "nodes.compare.temperature"), formatCompareNumber(metricValue(run.Left, "temperature"), "°C"), formatCompareNumber(metricValue(run.Right, "temperature"), "°C"))
	})
}

func compareMetricRow(c *ui.Context, label, left, right string) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1)
		ui.Text(c, left).FontFeatures("tnum").Selectable()
		ui.Text(c, right).FontFeatures("tnum").Selectable()
	})
}

func (s *viewState) compareCasesCard(c *ui.Context, cases []core.ReportCompareCase) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, reicon.FileText).Size(15, 15)
			ui.Text(c, tr(c, "nodes.compare.cases")).Bold()
		})
		for _, item := range cases {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, item.CaseID).FontSize(t.Type.Caption).Grow(1).MinWidth(0).SingleLine()
				ui.Text(c, compareRequestSummary(item.Left)).FontSize(t.Type.Caption).FontFeatures("tnum")
				ui.Text(c, compareRequestSummary(item.Right)).FontSize(t.Type.Caption).FontFeatures("tnum")
			})
		}
	})
}
