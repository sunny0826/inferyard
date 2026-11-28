package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func runTaskStateKind(task core.RunTask) stateKind {
	if task.Queued && task.State == "starting" {
		return stateNeutral
	}
	if task.Exit != nil {
		switch task.Exit.ExitCode {
		case 0:
			return stateSuccess
		case 2, 3:
			return stateWarning
		case 4:
			return stateDanger
		case 130:
			return stateNeutral
		}
	}
	switch task.State {
	case "starting", "running", "cancelling":
		return stateActive
	case "done":
		return stateSuccess
	case "failed":
		return stateDanger
	case "cancelled":
		return stateWarning
	}
	return stateNeutral
}

func runTaskIcon(kind stateKind) func() *ui.SVG {
	switch kind {
	case stateActive:
		return reicon.Activity
	case stateSuccess:
		return reicon.CircleCheck
	case stateDanger:
		return reicon.CircleAlert
	case stateWarning:
		return reicon.TriangleAlert
	default:
		return reicon.FileText
	}
}

func (s *viewState) detailView(c *ui.Context) {
	tok := tokens(c)
	s.subpageHeader(c, tr(c, "detail.title"))
	if s.detailError != nil {
		errorRecoveryBanner(c, s.detailError, func() {
			refreshIconButton(c, tr(c, "detail.retry_load"), false).OnClick(func() { s.selectTask(s.detailID) })
		})
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
	s.syncViewedDetailTask(d.Task.TaskId)
	t := d.Task
	kind := runTaskStateKind(t)
	scale := stateScale(c, kind)
	active := !terminal(t.State) && t.State != "cancelling" && !t.Queued
	var cancelConfirmed bool
	applyMaterial(c, ui.Column(c.Key("detail-task-card")).FillWidth().MinWidth(0).Gap(tok.Density.CardGap).Padding(tok.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Stack).Children(func() {
			ui.Box(c).Size(36, 36).Radius(tok.Radius.Inset).Shrink(0).
				Background(scale.SubtleBg).Border(1, scale.Border).
				AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				components.Icon(c, runTaskIcon(kind)).Size(17, 17).TextColor(semanticAccentColor(c, scale))
			})
			ui.Column(c).Grow(1).MinWidth(180).Gap(2).Children(func() {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
					heading(c, text(t.Bundle), 2)
					locLabel := taskLabelFor(contextLocale(c), t)
					if active {
						pill := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(tok.Density.Tight).
							Padding(tok.Density.Tight, tok.Density.Row).Radius(tok.Radius.Full).
							Background(scale.SubtleBg).Label(locLabel)
						if c.Preferences().HighContrast {
							pill.Background(tok.Color.Surface).Border(1, scale.Border)
						}
						pill.Children(func() {
							ambientLifecycleDot(c, scale, true)
							ui.Text(c, locLabel).FontSize(c.Theme().Rem(.86)).FontFeatures("tnum").TextColor(scale.Fg).SingleLine()
						})
					} else {
						inlineBadge(c, locLabel, kind, badgeOptions{Size: sizeSM, Style: badgeSoft})
					}
				})
				ui.Text(c, t.TaskId).FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgSecondary).SingleLine().Selectable()
			})
			wholeRound := s.task.stage != ""
			cancelOption := destructiveOption{
				kind: destructiveCancelRun, id: t.TaskId, label: tr(c, "detail.cancel.label"), confirmLabel: tr(c, "detail.cancel.confirm"),
				prompt:   cancelRunPrompt(wholeRound),
				disabled: (s.busy && s.task.stage == "") || terminal(t.State) || t.State == "cancelling",
			}
			if wholeRound {
				cancelConfirmed = s.dangerIconDestructiveDialog(c, reicon.Square, cancelOption)
			} else {
				cancelConfirmed = s.dangerIconDestructiveAction(c, reicon.Square, cancelOption)
			}
		})
		runPathRaw := emptyText(t.RunPath)
		applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tok.Density.Row).
			Padding(tok.Density.Row, tok.Density.Stack), materialSunken).Children(func() {
			components.Icon(c, reicon.Folder).Size(14, 14).TextColor(tok.Color.FgSecondary).Shrink(0)
			disp := trf(c, "detail.run_dir", "path", displayName(text(t.RunPath)))
			pathEl := ui.Text(c, disp).
				FontSize(tok.Type.Caption).TextColor(tok.Color.FgSecondary).Grow(1).MinWidth(0).SingleLine().TruncateMiddle().Selectable()
			if runPathRaw != "" {
				studioTooltipIfTruncated(c, pathEl, disp, runPathRaw, tok.Type.Caption, false)
			}
		})
		errorView(c, t.Error)
	})
	if cancelConfirmed {
		if s.task.stage != "" {
			s.newBenchmarkWithSelection(s.engine, s.model)
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
	s.detailProgressCard(c, d)
	s.resourceViewFor(c, d.Task.TaskId, d.Resources)
	s.detailCaseDrawer(c, d)
}

func (s *viewState) activeRunDetailSnapshot() *core.RunTaskDetail {
	task := s.running()
	if task == nil {
		return nil
	}
	if s.activeRunDetail != nil && s.activeRunDetail.Task.TaskId == task.TaskId {
		return s.activeRunDetail
	}
	if s.detail != nil && s.detail.Task.TaskId == task.TaskId {
		return s.detail
	}
	if s.preview != nil {
		if d := s.preview.detail(task.TaskId); d != nil {
			return d
		}
	}
	return &core.RunTaskDetail{
		Task:     *task,
		Progress: s.runProgress,
	}
}

func (s *viewState) inlineRunDetailView(c *ui.Context) {
	d := s.activeRunDetailSnapshot()
	if d == nil {
		return
	}
	s.syncViewedDetailTask(d.Task.TaskId)
	s.inlineDetailCasesCard(c, d)
	s.resourceViewFor(c, d.Task.TaskId, d.Resources)
	s.detailCaseDrawer(c, d)
}

func (s *viewState) inlineDetailCasesCard(c *ui.Context, d *core.RunTaskDetail) {
	tok := tokens(c)
	isRunning := !terminal(d.Task.State) && d.Task.State != "cancelling"
	applyMaterial(c, ui.Column(c.Key("inline-detail-cases-card-"+d.Task.TaskId)).FillWidth().MinWidth(0).Gap(tok.Density.CardGap).Padding(tok.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
			heading(c, tr(c, "detail.live_progress_card"), 2)
			ui.Box(c).Grow(1)
			if d.Progress != nil {
				passCount, failCount := countCaseOutcomes(d.Progress.Cases)
				if passCount > 0 {
					passKey := "pass"
					inlineBadge(c, fmt.Sprintf("%s %d", reportQualityFor(c, &passKey), passCount), stateSuccess, badgeOptions{Size: sizeSM, Style: badgeSoft})
				}
				if failCount > 0 {
					inlineBadge(c, fmt.Sprintf("%s %d", stateLabelFor(contextLocale(c), "failed"), failCount), stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
				}
			}
		})
		if runPathRaw := emptyText(d.Task.RunPath); runPathRaw != "" {
			applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tok.Density.Row).
				Padding(tok.Density.Row, tok.Density.Stack), materialSunken).Children(func() {
				components.Icon(c, reicon.Folder).Size(14, 14).TextColor(tok.Color.FgSecondary).Shrink(0)
				disp := trf(c, "detail.run_dir", "path", displayName(text(d.Task.RunPath)))
				pathEl := ui.Text(c, disp).
					FontSize(tok.Type.Caption).TextColor(tok.Color.FgSecondary).Grow(1).MinWidth(0).SingleLine().TruncateMiddle().Selectable()
				studioTooltipIfTruncated(c, pathEl, disp, runPathRaw, tok.Type.Caption, false)
			})
		}
		if d.Progress == nil {
			ui.Row(c.Key("inline-detail-waiting-" + d.Task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
				ambientLifecycleDot(c, tok.Color.Active, isRunning)
				ui.Text(c, tr(c, "detail.waiting_progress")).FontSize(tok.Type.Caption).TextColor(tok.Color.FgSecondary)
			})
			return
		}
		s.detailCasesTable(c, d.Progress)
	})
}

