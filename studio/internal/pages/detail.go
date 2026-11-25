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
	p := d.Progress
	if p == nil {
		ui.Text(c, tr(c, "detail.waiting_progress"))
		return
	}
	headline := progressHeadline(p)
	elapsed := ""
	if !terminal(d.Task.State) && d.Task.State != "cancelling" && !d.Task.AwaitingService {
		elapsed = s.formatTaskElapsed(c, emptyText(d.Task.StartedAt))
	}
	if elapsed != "" {
		ui.Row(c.Key("detail-elapsed-" + d.Task.TaskId)).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			ui.Text(c, headline)
			ui.Text(c, "· "+elapsed).TextColor(c.Theme().TextMuted)
		})
	} else {
		ui.Text(c, headline)
	}
	progressBar(c, progressValue(p.Done, p.Total), tr(c, "detail.progress_bar_label"))
	ui.Text(c, trf(c, "detail.current_case", "case_id", text(p.CurrentCaseId)))
	columns := []ui.TableColumn{
		{Title: tr(c, "detail.col.case"), Width: 180},
		{Title: tr(c, "detail.col.status"), Width: 160},
		{Title: tr(c, "detail.col.category"), Width: 100},
		{Title: tr(c, "detail.col.quality"), Width: 100},
		{Title: tr(c, "detail.col.duration"), Width: 100},
		{Title: tr(c, "detail.col.error_category"), Width: 160},
	}
	s.casesTable.Key = func(i int) any { return p.Cases[i].CaseId }
	tableLabel := tr(c, "detail.cases_table_label")
	pageTable(c, &s.casesTable, tableLabel, columns, len(p.Cases), func(row, col int) {
		item := p.Cases[row]
		values := []string{item.CaseId, stateLabel(item.State), optionalText(item.Category), caseQuality(item), formatDurationMs(item.DurationMs), caseErrorCategory(item)}
		ui.Text(c, values[col]).Selectable()
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
