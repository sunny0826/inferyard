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
	ui.Column(c.Key("reports-tabs")).Gap(tokens(c).Density.Row).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			ui.Box(c).Grow(1).Children(func() {
				components.Tabs(c, &s.reportsTab, components.TabsUnderline, tr(c, "reports.tab.reports"), tr(c, "reports.tab.context_speed"))
			})
			if s.reportsTab == reportsTabList {
				button(c, tr(c, "reports.action.create_from_history")).OnClick(func() {
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
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(tokens(c).Density.Row).Children(func() {
			errorView(c, s.historyError)
			ui.Text(c, tr(c, "reports.dialog.select_runs"))
			runs := s.historyRuns()
			if s.historyError != nil {
				button(c, tr(c, "reports.dialog.retry_load")).AlignSelf(ui.Start).OnClick(s.refreshAction)
			} else if !s.loaded || s.hydrating {
				ui.Text(c, tr(c, "reports.dialog.loading"))
			} else if len(runs) == 0 {
				ui.Text(c, tr(c, "reports.dialog.empty"))
				button(c, tr(c, "reports.action.new_task")).AlignSelf(ui.Start).OnClick(func() {
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
				applyMaterial(c, ui.Scroll(c.Key("report-sources")).FillWidth().Grow(1).MinHeight(220), materialInset).
					Label(tr(c, "reports.dialog.sources_list_aria")).Children(func() {
					ui.Column(c).FillWidth().Gap(tokens(c).Density.Row).Padding(tokens(c).Density.Inset).Children(func() {
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
								meta := historyModel(r) + " · " + historyBundleDisplay(r) + " · " + runScore(r)
								ui.Text(c, meta).FontSize(tokens(c).Type.Caption).FontFeatures("tnum").TextColor(c.Theme().TextMuted)
							})
						}
					})
				})
			}
			count := s.reportChoices.Len()
			ui.Text(c, trf(c, "reports.dialog.selected_count", "count", count)).FontFeatures("tnum")
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				if primaryActionButton(c, tr(c, "reports.dialog.create"), s.actionBusy("create-report")).AlignSelf(ui.Start).Disabled(s.busy || count < 1 || count > 100).Clicked() {
					paths := []string{}
					for _, r := range runs {
						if s.reportChoices.Has(r.Path) {
							paths = append(paths, r.Path)
						}
					}
					s.reportDialogOpen = false
					s.createReportFromPaths(c, paths)
				}
				if button(c, tr(c, "reports.dialog.close")).AlignSelf(ui.Start).Clicked() {
					if s.reportChoices.Len() > 0 {
						s.reportDiscardConfirm = true
					} else {
						s.reportDialogOpen = false
					}
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
