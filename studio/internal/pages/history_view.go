package pages

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

const (
	benchmarksTabTasks = iota
	benchmarksTabHistory
)

func historyStatusKind(status string) stateKind {
	switch status {
	case "complete", "completed":
		return stateSuccess
	case "partial", "dirty", "run_incomplete":
		return stateWarning
	case "failed", "invalid":
		return stateDanger
	default:
		return stateNeutral
	}
}

func (s *viewState) canResumeHistoryRun(r core.RunSummary) bool {
	return r.BatchId != nil && r.Resumable && !s.denied[r.Path]
}

func historyModel(r core.RunSummary) string {
	if r.ModelName == nil || strings.TrimSpace(*r.ModelName) == "" {
		return l10n.TR("dashboard.history.model_unrecorded")
	}
	return *r.ModelName
}

func historyBundleDisplay(r core.RunSummary) string {
	if b := strings.TrimSpace(emptyText(r.Bundle)); b != "" {
		return b
	}
	return "—"
}

func historyStarted(r core.RunSummary) string {
	raw := strings.TrimSpace(emptyText(r.StartedAt))
	if raw == "" {
		return "—"
	}
	return formatTimestamp(raw)
}

func (s *viewState) filterHistoryRuns(runs []core.RunSummary) []core.RunSummary {
	q := strings.ToLower(strings.TrimSpace(s.historyQuery))
	filter := s.historyStatusFilter
	if q == "" && filter == "" {
		return runs
	}
	out := make([]core.RunSummary, 0, len(runs))
	for _, r := range runs {
		switch filter {
		case "complete":
			if r.Status != "complete" && r.Status != "completed" {
				continue
			}
		case "incomplete":
			if r.Status == "complete" || r.Status == "completed" {
				continue
			}
		case "resumable":
			if !s.canResumeHistoryRun(r) {
				continue
			}
		}
		if q != "" {
			haystack := strings.ToLower(strings.Join([]string{
				r.RunId,
				emptyText(r.ModelName),
				emptyText(r.Bundle),
				emptyText(r.Engine),
				emptyText(r.BatchId),
			}, " "))
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func (s *viewState) openHistoryRunDetail(path string) {
	s.historyDetailPath = path
}

func (s *viewState) findHistoryRunByPath(path string) (core.RunSummary, bool) {
	if path == "" {
		return core.RunSummary{}, false
	}
	for _, r := range s.historyRuns() {
		if r.Path == path {
			return r, true
		}
	}
	return core.RunSummary{}, false
}

func (s *viewState) findBatchForHistoryRun(path string) *core.BatchSummary {
	for i := range s.history.Batches {
		for _, r := range s.history.Batches[i].Runs {
			if r.Path == path {
				return &s.history.Batches[i]
			}
		}
	}
	return nil
}

func (s *viewState) findReportForRun(r core.RunSummary) string {
	targetPath := cleanMatchPath(r.Path)
	for _, rep := range s.reports {
		if rep.Unreadable {
			continue
		}
		if r.RunId != "" && slices.Contains(rep.RunIds, r.RunId) {
			return rep.ReportId
		}
		if targetPath != "" {
			for _, rp := range rep.RunPaths {
				if cleanMatchPath(emptyText(rp)) == targetPath {
					return rep.ReportId
				}
			}
		}
	}
	return ""
}

func (s *viewState) findTaskForHistoryRun(r core.RunSummary) *core.RunTask {
	targetPath := cleanMatchPath(r.Path)
	if targetPath == "" {
		return nil
	}
	for i := range s.tasks {
		if cleanMatchPath(emptyText(s.tasks[i].RunPath)) == targetPath {
			return &s.tasks[i]
		}
	}
	return nil
}

func (s *viewState) createReportFromPaths(c *ui.Context, paths []string) {
	if len(paths) == 0 {
		return
	}
	if s.simulate(previewCreateReport{paths: paths}) {
		return
	}
	const toastID = "create-report-toast"
	s.enqueueProgressToast(toastID, tr(c, "reports.toast.creating"))
	s.workAs("create-report", func() (string, *core.ApiError) {
		report, e := s.service.CreateReport(core.CreateReportRequest{RunPaths: paths})
		if e != nil {
			s.updateView(func() { s.dismissToast(toastID) })
			return "", e
		}
		reportID := report.ReportId
		s.updateView(func() {
			s.lastManualReportID = reportID
			s.resolveProgressToast(toastID, l10n.TR("reports.toast.created"), l10n.TR("reports.toast.view_report"), func() {
				s.openReport(reportID)
			})
		})
		return "", nil
	})
}

func (s *viewState) rerunFromHistoryRun(r core.RunSummary) {
	s.historyDetailPath = ""
	eng, mod := s.resolveHistoryRerunSelection(r)
	wantBundle := strings.TrimSpace(emptyText(r.Bundle))
	s.newBenchmarkWithSelection(eng, mod)
	if wantBundle != "" && s.progress != nil {
		hasBundle := false
		for _, b := range s.progress.Bundles {
			if b.Bundle == wantBundle {
				hasBundle = true
				break
			}
		}
		if hasBundle {
			s.selectedBundles = map[string]bool{wantBundle: true}
		}
	}
}

func (s *viewState) historyOverlays(c *ui.Context) {
	s.historyRunDrawer(c)
	s.reportSourcesDialog(c)
}

func (s *viewState) historyView(c *ui.Context) {
	t := tokens(c)
	loc := contextLocale(c)
	runs := s.historyRuns()
	filtered := s.filterHistoryRuns(runs)
	filterOptions := []components.NativeSelectOption[string]{
		{Value: "", Label: tr(c, "dashboard.history.filter.all")},
		{Value: "complete", Label: tr(c, "dashboard.history.filter.complete")},
		{Value: "incomplete", Label: tr(c, "dashboard.history.filter.incomplete")},
		{Value: "resumable", Label: tr(c, "dashboard.history.filter.resumable")},
	}
	var emptyAction func()
	if !s.environmentMissing() && !s.roundWorkActive() {
		emptyAction = func() {
			if primaryButton(c, tr(c, "benchmarks.action.new")).Clicked() {
				s.newBenchmark()
			}
		}
	}
	filterableTable(c, filterableTableOptions{
		SearchPlaceholder: tr(c, "dashboard.history.search_placeholder"),
		Query:             &s.historyQuery,
		Filters: func() {
			segmentedControl(c, tr(c, "dashboard.history.filter_label"), &s.historyStatusFilter, filterOptions)
		},
		Actions: func() {
			ui.Box(c).Grow(1)
			if refreshIconButton(c, tr(c, "dashboard.history.refresh"), s.refreshing).Disabled(s.busy).Clicked() {
				s.refreshAction()
			}
			if len(runs) > 0 {
				if outlineIconButton(c, reicon.Plus, tr(c, "reports.action.create_from_history")).Clicked() {
					s.reportDialogOpen = true
					s.reportDiscardConfirm = false
				}
			}
		},
		HasActiveFilters: s.historyStatusFilter != "",
		OnClearFilters: func() {
			s.historyQuery = ""
			s.historyStatusFilter = ""
		},
		TotalCount:       len(runs),
		FilteredCount:    len(filtered),
		EmptyTitle:       tr(c, "dashboard.history.empty_title"),
		EmptyDescription: tr(c, "dashboard.history.empty_desc"),
		EmptyAction:      emptyAction,
		Table: func() {
			selectableTable(&s.historyTable, func(i int) any { return filtered[i].Path }, func(i int) string { return filtered[i].RunId })
			c.Key("history-table")
			tableLabel := tr(c, "dashboard.history.table_label")
			pageTable(c, &s.historyTable, tableLabel, []ui.TableColumn{
				{Title: tr(c, "dashboard.history.col_model"), MinWidth: 210},
				{Title: tr(c, "dashboard.candidate.bundle"), Width: 140, MinWidth: 110},
				{Title: tr(c, "dashboard.completion.status"), Width: 140, MinWidth: 100},
				{Title: tr(c, "dashboard.history.col_score"), MinWidth: 165},
				{Title: tr(c, "dashboard.history.col_started"), MinWidth: 155},
			}, len(filtered), func(row, col int) {
				r := filtered[row]
				switch col {
				case 0:
					modelTitle := historyModel(r)
					ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).MinWidth(0).Children(func() {
						modelIconBox(c, s.modelIconHint(modelTitle), 22, t.Radius.XS)
						ui.Text(c, modelTitle).Bold().SingleLine().Grow(1).MinWidth(0)
					})
				case 1:
					ui.Text(c, historyBundleDisplay(r)).SingleLine()
				case 2:
					ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						inlineBadge(c, stateLabelFor(loc, r.Status), historyStatusKind(r.Status), badgeOptions{Size: sizeSM, Style: badgeDot})
						if s.canResumeHistoryRun(r) {
							inlineBadge(c, tr(c, "dashboard.history.badge_resumable"), stateWarning, badgeOptions{Size: sizeSM})
						}
					})
				case 3:
					ui.Text(c, runScore(r)).FontFeatures("tnum").SingleLine()
				case 4:
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, historyStarted(r)).FontFeatures("tnum").TextColor(t.Color.FgSecondary).Grow(1).MinWidth(0).SingleLine()
						components.Icon(c, reicon.ChevronRight).Size(14, 14).Shrink(0).TextColor(t.Color.FgTertiary).Role(ui.RoleNone)
					})
				}
			}).MaxHeight(c.Theme().Space(10)*10 + c.Theme().Space(9)).Label(tableLabel)
			if s.historyTable.clicked || s.historyTable.submitted {
				if idx := selectedRow(&s.historyTable, len(filtered)); idx >= 0 {
					s.openHistoryRunDetail(filtered[idx].Path)
				}
			}
		},
	})
	s.renderHistoryMetadataFooter(c)
}

