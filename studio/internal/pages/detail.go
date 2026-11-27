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
	s.subpageHeader(c, tr(c, "detail.title"), tr(c, "detail.back_dashboard"), func() {
		s.detailID = ""
		s.detailCaseID = ""
	})
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
					if active {
						ui.Row(c).AlignItems(ui.Center).Gap(tok.Density.Tight).
							Padding(tok.Density.Tight, tok.Density.Row).Radius(tok.Radius.Full).
							Background(scale.SubtleBg).Label(taskLabel(t)).Children(func() {
							ambientLifecycleDot(c, scale, true)
							ui.Text(c, taskLabel(t)).FontSize(max(11, tok.Type.ShellLabel)).TextColor(scale.Fg).SingleLine()
						})
					} else {
						inlineBadge(c, taskLabel(t), kind, badgeOptions{Size: sizeSM, Style: badgeSoft})
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
				cancelConfirmed = s.quietDestructiveDialog(c, cancelOption)
			} else {
				cancelConfirmed = s.quietDestructiveAction(c, cancelOption)
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
	s.detailProgressCard(c, d)
	s.resourceView(c, d.Resources)
	s.detailCaseDrawer(c, d)
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
		if isRunning {
			ambientLifecycleDot(c, tok.Color.Active, true)
		} else {
			components.Icon(c, reicon.Play).Size(13, 13).TextColor(tok.Color.FgTertiary)
		}
		ui.Text(c, trf(c, "detail.current_case", "case_id", text(p.CurrentCaseId))).
			FontSize(tok.Type.Caption).FontFeatures("tnum").TextColor(tok.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
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
		{Title: tr(c, "detail.col.case"), MinWidth: 180},
		{Title: tr(c, "detail.col.status"), MinWidth: 120},
		{Title: tr(c, "detail.col.category"), MinWidth: 100},
		{Title: tr(c, "detail.col.quality"), MinWidth: 96},
		{Title: tr(c, "detail.col.duration"), MinWidth: 96},
		{Title: tr(c, "detail.col.error_category"), MinWidth: 150},
	}
	selectableTable(&s.casesTable, func(i int) any { return visibleCases[i].CaseId }, func(i int) string { return visibleCases[i].CaseId })
	tableLabel := tr(c, "detail.cases_table_label")
	applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
		pageTable(c, &s.casesTable, tableLabel, columns, len(visibleCases), func(row, col int) {
			item := visibleCases[row]
			switch col {
			case 0:
				ui.Text(c, item.CaseId).FontFeatures("tnum").SingleLine().Selectable()
				return
			case 1:
				inlineBadge(c, stateLabel(item.State), caseStateKind(item.State), badgeOptions{Size: sizeSM, Style: badgeDot})
				return
			case 2:
				catText := optionalText(item.Category)
				catColor := tok.Color.FgSecondary
				if item.Category == nil || *item.Category == "" {
					catColor = tok.Color.FgTertiary
				}
				ui.Text(c, catText).TextColor(catColor).SingleLine().Selectable()
				return
			case 3:
				qText := caseQuality(item)
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
				errText := caseErrorCategory(item)
				errColor := tok.Color.FgTertiary
				if item.ErrorCategory != nil && *item.ErrorCategory != "" {
					errColor = stateScale(c, stateDanger).Fg
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(tok.Density.Tight).Children(func() {
					ui.Text(c, errText).TextColor(errColor).Grow(1).MinWidth(0).SingleLine().Selectable()
					components.Icon(c, reicon.ChevronRight).Size(14, 14).TextColor(tok.Color.FgTertiary).Role(ui.RoleNone)
				})
				return
			}
		}).Height(detailCasesTableHeight(c.Theme(), len(visibleCases))).Label(tableLabel)
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
