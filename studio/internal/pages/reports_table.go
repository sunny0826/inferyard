package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
)

func (s *viewState) reportsTableView(c *ui.Context) {
	errorView(c, s.reportsError)
	if len(s.reports) == 0 {
		ui.Text(c, "暂无报告。")
	}
	selectableTable(&s.reportsTable, func(i int) any { return s.reports[i].Path }, func(i int) string { return s.reports[i].ReportId })
	c.Key("reports-table")
	ui.Table(c, &s.reportsTable, []ui.TableColumn{{Title: "ReportId", Width: 280}, {Title: "修改时间", Width: 240}, {Title: "来源数", Width: 100}}, len(s.reports), func(row, col int) {
		report := s.reports[row]
		ui.Text(c, []string{report.ReportId, text(report.CreatedAt), fmt.Sprint(len(report.RunIds))}[col])
	}).Height(220)
	index := selectedRow(&s.reportsTable, len(s.reports))
	if index >= 0 {
		report := s.reports[index]
		ui.Text(c, report.Path).Selectable()
		for i, id := range report.RunIds {
			path := "来源不可达"
			if i < len(report.RunPaths) && report.RunPaths[i] != nil {
				path = *report.RunPaths[i]
			}
			ui.Text(c, id+" · "+path).Selectable()
		}
	}
	ui.Button(c, "打开报告").AlignSelf(ui.Start).Disabled(index < 0).OnClick(func() {
		_, s.failure = s.reportWindows.Open(s.reports[index].ReportId)
	})
}
