package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) detailView(c *ui.Context) {
	s.header(c, tr(c, "detail.title"))
	if button(c, tr(c, "detail.back_dashboard")).Clicked() {
		s.detailID = ""
	}
	errorView(c, s.detailError)
	if s.detailError != nil {
		button(c, tr(c, "detail.retry_load")).OnClick(func() { s.selectTask(s.detailID) })
		if s.detail == nil {
			return
		}
	}
	if s.detail == nil {
		host := ui.Column(c.Key("detail-loading-" + s.detailID)).FillWidth().Gap(tokens(c).Density.Section)
		show := delayedVisible(c, host, "detail-load", true)
		host.Children(func() {
			if show {
				detailLoadingSkeleton(c)
			}
		})
		return
	}
	d := s.detail
	t := d.Task
	card(c, text(t.Bundle)+" · "+t.TaskId, func() {
		statusBadge(c, taskLabel(t), runColor(c, t))
		ui.Text(c, trf(c, "detail.run_dir", "path", displayName(text(t.RunPath)))).Selectable()
		errorView(c, t.Error)
		wholeRound := s.task.stage != ""
		cancelOption := destructiveOption{
			kind: destructiveCancelRun, id: t.TaskId, label: tr(c, "detail.cancel.label"), confirmLabel: tr(c, "detail.cancel.confirm"),
			prompt:   cancelRunPrompt(wholeRound),
			disabled: (s.busy && s.task.stage == "") || terminal(t.State) || t.State == "cancelling",
		}
		var cancelConfirmed bool
		if wholeRound {
			cancelConfirmed = s.destructiveDialog(c, cancelOption)
		} else {
			cancelConfirmed = s.destructiveAction(c, cancelOption)
		}
		if cancelConfirmed {
			if s.task.stage != "" {
				s.newBenchmark()
				return
			}
			id := t.TaskId
			if s.simulate(previewCancelRun{id: id}) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.cancelRunTask(id)
				return l10n.TR("dashboard.toast.cancel_requested"), e
			})
		}
	})
	card(c, tr(c, "detail.live_progress_card"), func() { s.detailProgress(c, d) })
	s.resourceView(c, d.Resources)
}

func (s *viewState) detailProgress(c *ui.Context, d *core.RunTaskDetail) {
	elapsed := ""
	if !terminal(d.Task.State) && d.Task.State != "cancelling" && !d.Task.AwaitingService {
		elapsed = s.formatTaskElapsed(c, emptyText(d.Task.StartedAt))
	}
	p := d.Progress
	if p == nil {
		waiting := tr(c, "detail.waiting_progress")
		if elapsed != "" {
			ui.Row(c.Key("detail-waiting-elapsed-" + d.Task.TaskId)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
				ui.Text(c, waiting)
				ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(c.Theme().TextMuted)
			})
		} else {
			ui.Text(c, waiting)
		}
		return
	}
	headline := progressHeadline(p)
	if elapsed != "" {
		ui.Row(c.Key("detail-elapsed-" + d.Task.TaskId)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			ui.Text(c, headline).FontFeatures("tnum")
			ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(c.Theme().TextMuted)
		})
	} else {
		ui.Text(c, headline).FontFeatures("tnum")
	}
	progressBar(c, progressValue(p.Done, p.Total), tr(c, "detail.progress_bar_label"))
	ui.Text(c, trf(c, "detail.current_case", "case_id", text(p.CurrentCaseId)))
	const detailCasePageSize = 50
	totalCases := len(p.Cases)
	pages := max(1, (totalCases+detailCasePageSize-1)/detailCasePageSize)
	s.casesTablePage = min(max(0, s.casesTablePage), pages-1)
	if pages > 1 {
		ui.Row(c).AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			if button(c, tr(c, "detail.table.prev_page")).Disabled(s.casesTablePage == 0).Clicked() {
				s.casesTablePage--
			}
			ui.Text(c, trf(c, "detail.table.page_status", "page", s.casesTablePage+1, "totalPages", pages, "count", totalCases)).FontFeatures("tnum")
			if button(c, tr(c, "detail.table.next_page")).Disabled(s.casesTablePage == pages-1).Clicked() {
				s.casesTablePage++
			}
		})
	}
	start := s.casesTablePage * detailCasePageSize
	visibleCases := p.Cases[start:min(start+detailCasePageSize, totalCases)]
	columns := []ui.TableColumn{
		{Title: tr(c, "detail.col.case"), Width: 180},
		{Title: tr(c, "detail.col.status"), Width: 160},
		{Title: tr(c, "detail.col.category"), Width: 100},
		{Title: tr(c, "detail.col.quality"), Width: 100},
		{Title: tr(c, "detail.col.duration"), Width: 100},
		{Title: tr(c, "detail.col.error_category"), Width: 160},
	}
	s.casesTable.Key = func(i int) any { return visibleCases[i].CaseId }
	tableLabel := tr(c, "detail.cases_table_label")
	pageTable(c, &s.casesTable, tableLabel, columns, len(visibleCases), func(row, col int) {
		item := visibleCases[row]
		values := []string{item.CaseId, stateLabel(item.State), optionalText(item.Category), caseQuality(item), formatDurationMs(item.DurationMs), caseErrorCategory(item)}
		cell := ui.Text(c, values[col]).Selectable()
		if col == 4 {
			cell.FontFeatures("tnum")
		}
	}).Height(240).Label(tableLabel)
}

func progressHeadline(p *core.RunProgress) string {
	label := l10n.TRf("detail.headline_cases", "done", p.Done, "total", progressTotal(p.Total))
	if phase := emptyText(p.Phase); phase != "" {
		return stateLabel(phase) + " · " + label
	}
	return label
}

func caseQuality(item core.CaseProgress) string {
	if item.QualityState == nil || *item.QualityState == "" {
		return l10n.TR("detail.missing_value")
	}
	return reportQuality(item.QualityState)
}

func caseErrorCategory(item core.CaseProgress) string {
	if item.ErrorCategory != nil && *item.ErrorCategory != "" {
		return *item.ErrorCategory
	}
	if item.State == "failed" {
		return l10n.TR("detail.missing_value")
	}
	return "—"
}

func optionalText(value *string) string {
	if value == nil || *value == "" {
		return l10n.TR("detail.missing_value")
	}
	return *value
}
