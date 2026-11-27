package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) detailView(c *ui.Context) {
	tok := tokens(c)
	s.subpageHeader(c, tr(c, "detail.title"), tr(c, "detail.back_dashboard"), func() { s.detailID = "" })
	errorView(c, s.detailError)
	if s.detailError != nil {
		refreshIconButton(c, tr(c, "detail.retry_load"), false).OnClick(func() { s.selectTask(s.detailID) })
		if s.detail == nil {
			return
		}
	}
	if s.detail == nil {
		host := ui.Column(c.Key("detail-loading-" + s.detailID)).FillWidth().Gap(tok.Density.Section)
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
	var cancelConfirmed bool
	applyMaterial(c, ui.Column(c.Key("detail-task-card")).FillWidth().MinWidth(0).Gap(tok.Density.CardGap).Padding(tok.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
			heading(c, text(t.Bundle)+" · "+t.TaskId, 2)
			statusBadge(c, taskLabel(t), runColor(c, t))
			ui.Box(c).Grow(1)
			wholeRound := s.task.stage != ""
			cancelOption := destructiveOption{
				kind: destructiveCancelRun, id: t.TaskId, label: tr(c, "detail.cancel.label"), confirmLabel: tr(c, "detail.cancel.confirm"),
				prompt:   cancelRunPrompt(wholeRound),
				disabled: (s.busy && s.task.stage == "") || terminal(t.State) || t.State == "cancelling",
			}
			if wholeRound {
				cancelConfirmed = s.quietDestructiveDialog(c, cancelOption)
			} else {
				cancelConfirmed = s.quietDestructiveAction(c, cancelOption)
			}
		})
		applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tok.Density.Row).
			Padding(tok.Density.Row, tok.Density.Stack), materialSunken).Children(func() {
			components.Icon(c, lucide.Folder).Size(14, 14).TextColor(tok.Color.FgSecondary)
			ui.Text(c, trf(c, "detail.run_dir", "path", displayName(text(t.RunPath)))).
				FontSize(tok.Type.Caption).TextColor(tok.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine().TruncateMiddle().Selectable()
		})
		errorView(c, t.Error)
	})
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
	card(c, tr(c, "detail.live_progress_card"), func() { s.detailProgress(c, d) })
	s.resourceView(c, d.Resources)
}

func caseStateKind(state string) stateKind {
	switch state {
	case "done", "complete":
		return stateSuccess
	case "running", "starting":
		return stateActive
	case "failed":
		return stateDanger
	case "cancelled":
		return stateWarning
	default:
		return stateNeutral
	}
}