func (s *viewState) renderHistoryMetadataFooter(c *ui.Context) {
	resultsRoot := strings.TrimSpace(emptyText(s.history.ResultsRoot))
	if resultsRoot == "" {
		resultsRoot = strings.TrimSpace(emptyText(s.settings.Effective.ResultsRoot))
	}
	scannedAt := strings.TrimSpace(formatTimestamp(s.history.ScannedAt))
	if resultsRoot == "" && scannedAt == "" {
		return
	}
	t := tokens(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		if resultsRoot != "" {
			disp := trf(c, "dashboard.history.results_root", "path", resultsRoot)
			pathTxt := ui.Text(c, disp).
				FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).
				MaxWidth(460).SingleLine().TruncateMiddle().Selectable()
			studioTooltipIfTruncated(c, pathTxt, disp, resultsRoot, t.Type.Caption, false)
		}
		if resultsRoot != "" && scannedAt != "" {
			ui.Text(c, "·").FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
		}
		if scannedAt != "" {
			ui.Text(c, trf(c, "dashboard.history.meta_scanned", "time", scannedAt)).
				FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgTertiary).SingleLine()
		}
	})
}

func (s *viewState) historyRunDrawer(c *ui.Context) {
	open := s.historyDetailPath != ""
	var run core.RunSummary
	if open {
		var ok bool
		run, ok = s.findHistoryRunByPath(s.historyDetailPath)
		if !ok {
			open = false
			s.historyDetailPath = ""
		}
	}
	c.Key("history-run-drawer")
	title := tr(c, "dashboard.history.detail_title")
	drawer(c, &open, drawerOptions{
		Label: title,
		Title: title,
		Width: c.Theme().Space(138),
	}, func(panel ui.Element) {
		t := tokens(c)
		loc := contextLocale(c)
		modelTitle := historyModel(run)
		bundleLabel := historyBundleDisplay(run)
		engineLabel := strings.TrimSpace(emptyText(run.Engine))
		sealedLabel := tr(c, "dashboard.history.unsealed")
		sealedKind := stateWarning
		if run.Sealed {
			sealedLabel = tr(c, "dashboard.history.sealed")
			sealedKind = stateInfo
		}

		ui.Scroll(c.Key("history-drawer-scroll-" + run.Path)).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).Padding(t.Card.Padding), materialSunken).Children(func() {
					modelIconBox(c, s.modelIconHint(modelTitle), 36, t.Radius.SM)
					ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, modelTitle).Bold().FontSize(t.Type.CardTitle).SingleLine()
						ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
							if bundleLabel != "—" {
								inlineBadge(c, bundleLabel, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
							}
							if engineLabel != "" {
								s.enginePill(c, engineLabel)
							}
							inlineBadge(c, stateLabelFor(loc, run.Status), historyStatusKind(run.Status), badgeOptions{Size: sizeSM, Style: badgeDot})
							inlineBadge(c, sealedLabel, sealedKind, badgeOptions{Size: sizeSM})
						})
					})
				})

				cols := 2
				if compactLayout(c) {
					cols = 1
				}
				ui.Grid(c).Columns(cols).Gap(t.Density.Row).Children(func() {
					metricCard(c, tr(c, "dashboard.history.col_score"), runScore(run), "", "")
					metricCard(c, tr(c, "dashboard.completion.duration"), runDuration(run), "", "")
				})

				applyMaterial(c, ui.Column(c).FillWidth().Gap(0).Padding(t.Density.Row, t.Card.Padding), materialCard).Children(func() {
					keyValueRow(c, tr(c, "dashboard.history.col_run_id"), run.RunId, true)
					reportKVDivider(c)
					modeVal := tr(c, "dashboard.history.mode_standalone")
					if batch := s.findBatchForHistoryRun(run.Path); batch != nil {
						modeVal = trf(c, "dashboard.history.batch_header", "id", batch.BatchId, "status", aggregateStatus(batch.Runs))
					} else if run.BatchId != nil && *run.BatchId != "" {
						modeVal = *run.BatchId
					}
					keyValueRow(c, tr(c, "dashboard.history.mode_label"), modeVal, false)
					reportKVDivider(c)
					evidenceVal := sealedLabel
					if comp := strings.TrimSpace(emptyText(run.Completeness)); comp != "" {
						evidenceVal = sealedLabel + " · " + stateLabelFor(loc, comp)
					}
					keyValueRow(c, tr(c, "dashboard.history.evidence_label"), evidenceVal, false)
					reportKVDivider(c)
					keyValueRow(c, tr(c, "dashboard.history.col_started"), historyStarted(run), true)
					if fin := strings.TrimSpace(emptyText(run.FinishedAt)); fin != "" {
						reportKVDivider(c)
						keyValueRow(c, tr(c, "dashboard.history.col_finished"), formatTimestamp(fin), true)
					}
					if run.StopReason != nil && strings.TrimSpace(*run.StopReason) != "" {
						reportKVDivider(c)
						keyValueRow(c, tr(c, "dashboard.history.stop_reason_label"), stopReasonLabelFor(c, *run.StopReason), false)
					}
					reportKVDivider(c)
					pathKeyValueRow(c, tr(c, "dashboard.history.run_path_label"), run.Path)
				})
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		studioTooltipGroup(c, ui.Row(c).FillWidth().Shrink(0).Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row), func() {
			if task := s.findTaskForHistoryRun(run); task != nil {
				taskID := task.TaskId
				if outlineIconButton(c, reicon.Info, tr(c, "dashboard.history.live_detail")).Clicked() {
					s.historyDetailPath = ""
					s.selectTask(taskID)
					s.selected = "benchmarks"
					return
				}
			}
			rerunBlocked := ""
			if s.environmentMissing() {
				rerunBlocked = l10n.T(loc, "shell.nav.new_task_hint.env_missing")
			} else if s.roundWorkActive() {
				rerunBlocked = l10n.T(loc, "shell.nav.new_task_hint.round_active")
			}
			if rerunBlocked == "" {
				if outlineIconButton(c, reicon.RotateCw, tr(c, "dashboard.history.rerun_same")).Clicked() {
					s.rerunFromHistoryRun(run)
					return
				}
			} else {
				iconButtonWithTooltip(c, rerunBlocked, func() ui.Element {
					return outlineIconButton(c, reicon.RotateCw, "").Label(tr(c, "dashboard.history.rerun_same")).Disabled(true)
				})
			}
			if s.canResumeHistoryRun(run) {
				if outlineIconButton(c, reicon.Play, tr(c, "dashboard.history.resume_button")).Disabled(s.busy).Clicked() {
					source := run
					s.historyDetailPath = ""
					s.resumeSource = &source
					s.resumeDiscardConfirm = false
					s.endpoint = ""
					s.pid = ""
				}
			} else if run.BatchId != nil && run.Status != "complete" && run.Status != "completed" {
				iconButtonWithTooltip(c, tr(c, "dashboard.history.not_resumable"), func() ui.Element {
					return outlineIconButton(c, reicon.Play, "").Label(tr(c, "dashboard.history.resume_button")).Disabled(true)
				})
			}
			if repID := s.findReportForRun(run); repID != "" {
				if primaryIconButton(c, reicon.Eye, tr(c, "dashboard.completion.view_report")).Clicked() {
					s.historyDetailPath = ""
					s.openReport(repID)
					return
				}
			} else if primaryIconButton(c, reicon.FileText, tr(c, "reports.dialog.create")).Disabled(s.busy || s.actionBusy("create-report")).Clicked() {
				s.historyDetailPath = ""
				s.createReportFromPaths(c, []string{run.Path})
				return
			}
		})
	})
	if !open {
		s.historyDetailPath = ""
	}
}
