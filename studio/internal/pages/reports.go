package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) reportsView(c *ui.Context) {
	if s.speed != nil && s.speed.opened != nil {
		s.speedResultView(c)
		return
	}
	if s.openedReport != nil {
		s.reportView(c)
		return
	}
	s.header(c, "报告")
	s.speedHistoryView(c)
	card(c, "历史任务", func() { errorView(c, s.historyError); s.historyView(c) })
	card(c, "报告列表", func() {
		s.reportsTableView(c)
		if s.progress != nil {
			if s.progress.Report.Status == "verified" {
				statusBadge(c, "已核验", c.Theme().Success)
			} else if s.progress.NextAction == "verify" {
				if button(c, "核验").Disabled(s.busy).Clicked() {
					s.execute(s.nextCommand(), true)
				}
			}
		}
	})
	s.reportSourcesView(c)
}

func (s *viewState) reportSourcesView(c *ui.Context) {
	componentFold(c, "从历史运行另做报告", func() {
		errorView(c, s.historyError)
		ui.Text(c, "选择来源 run（1–100 个）")
		runs := s.historyRuns()
		if s.historyError != nil {
			button(c, "重试读取来源").AlignSelf(ui.Start).OnClick(s.refreshAction)
		} else if !s.loaded {
			ui.Text(c, "正在读取报告来源…")
		} else if len(runs) == 0 {
			ui.Text(c, "暂无运行记录，请先完成测评，再选择来源。")
			button(c, "新任务").AlignSelf(ui.Start).OnClick(s.newBenchmark)
		}
		known := map[any]bool{}
		for _, r := range runs {
			known[r.Path] = true
		}
		for path := range s.reportChoices.All() {
			if !known[path] {
				s.reportChoices.Remove(path)
			}
		}
		s.reportSources.Label = func(i int) string { return runs[i].RunId + " · " + displayName(runs[i].Path) }
		c.Key("report-sources")
		s.reportSources.Key = func(i int) any { return runs[i].Path }
		s.reportSources.Selection = &s.reportChoices
		components.List(c, &s.reportSources, len(runs), func(i int) {
			ui.Column(c).Padding(6).Gap(2).Children(func() {
				ui.Text(c, runs[i].RunId)
				ui.Text(c, displayName(runs[i].Path)).FontSize(max(12, c.Theme().Rem(.85)))
			})
		}).Label("报告来源运行列表").Height(220).Disabled(s.busy)
		count := s.reportChoices.Len()
		if count < 1 || count > 100 {
			ui.Text(c, "请选择 1–100 个来源 run。")
		}
		ui.Text(c, fmt.Sprintf("已选 %d / 100", count))
		if button(c, "生成报告").AlignSelf(ui.Start).Disabled(s.busy || count < 1 || count > 100).Clicked() {
			paths := []string{}
			for _, r := range runs {
				if s.reportChoices.Has(r.Path) {
					paths = append(paths, r.Path)
				}
			}
			if s.simulate(previewCreateReport{paths: paths}) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				report, e := s.service.CreateReport(core.CreateReportRequest{RunPaths: paths})
				return "已生成 " + report.ReportId, e
			})
		}
	})
}
