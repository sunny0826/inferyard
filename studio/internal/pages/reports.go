package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

// The Reports page groups three heterogeneous lists behind one tab row. The
// evaluator report list comes first because reading a report is the common
// action; the specialty history and the run history are separate tasks.
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
	tab := reportsTabState(c)
	box := ui.Column(c.Key("reports-tabs")).Gap(tokens(c).Density.Row)
	box.Children(func() {
		components.Tabs(c, tab, components.TabsSegmented, "测评报告", "长上下文专项", "运行历史")
		switch *tab {
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

// reportsTabState keeps the chosen tab on the window root, so returning from a
// report detail or a specialty result comes back to the tab the user left.
func reportsTabState(c *ui.Context) *int {
	return ui.Local(c.Root(), "reports-tab", func() int { return reportsTabList })
}

// speedReportsTabView keeps the specialty history in its own tab and states
// where a specialty measurement starts when none has finished yet.
func (s *viewState) speedReportsTabView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		card(c, "长上下文专项", func() {
			ui.Text(c, "暂无专项历史任务。在任务页选择「长上下文速度」后开始专项测评。")
		})
		return
	}
	s.speedHistoryView(c)
}

// historyReportsTabView shows the run history alone; run details still open
// from the selected row.
func (s *viewState) historyReportsTabView(c *ui.Context) {
	errorView(c, s.historyError)
	s.historyView(c)
}

func (s *viewState) reportSourcesView(c *ui.Context) {
	componentFold(c, "从历史运行生成报告", func() {
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
			ui.Column(c).Padding(tokens(c).Density.Inset).Gap(tokens(c).Density.Hairline).Children(func() {
				ui.Text(c, runs[i].RunId)
				ui.Text(c, displayName(runs[i].Path)).FontSize(tokens(c).Type.Caption)
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