func (s *viewState) detailProgressCard(c *ui.Context, d *core.RunTaskDetail) {
	tok := tokens(c)
	elapsed := ""
	if !terminal(d.Task.State) && d.Task.State != "cancelling" && !d.Task.AwaitingService {
		elapsed = s.formatTaskElapsed(c, emptyText(d.Task.StartedAt))
	}
	applyMaterial(c, ui.Column(c.Key("detail-progress-card-"+d.Task.TaskId)).FillWidth().MinWidth(0).Gap(tok.Density.CardGap).Padding(tok.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
			heading(c, tr(c, "detail.live_progress_card"), 2)
			ui.Box(c).Grow(1)
			if elapsed != "" {
				applyMaterial(c, ui.Row(c).AlignItems(ui.Center).Gap(tok.Density.Tight).
					Padding(tok.Density.Hairline, tok.Density.Row), materialSunken).Children(func() {
					components.Icon(c, reicon.Clock).Size(12, 12).TextColor(tok.Color.FgSecondary)
					ui.Text(c, elapsed).FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgPrimary).SingleLine()
				})
			}
		})
		s.detailProgress(c, d)
	})
}

func caseStateKind(state string) stateKind {
	switch state {
	case "done", "complete", "completed":
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
	p := d.Progress
	isRunning := !terminal(d.Task.State) && d.Task.State != "cancelling"
	if p == nil {
		ui.Row(c.Key("detail-waiting-elapsed-" + d.Task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
			ambientLifecycleDot(c, tok.Color.Active, isRunning)
			ui.Text(c, tr(c, "detail.waiting_progress")).FontSize(tok.Type.Caption).TextColor(tok.Color.FgSecondary)
		})
		progressBar(c, -1, tr(c, "detail.progress_bar_label")).Height(10)
		return
	}
	headline := progressHeadline(p)
	val := progressValue(p.Done, p.Total)
	ui.Row(c.Key("detail-elapsed-" + d.Task.TaskId)).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).Children(func() {
		ui.Text(c, headline).Bold().FontSize(tok.Type.Section).FontFeatures("tnum")
		ui.Box(c).Grow(1)
		if val >= 0 {
			pctColor := stateColor(c, stateActive)
			if val >= 1 {
				pctColor = stateColor(c, stateSuccess)
			}
			ui.Text(c, fmt.Sprintf("%.1f%%", val*100)).Bold().FontSize(tok.Type.Metric).FontFeatures("tnum").TextColor(pctColor)
		}
	})
	progressBar(c, val, tr(c, "detail.progress_bar_label")).Height(10)

	passCount, failCount := countCaseOutcomes(p.Cases)
	applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row).
		Padding(tok.Density.Row, tok.Density.Stack), materialSunken).Children(func() {
		if isRunning {
			ambientLifecycleDot(c, tok.Color.Active, true)
		} else {
			components.Icon(c, reicon.Play).Size(13, 13).TextColor(tok.Color.FgTertiary)
		}
		ui.Text(c, trf(c, "detail.current_case", "case_id", text(p.CurrentCaseId))).
			FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
		if passCount > 0 {
			passKey := "pass"
			inlineBadge(c, fmt.Sprintf("%s %d", reportQualityFor(c, &passKey), passCount), stateSuccess, badgeOptions{Size: sizeSM, Style: badgeSoft})
		}
		if failCount > 0 {
			inlineBadge(c, fmt.Sprintf("%s %d", stateLabelFor(contextLocale(c), "failed"), failCount), stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
		}
	})

	s.detailCasesTable(c, p)
}

