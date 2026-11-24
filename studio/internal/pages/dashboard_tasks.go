package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func aggregateStatus(runs []core.RunSummary) string {
	priority := map[string]int{"complete": 1, "partial": 2, "dirty": 3, "cancelled": 4}
	state := ""
	for _, r := range runs {
		if priority[r.Status] > priority[state] {
			state = r.Status
		}
	}
	return stateLabel(state)
}
func (s *viewState) historyRow(c *ui.Context, r core.RunSummary) {
	t := tokens(c)
	ui.Column(c.Key(r.Path)).Gap(t.Density.Inset).Padding(t.Density.CardGap).Children(func() {
		ui.Text(c, r.RunId+" · "+stateLabel(r.Status)).Bold()
		ui.Text(c, text(r.ModelName)+" · "+text(r.Bundle)+" · "+text(r.Engine))
		ui.Text(c, displayName(r.Path)).Selectable()
		sealed := tr(c, "dashboard.history.unsealed")
		if r.Sealed {
			sealed = tr(c, "dashboard.history.sealed")
		}
		ui.Text(c, trf(c, "dashboard.history.sealed_completeness", "sealed", sealed, "completeness", stateLabel(text(r.Completeness))))
		if r.StopReason != nil {
			ui.Text(c, trf(c, "dashboard.history.stop_reason", "reason", stopReasonLabel(*r.StopReason)))
		}
		ui.Text(c, trf(c, "dashboard.history.score_line", "score", runScore(r)))
		componentFold(c, tr(c, "dashboard.history.raw_fold"), func() {
			ui.Text(c, trf(c, "dashboard.history.timestamps", "start", formatTimestampOrMissingUTC(r.StartedAt), "end", formatTimestampOrMissingUTC(r.FinishedAt))).Selectable()
			if r.StopReason != nil {
				ui.Text(c, trf(c, "dashboard.history.raw_stop_reason", "reason", *r.StopReason)).Selectable()
			}
			ui.Text(c, trf(c, "dashboard.history.relation", "relation", text(r.Relation), "parent", text(r.ParentRunId))).Selectable()
		})
		ui.Row(c).Wrap().Gap(tokens(c).Density.CardGap).Children(func() {
			button(c, tr(c, "dashboard.history.open_task")).OnClick(func() {
				s.detailID = ""
				s.resumeSource = nil
				s.focusedRun = r.Path
				s.selected = "dashboard"
			})
			if button(c, tr(c, "dashboard.history.resume_button")).AlignSelf(ui.Start).Disabled(s.busy || r.BatchId == nil || !r.Resumable || s.denied[r.Path]).Clicked() {
				source := r
				s.resumeSource = &source
				s.resumeDiscardConfirm = false
				s.endpoint = ""
				s.pid = ""
			}
			for _, task := range s.tasks {
				if emptyText(task.RunPath) == r.Path && button(c, tr(c, "dashboard.history.live_detail")).AlignSelf(ui.Start).Clicked() {
					s.selectTask(task.TaskId)
					s.selected = "dashboard"
				}
			}
		})

		if r.BatchId == nil {
			ui.Text(c, tr(c, "dashboard.history.standalone_no_resume"))
		} else if !r.Resumable || s.denied[r.Path] {
			ui.Text(c, tr(c, "dashboard.history.not_resumable"))
		}

	})
}
func (s *viewState) resumeDirty() bool {
	return strings.TrimSpace(s.endpoint) != "" || strings.TrimSpace(s.pid) != ""
}

func (s *viewState) closeResumeDialog() {
	s.resumeSource = nil
	s.resumeDiscardConfirm = false
	s.endpoint = ""
	s.pid = ""
}

