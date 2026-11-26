package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
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
	studioDialog(c, &dialogOpen, components.DialogOptions{
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
	if studioAlertDialog(c, &s.resumeDiscardConfirm, components.AlertDialogOptions{
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
			s.selected = "benchmarks"
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
