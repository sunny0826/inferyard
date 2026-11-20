package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

const (
	reportsTabList = iota
	reportsTabSpeed
	reportsTabHistory
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
	if s.reportsTab < reportsTabList || s.reportsTab > reportsTabHistory {
		s.reportsTab = reportsTabList
	}
	ui.Column(c.Key("reports-tabs")).Gap(tokens(c).Density.Row).Children(func() {
		components.Tabs(c, &s.reportsTab, components.TabsUnderline, "测评报告", "长上下文专项", "运行历史")
		switch s.reportsTab {
		case reportsTabSpeed:
			s.speedReportsTabView(c)
		case reportsTabHistory:
			s.historyReportsTabView(c)
		default:
			s.reportsTableView(c)
			s.reportSourcesView(c)
		}
	})
}

func (s *viewState) speedReportsTabView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		card(c, "长上下文专项", func() {
			ui.Text(c, "暂无专项历史任务。在任务页选择「长上下文速度」后开始专项测评。")
		})
		return
	}
	s.speedHistoryView(c)
}

func (s *viewState) historyReportsTabView(c *ui.Context) {
	errorView(c, s.historyError)
	s.historyView(c)
}

func (s *viewState) reportSourcesView(c *ui.Context) {
	componentFold(c, "从历史运行生成报告", func() {
		errorView(c, s.historyError)
		ui.Text(c, "选择来源运行（1–100 个）")
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
		if len(runs) > 0 {
			ui.Scroll(c.Key("report-sources")).FillWidth().Height(220).
				Border(1, c.Theme().Border).Radius(tokens(c).Radius.Inset).
				Label("报告来源运行列表").Children(func() {
				ui.Column(c).FillWidth().Gap(tokens(c).Density.Tight).Padding(tokens(c).Density.Inset).Children(func() {
					for _, r := range runs {
						checked := s.reportChoices.Has(r.Path)
						ui.Column(c.Key(r.Path)).FillWidth().Gap(tokens(c).Density.Hairline).Children(func() {
							if components.Checkbox(c, &checked, r.RunId).Disabled(s.busy).Changed() {
								if checked {
									s.reportChoices.Add(r.Path)
								} else {
									s.reportChoices.Remove(r.Path)
								}
							}
							ui.Text(c, displayName(r.Path)).FontSize(tokens(c).Type.Caption).TextColor(c.Theme().TextMuted)
						})
					}
				})
			})
		}
		count := s.reportChoices.Len()
		if count < 1 || count > 100 {
			ui.Text(c, "请选择 1–100 个来源运行。")
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