func (s *viewState) resumeDialog(c *ui.Context) {
	open := s.resumeSource != nil
	dirty := open && s.resumeDirty()
	dialogOpen := open
	c.Key("resume-dialog")
	components.Dialog(c, &dialogOpen, components.DialogOptions{
		Label:                tr(c, "dashboard.resume.dialog_label"),
		DisableBackdropClose: s.resumeDiscardConfirm,
		DisableEscapeClose:   s.resumeDiscardConfirm,
	}, func(panel ui.Element) {
		if s.resumeSource == nil {
			return
		}
		panel.Width(c.Theme().Space(115)).MaxWidthPercent(90).Children(func() {
			ui.Column(c).Gap(tokens(c).Density.CardGap).Children(func() {
				heading(c, trf(c, "dashboard.resume.heading", "id", s.resumeSource.RunId), 2)
				ui.Text(c, displayName(s.resumeSource.Path)).Selectable().TextColor(c.Theme().TextMuted)
				endpointError, pidError := resumeErrors(s.endpoint, s.pid)
				field(c, tr(c, "dashboard.resume.endpoint_label"), &s.endpoint).Description(tr(c, "dashboard.resume.endpoint_desc")).Error(endpointError)
				field(c, tr(c, "dashboard.resume.pid_label"), &s.pid).Description(tr(c, "dashboard.resume.pid_desc")).Error(pidError)
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.CardGap).Children(func() {
					if primaryActionButton(c, tr(c, "dashboard.resume.confirm_button"), s.actionBusy("resume-run")).AlignSelf(ui.Start).Disabled(s.busy || s.denied[s.resumeSource.Path]).Clicked() {
						s.resume()
					}
					if button(c, tr(c, "dashboard.resume.close_button")).AlignSelf(ui.Start).Clicked() {
						if s.resumeDirty() {
							s.resumeDiscardConfirm = true
						} else {
							dialogOpen = false
						}
					}
				})
			})
		})
	})
	if open && !dialogOpen && s.resumeSource != nil {
		if dirty {
			s.resumeDiscardConfirm = true
		} else {
			s.closeResumeDialog()
		}
	}
	if components.AlertDialog(c, &s.resumeDiscardConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "dashboard.resume.discard_label")},
		Title:         tr(c, "dashboard.resume.discard_title"),
		Message:       tr(c, "dashboard.resume.discard_message"),
		ConfirmLabel:  tr(c, "dashboard.resume.discard_confirm"),
		CancelLabel:   tr(c, "dashboard.resume.discard_cancel"),
	}) == components.DialogConfirmed {
		s.closeResumeDialog()
	}
}
func (s *viewState) resume() {
	ee, pe := resumeErrors(s.endpoint, s.pid)
	if ee != "" || pe != "" {
		message := ee
		if pe != "" {
			message = pe
		}
		s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
		return
	}
	if s.simulate(previewStartRun{bundle: emptyText(s.resumeSource.Bundle), resumePath: s.resumeSource.Path}) {
		return
	}
	req := core.ResumeRunRequest{FromRun: s.resumeSource.Path}
	endpoint, pid := strings.TrimSpace(s.endpoint), strings.TrimSpace(s.pid)
	if pid != "" {
		value, _ := strconv.ParseInt(pid, 10, 64)
		req.EndpointUrl = &endpoint
		req.ServerPid = &value
	}
	s.workAs("resume-run", func() (string, *core.ApiError) {
		task, e := s.service.Resume(req)
		s.win.Update(func() {
			if e != nil {
				if e.Details["reason"] == "not_resumable" {
					s.denied[req.FromRun] = true
				}
				return
			}
			s.closeResumeDialog()
			s.selected = "dashboard"
			s.selectTask(task.TaskId)
		})
		return l10n.TR("dashboard.toast.resume_queued"), e
	})
}

func selectableTable(state *pageTableState, key func(int) any, label func(int) string) {
	if state.Selected == nil {
		index := -1
		state.Selected = &index
	}
	state.Key, state.Label = key, label
}

func selectedRow(state *pageTableState, count int) int {
	if state.Selected == nil || *state.Selected < 0 || *state.Selected >= count {
		return -1
	}
	return *state.Selected
}

