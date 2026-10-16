package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"strconv"
)

type reportViewState struct {
	id      string
	index   *core.ReportIndex
	failure *core.ApiError
}

func (s *viewState) readReport(id string) (*core.ReportIndex, *core.ApiError) {
	if s.service == nil {
		return nil, core.Err("not_found")
	}
	return s.service.ReportIndex(id)
}
func (s *viewState) openReport(id string) {
	index, e := s.readReport(id)
	s.openedReport = &reportViewState{id: id, index: index, failure: e}
}
func reportNumber(value *float64) string {
	if value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
func reportRate(rate core.ReportRate) string {
	return reportNumber(rate.Numerator) + " / " + reportNumber(rate.Denominator)
}
func (s *viewState) reportView(c *ui.Context) {
	report := s.openedReport
	s.header(c, "报告 "+report.id)
	button(c, "返回报告列表").AlignSelf(ui.Start).OnClick(func() { s.openedReport = nil })
	if report.failure != nil {
		ui.Text(c, "无法读取")
		button(c, "重新读取报告").AlignSelf(ui.Start).OnClick(func() { s.openReport(report.id) })
		return
	}
	for _, run := range report.index.Runs {
		card(c, run.RunID, func() {
			if run.Dashboard == nil {
				ui.Text(c, "无法读取 dashboard")
				return
			}
			d := run.Dashboard
			heading(c, "质量 "+reportRate(d.Quality), 2)
			ui.Text(c, "各类别不构成综合评分")
			ui.Text(c, "质量排除数 "+reportNumber(d.Quality.Excluded))
			for _, category := range d.Categories {
				label := category.Label
				if label == "" {
					label = category.ID
				}
				ui.Text(c, label+" · "+reportRate(category.Rate)+" · 排除 "+reportNumber(category.Rate.Excluded))
			}
			ui.Text(c, fmt.Sprintf("延迟 (ms) · 最小 %s · P50 %s · P95 %s · 最大 %s", reportNumber(d.Latency.Min), reportNumber(d.Latency.P50), reportNumber(d.Latency.P95), reportNumber(d.Latency.Max)))
			ui.Text(c, "延迟样本 "+reportNumber(d.Latency.SampleCount)+" · 排除 "+reportNumber(d.Latency.Excluded))
			for _, request := range run.Requests {
				ui.Text(c, request.CaseID+" · "+request.CategoryLabel+" · "+request.State+" · "+text(request.Quality)+" · 延迟 (ms) "+reportNumber(request.LatencyMS)).Selectable()
			}
		})
	}
}
