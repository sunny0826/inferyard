package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

const (
	reportsTabList = iota
	reportsTabSpeed
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
	s.header(c, tr(c, "reports.title"))
	if s.reportsTab < reportsTabList || s.reportsTab > reportsTabSpeed {
		s.reportsTab = reportsTabList
	}
	t := tokens(c)
	ui.Column(c.Key("reports-tabs")).FillWidth().Gap(t.Density.Stack).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Box(c).Grow(1).MinWidth(0).Children(func() {
				components.Tabs(c, &s.reportsTab, components.TabsUnderline, tr(c, "reports.tab.reports"), tr(c, "reports.tab.context_speed"))
			})
			if s.reportsTab == reportsTabList {
				outlineIconButton(c, lucide.Plus, tr(c, "reports.action.create_from_history")).OnClick(func() {
					s.reportDialogOpen = true
					s.reportDiscardConfirm = false
				})
			}
		})
		if s.reportsTab == reportsTabSpeed {
			s.speedReportsTabView(c)
		} else {
			s.reportsTableView(c)
		}
	})
	s.reportSourcesDialog(c)
}

func (s *viewState) speedReportsTabView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.Gauge,
			Title:       tr(c, "reports.tab.context_speed"),
			Description: tr(c, "reports.speed.empty_description"),
			Surface:     emptyCard,
		})
		return
	}
	s.speedHistoryView(c)
}

func (s *viewState) reportSourcesDialog(c *ui.Context) {
	dirty := s.reportDialogOpen && s.reportChoices.Len() > 0
	c.Key("report-sources-drawer")
	drawerTitle := tr(c, "reports.action.create_from_history")
	drawer(c, &s.reportDialogOpen, drawerOptions{
		Label:          drawerTitle,
		Title:          drawerTitle,
		Width:          c.Theme().Space(140),
		GuardClose:     dirty,
		SuspendDismiss: s.reportDiscardConfirm,
		OnCloseAttempt: func() { s.reportDiscardConfirm = true },
	}, func(panel ui.Element) {
		t := tokens(c)
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(t.Density.Stack).Children(func() {
			errorView(c, s.historyError)
			ui.Text(c, tr(c, "reports.dialog.select_runs")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			runs := s.historyRuns()
			if s.historyError != nil {
				button(c, tr(c, "reports.dialog.retry_load")).AlignSelf(ui.Start).OnClick(s.refreshAction)
			} else if !s.loaded || s.hydrating {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					activitySpinner(c, tr(c, "reports.dialog.loading"))
					ui.Text(c, tr(c, "reports.dialog.loading")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				})
			} else if len(runs) == 0 {
				emptyGuideCard(c, emptyGuideOptions{
					Icon:        lucide.FileText,
					Title:       tr(c, "reports.dialog.empty"),
					Surface:     emptySunken,
					Action: func() {
						primaryButton(c, tr(c, "reports.action.new_task")).OnClick(func() {
							s.reportDialogOpen = false
							s.newBenchmark()
						})
					},
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
				itemRadius := concentricRadius(t.Radius.Inset, t.Density.Tight)
				applyMaterial(c, ui.Scroll(c.Key("report-sources")).FillWidth().Grow(1).MinHeight(220), materialSunken).
					Label(tr(c, "reports.dialog.sources_list_aria")).Children(func() {
					ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Row).Children(func() {
						for _, r := range runs {
							checked := s.reportChoices.Has(r.Path)
							row := ui.Column(c.Key(r.Path)).FillWidth().Gap(2).
								Padding(t.Density.Row, t.Density.Stack).Radius(itemRadius)
							if checked {
								row.Background(t.Color.AccentSelection)
							} else if row.Hovered() {
								row.Background(t.Color.SurfaceHover)
							}
							row.Children(func() {
								if components.Checkbox(c, &checked, r.RunId).Disabled(s.busy).Changed() {
									if checked {
										s.reportChoices.Add(r.Path)
									} else {
										s.reportChoices.Remove(r.Path)
									}
								}
								meta := historyModel(r) + " · " + historyBundleDisplay(r) + " · " + runScore(r)
								ui.Text(c, meta).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
							})
						}
					})
				})
			}
			count := s.reportChoices.Len()
			ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, trf(c, "reports.dialog.selected_count", "count", count)).
					FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).Grow(1)
				if outlineButton(c, tr(c, "reports.dialog.close")).Clicked() {
					if s.reportChoices.Len() > 0 {
						s.reportDiscardConfirm = true
					} else {
						s.reportDialogOpen = false
					}
				}
				if primaryActionButton(c, tr(c, "reports.dialog.create"), s.actionBusy("create-report")).Disabled(s.busy || count < 1 || count > 100).Clicked() {
					paths := []string{}
					for _, r := range runs {
						if s.reportChoices.Has(r.Path) {
							paths = append(paths, r.Path)
						}
					}
					s.reportDialogOpen = false
					s.createReportFromPaths(c, paths)
				}
			})
		})
	})
	if studioAlertDialog(c, &s.reportDiscardConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "reports.dialog.discard_aria")},
		Title:         tr(c, "reports.dialog.discard_title"),
		Message:       tr(c, "reports.dialog.discard_message"),
		ConfirmLabel:  tr(c, "reports.dialog.discard_confirm"),
		CancelLabel:   tr(c, "reports.dialog.discard_cancel"),
	}) == components.DialogConfirmed {
		s.reportChoices.Clear()
		s.reportDialogOpen = false
	}
}