func (s *viewState) detailProgress(c *ui.Context, d *core.RunTaskDetail) {
	tok := tokens(c)
	elapsed := ""
	if !terminal(d.Task.State) && d.Task.State != "cancelling" && !d.Task.AwaitingService {
		elapsed = s.formatTaskElapsed(c, emptyText(d.Task.StartedAt))
	}
	p := d.Progress
	if p == nil {
		waiting := tr(c, "detail.waiting_progress")
		if elapsed != "" {
			ui.Row(c.Key("detail-waiting-elapsed-" + d.Task.TaskId)).Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
				ui.Text(c, waiting)
				ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(tok.Color.FgSecondary)
			})
		} else {
			ui.Text(c, waiting)
		}
		progressBar(c, -1, tr(c, "detail.progress_bar_label"))
		return
	}
	headline := progressHeadline(p)
	val := progressValue(p.Done, p.Total)
	ui.Row(c.Key("detail-elapsed-" + d.Task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
		ui.Text(c, headline).Bold().FontFeatures("tnum")
		if elapsed != "" {
			ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(tok.Color.FgSecondary)
		}
		ui.Box(c).Grow(1)
		if val >= 0 {
			ui.Text(c, fmt.Sprintf("%.1f%%", val*100)).Bold().FontFeatures("tnum").TextColor(stateColor(c, stateActive))
		}
	})
	progressBar(c, val, tr(c, "detail.progress_bar_label"))

	var passCount, failCount int
	for _, item := range p.Cases {
		if item.State == "failed" {
			failCount++
		} else if q := emptyText(item.QualityState); q == "pass" || q == "passed" {
			passCount++
		}
	}
	applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).
		Padding(tok.Density.Row, tok.Density.Stack), materialSunken).Children(func() {
		components.Icon(c, lucide.Play).Size(13, 13).TextColor(stateColor(c, stateActive))
		ui.Text(c, trf(c, "detail.current_case", "case_id", text(p.CurrentCaseId))).FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgPrimary)
		ui.Box(c).Grow(1)
		if passCount > 0 {
			passKey := "pass"
			inlineBadge(c, fmt.Sprintf("%s %d", reportQuality(&passKey), passCount), stateSuccess, badgeOptions{Size: sizeSM, Style: badgeSoft})
		}
		if failCount > 0 {
			inlineBadge(c, fmt.Sprintf("%s %d", stateLabel("failed"), failCount), stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
		}
	})

	const detailCasePageSize = 50
	totalCases := len(p.Cases)
	pages := max(1, (totalCases+detailCasePageSize-1)/detailCasePageSize)
	s.casesTablePage = min(max(0, s.casesTablePage), pages-1)
	if pages > 1 {
		ui.Row(c).AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
			if outlineIconButton(c, lucide.ArrowLeft, tr(c, "detail.table.prev_page")).Disabled(s.casesTablePage == 0).Clicked() {
				s.casesTablePage--
			}
			ui.Text(c, trf(c, "detail.table.page_status", "page", s.casesTablePage+1, "totalPages", pages, "count", totalCases)).FontSize(tok.Type.Caption).FontFeatures("tnum")
			if outlineIconButton(c, lucide.ArrowRight, tr(c, "detail.table.next_page")).Disabled(s.casesTablePage == pages-1).Clicked() {
				s.casesTablePage++
			}
		})
	}
	start := s.casesTablePage * detailCasePageSize
	visibleCases := p.Cases[start:min(start+detailCasePageSize, totalCases)]
	columns := []ui.TableColumn{
		{Title: tr(c, "detail.col.case"), Width: 180},
		{Title: tr(c, "detail.col.status"), Width: 140},
		{Title: tr(c, "detail.col.category"), Width: 110},
		{Title: tr(c, "detail.col.quality"), Width: 100},
		{Title: tr(c, "detail.col.duration"), Width: 100},
		{Title: tr(c, "detail.col.error_category"), Width: 160},
	}
	s.casesTable.Key = func(i int) any { return visibleCases[i].CaseId }
	tableLabel := tr(c, "detail.cases_table_label")
	pageTable(c, &s.casesTable, tableLabel, columns, len(visibleCases), func(row, col int) {
		item := visibleCases[row]
		switch col {
		case 1:
			inlineBadge(c, stateLabel(item.State), caseStateKind(item.State), badgeOptions{Size: sizeSM, Style: badgeDot})
			return
		case 3:
			qText := caseQuality(item)
			qColor := tok.Color.FgSecondary
			switch emptyText(item.QualityState) {
			case "pass", "passed":
				qColor = stateColor(c, stateSuccess)
			case "fail", "failed":
				qColor = stateColor(c, stateDanger)
			}
			ui.Text(c, qText).TextColor(qColor).Selectable()
			return
		}
		values := []string{item.CaseId, "", optionalText(item.Category), "", formatDurationMs(item.DurationMs), caseErrorCategory(item)}
		cell := ui.Text(c, values[col]).Selectable()
		if col == 4 {
			cell.FontFeatures("tnum")
		}
	}).Height(detailCasesTableHeight(c.Theme(), len(visibleCases))).Label(tableLabel)
}

// detailCasesTableMaxHeight caps the live-progress case table so long runs
// scroll inside the card instead of pushing the resource charts off screen.
const detailCasesTableMaxHeight float32 = 300

// detailCasesTableHeight sizes the live-progress case table to its rows: the
// header plus one row per case, keeping a single row when no case has arrived
// yet, and never growing past detailCasesTableMaxHeight. Row and header
// heights follow components.Table and components.TableRow.
func detailCasesTableHeight(theme *ui.Theme, rows int) float32 {
	rowHeight := theme.Space(8)
	headerHeight := theme.Space(8) + 1
	return min(headerHeight+rowHeight*float32(max(1, rows)), detailCasesTableMaxHeight)
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