func countCaseOutcomes(cases []core.CaseProgress) (passCount, failCount int) {
	for _, item := range cases {
		if item.State == "failed" {
			failCount++
		} else if q := emptyText(item.QualityState); q == "pass" || q == "passed" {
			passCount++
		}
	}
	return passCount, failCount
}

func (s *viewState) detailCasesTable(c *ui.Context, p *core.RunProgress) {
	tok := tokens(c)
	const detailCasePageSize = 50
	totalCases := len(p.Cases)
	pages := max(1, (totalCases+detailCasePageSize-1)/detailCasePageSize)
	s.casesTablePage = min(max(0, s.casesTablePage), pages-1)
	if pages > 1 {
		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(tok.Density.Row), func() {
			ui.Text(c, trf(c, "detail.table.page_status", "page", s.casesTablePage+1, "totalPages", pages, "count", totalCases)).
				FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgSecondary)
			ui.Box(c).Grow(1)
			if outlineIconButton(c, reicon.ArrowLeft, tr(c, "detail.table.prev_page")).Disabled(s.casesTablePage == 0).Clicked() {
				s.casesTablePage--
			}
			if outlineIconButton(c, reicon.ArrowRight, tr(c, "detail.table.next_page")).Disabled(s.casesTablePage == pages-1).Clicked() {
				s.casesTablePage++
			}
		})
	}
	start := s.casesTablePage * detailCasePageSize
	visibleCases := p.Cases[start:min(start+detailCasePageSize, totalCases)]
	columns := []ui.TableColumn{
		{Title: tr(c, "detail.col.case"), MinWidth: 160},
		{Title: tr(c, "detail.col.status"), MinWidth: 108},
		{Title: tr(c, "detail.col.category"), MinWidth: 96},
		{Title: tr(c, "detail.col.quality"), MinWidth: 88},
		{Title: tr(c, "detail.col.duration"), MinWidth: 88},
		{Title: tr(c, "detail.col.error_category"), MinWidth: 132},
	}
	selectableTable(&s.casesTable, func(i int) any { return visibleCases[i].CaseId }, func(i int) string { return visibleCases[i].CaseId })
	tableLabel := tr(c, "detail.cases_table_label")
	loc := contextLocale(c)
	applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
		tbl := pageTable(c, &s.casesTable, tableLabel, columns, len(visibleCases), func(row, col int) {
			item := visibleCases[row]
			switch col {
			case 0:
				ui.Text(c, item.CaseId).FontFeatures("tnum").SingleLine().Selectable()
				return
			case 1:
				inlineBadge(c, stateLabelFor(loc, item.State), caseStateKind(item.State), badgeOptions{Size: sizeSM, Style: badgeDot})
				return
			case 2:
				catText := optionalTextFor(c, item.Category)
				catColor := tok.Color.FgSecondary
				if item.Category == nil || *item.Category == "" {
					catColor = tok.Color.FgTertiary
				}
				ui.Text(c, catText).TextColor(catColor).SingleLine().Selectable()
				return
			case 3:
				qText := caseQualityFor(c, item)
				switch emptyText(item.QualityState) {
				case "pass", "passed":
					inlineBadge(c, qText, stateSuccess, badgeOptions{Size: sizeSM, Style: badgeSoft})
				case "fail", "failed":
					inlineBadge(c, qText, stateDanger, badgeOptions{Size: sizeSM, Style: badgeSoft})
				case "unscorable":
					inlineBadge(c, qText, stateWarning, badgeOptions{Size: sizeSM, Style: badgeSoft})
				default:
					ui.Text(c, qText).FontSize(tok.Type.Caption).TextColor(tok.Color.FgTertiary).SingleLine().Selectable()
				}
				return
			case 4:
				durText := formatDurationMs(item.DurationMs)
				durColor := tok.Color.FgPrimary
				if item.DurationMs == nil {
					durColor = tok.Color.FgTertiary
				}
				ui.Text(c, durText).FontFeatures("tnum").TextColor(durColor).SingleLine().Selectable()
				return
			case 5:
				errText := caseErrorCategoryFor(c, item)
				errColor := tok.Color.FgTertiary
				if item.ErrorCategory != nil && *item.ErrorCategory != "" {
					errColor = stateScale(c, stateDanger).Fg
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tok.Density.Tight).Children(func() {
					ui.Text(c, errText).TextColor(errColor).Grow(1).MinWidth(0).SingleLine().Selectable()
					components.Icon(c, reicon.ChevronRight).Size(14, 14).Shrink(0).TextColor(tok.Color.FgTertiary).Role(ui.RoleNone)
				})
				return
			}
		}).Label(tableLabel)
		if len(visibleCases) == 0 {
			tbl.Height(detailCasesTableHeight(c.Theme(), 0))
		} else {
			tbl.MaxHeight(detailCasesTableMaxHeight)
		}
	})
	if s.casesTable.clicked || s.casesTable.submitted {
		if idx := selectedRow(&s.casesTable, len(visibleCases)); idx >= 0 {
			s.detailCaseID = visibleCases[idx].CaseId
		}
	}
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
	return caseQualityFor(nil, item)
}

func caseQualityFor(c *ui.Context, item core.CaseProgress) string {
	if item.QualityState == nil || *item.QualityState == "" {
		return tr(c, "detail.missing_value")
	}
	return reportQualityFor(c, item.QualityState)
}

func caseErrorCategory(item core.CaseProgress) string {
	return caseErrorCategoryFor(nil, item)
}

func caseErrorCategoryFor(c *ui.Context, item core.CaseProgress) string {
	if item.ErrorCategory != nil && *item.ErrorCategory != "" {
		return *item.ErrorCategory
	}
	if item.State == "failed" {
		return tr(c, "detail.missing_value")
	}
	return "—"
}

func optionalText(value *string) string {
	return optionalTextFor(nil, value)
}

func optionalTextFor(c *ui.Context, value *string) string {
	if value == nil || *value == "" {
		return tr(c, "detail.missing_value")
	}
	return *value
}
