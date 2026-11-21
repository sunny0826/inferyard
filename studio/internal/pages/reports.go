package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			ui.Box(c).Grow(1).Children(func() {
				components.Tabs(c, &s.reportsTab, components.TabsUnderline, "测评报告", "长上下文专项", "运行历史")
			})
			if s.reportsTab == reportsTabList {
				button(c, "从历史运行生成报告").OnClick(func() {
					s.reportDialogOpen = true
					s.reportDiscardConfirm = false
				})
			}
		})
		switch s.reportsTab {
		case reportsTabSpeed:
			s.speedReportsTabView(c)
		case reportsTabHistory:
			s.historyReportsTabView(c)
		default:
			s.reportsTableView(c)
		}
	})
	s.reportSourcesDialog(c)
}

func (s *viewState) speedReportsTabView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.Gauge,
			Title:       "长上下文专项",
			Description: "暂无专项历史任务。在任务页选择「长上下文速度」后开始专项测评。",
			Surface:     emptyCard,
		})
		return
	}
	s.speedHistoryView(c)
}

func (s *viewState) historyReportsTabView(c *ui.Context) {
	errorView(c, s.historyError)
	s.historyView(c)
}

func (s *viewState) reportSourcesDialog(c *ui.Context) {
	dirty := s.reportDialogOpen && s.reportChoices.Len() > 0
	c.Key("report-sources-dialog")
	components.Dialog(c, &s.reportDialogOpen, components.DialogOptions{
		Label:                "从历史运行生成报告",
		DisableBackdropClose: dirty,
		DisableEscapeClose:   dirty,
	}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(130)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(tokens(c).Density.Row).Children(func() {
				heading(c, "从历史运行生成报告", 2)
				errorView(c, s.historyError)
				ui.Text(c, "选择来源运行（1–100 个）")
				runs := s.historyRuns()
				if s.historyError != nil {
					button(c, "重试读取来源").AlignSelf(ui.Start).OnClick(s.refreshAction)
				} else if !s.loaded {
					ui.Text(c, "正在读取报告来源…")
				} else if len(runs) == 0 {
					ui.Text(c, "暂无运行记录，请先完成测评，再选择来源。")
					button(c, "新任务").AlignSelf(ui.Start).OnClick(func() {
						s.reportDialogOpen = false
						s.newBenchmark()
					})
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
					applyMaterial(c, ui.Scroll(c.Key("report-sources")).FillWidth().Height(260), materialInset).
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
					ui.Text(c, "请选择 1–100 个来源运行。").TextColor(c.Theme().TextMuted)
				}
				ui.Text(c, fmt.Sprintf("已选 %d / 100", count))
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
					if primaryActionButton(c, "生成报告", s.actionBusy("create-report")).AlignSelf(ui.Start).Disabled(s.busy || count < 1 || count > 100).Clicked() {
						paths := []string{}
						for _, r := range runs {
							if s.reportChoices.Has(r.Path) {
								paths = append(paths, r.Path)
							}
						}
						s.reportDialogOpen = false
						if s.simulate(previewCreateReport{paths: paths}) {
							return
						}
						const toastID = "create-report-toast"
						s.enqueueProgressToast(toastID, "正在生成报告…")
						s.workAs("create-report", func() (string, *core.ApiError) {
							report, e := s.service.CreateReport(core.CreateReportRequest{RunPaths: paths})
							if e != nil {
								s.win.Update(func() { s.dismissToast(toastID) })
								return "", e
							}
							reportID := report.ReportId
							s.win.Update(func() {
								s.lastManualReportID = reportID
								s.resolveProgressToast(toastID, "报告已生成", "查看报告", func() {
									s.openReport(reportID)
								})
							})
							return "", nil
						})
					}
					if button(c, "关闭").AlignSelf(ui.Start).Clicked() {
						if s.reportChoices.Len() > 0 {
							s.reportDiscardConfirm = true
						} else {
							s.reportDialogOpen = false
						}
					}
				})
			})
		})
	})
	if components.AlertDialog(c, &s.reportDiscardConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: "放弃已勾选的来源运行"},
		Title:         "放弃当前选择？",
		Message:       "已勾选的来源运行尚未生成报告，关闭后将清空选择。",
		ConfirmLabel:  "放弃选择",
		CancelLabel:   "继续选择",
	}) == components.DialogConfirmed {
		s.reportChoices.Clear()
		s.reportDialogOpen = false
	}
}