func runScore(r core.RunSummary) string {
	if r.Score == nil {
		return l10n.TR("dashboard.score.unrecorded")
	}
	rate := l10n.TR("dashboard.score.no_valid_cases")
	switch gap := r.Score.RateGap(); {
	case gap != "":
		rate = l10n.TRf("dashboard.score.with_gap", "gap", gapDescription(*r.Score, gap))
	case r.Score.Rate != nil:
		rate = fmt.Sprintf("%.1f%%", *r.Score.Rate*100)
	}
	if r.Score.Failed > 0 {
		rate += l10n.TRf("dashboard.score.with_failed", "count", r.Score.Failed)
	}
	return fmt.Sprintf("%d/%d · %s", r.Score.Passed, r.Score.Valid, rate)
}

func (s *viewState) historyView(c *ui.Context) {
	ui.Text(c, trf(c, "dashboard.history.results_root", "path", configuredValue(displayName(emptyText(s.history.ResultsRoot)))))
	ui.Text(c, trf(c, "dashboard.history.scanned_at", "time", configuredValue(formatTimestamp(s.history.ScannedAt))))
	runs := s.historyRuns()
	if len(runs) == 0 && s.historyError == nil && s.loaded {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:    lucide.Clock,
			Title:   tr(c, "dashboard.history.empty_title"),
			Surface: emptySunken,
		})
		return
	}
	selectableTable(&s.historyTable, func(i int) any { return runs[i].Path }, func(i int) string { return runs[i].RunId })
	c.Key("history-table")
	tableLabel := tr(c, "dashboard.history.table_label")
	pageTable(c, &s.historyTable, tableLabel, []ui.TableColumn{
		{Title: tr(c, "dashboard.history.col_model"), Width: 190},
		{Title: tr(c, "dashboard.candidate.bundle"), Width: 130},
		{Title: tr(c, "dashboard.completion.status"), Width: 70},
		{Title: tr(c, "dashboard.history.col_score"), Width: 170},
		{Title: tr(c, "dashboard.history.col_started"), Width: 170},
	}, len(runs), func(row, col int) {
		r := runs[row]
		ui.Text(c, []string{historyModel(r), text(r.Bundle), stateLabel(r.Status), runScore(r), historyStarted(r)}[col])
	}).Height(c.Theme().Space(8)*float32(min(len(runs), 8)+1) + 2).Label(tableLabel)
	if i := selectedRow(&s.historyTable, len(runs)); i >= 0 {
		r := runs[i]
		s.historyIdentity(c, r)
		for _, batch := range s.history.Batches {
			for _, run := range batch.Runs {
				if run.Path == r.Path {
					ui.Text(c, trf(c, "dashboard.history.batch_header", "id", batch.BatchId, "status", aggregateStatus(batch.Runs)))
					ui.Text(c, displayName(batch.Path)).Selectable()
					break
				}
			}
		}
		s.historyRow(c, r)
	}
}

func historyModel(r core.RunSummary) string {
	if r.ModelName == nil || *r.ModelName == "" {
		return l10n.TR("dashboard.history.model_unrecorded")
	}
	return *r.ModelName
}

func historyStarted(r core.RunSummary) string {
	raw := emptyText(r.StartedAt)
	if raw == "" {
		return l10n.TR("dashboard.history.unrecorded")
	}
	return formatTimestamp(raw)
}

func formatTimestampOrMissingUTC(p *string) string {
	if p == nil || *p == "" {
		return l10n.TR("detail.missing_value")
	}
	return formatTimestampUTC(*p)
}

func (s *viewState) historyIdentity(c *ui.Context, r core.RunSummary) {
	line := trf(c, "dashboard.history.identity_standalone", "run_id", r.RunId)
	if r.BatchId != nil {
		line = trf(c, "dashboard.history.identity_batch", "run_id", r.RunId, "batch_id", *r.BatchId)
	}
	ui.Text(c, line).Selectable().TextColor(c.Theme().TextMuted)
}
