package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) canCleanWorkspaceArtifacts() bool {
	if s.busy || s.actionBusy("clean-workspace-artifacts") || s.running() != nil {
		return false
	}
	if s.preview != nil || s.cleanWorkspaceArtifactsFn != nil {
		return true
	}
	if s.service == nil {
		return false
	}
	if s.service.BoundPaths() != nil {
		return true
	}
	return emptyText(s.settings.Effective.WorkspaceRoot) != "" && s.ready.Workspace.Initialized
}

func (s *viewState) cleanWorkspaceArtifactsButton(c *ui.Context) {
	if dangerButton(c, tr(c, "settings.workspace.cleanup.action")).
		Disabled(!s.canCleanWorkspaceArtifacts()).
		Clicked() {
		s.cleanArtifactsConfirm = true
	}
}

func (s *viewState) cleanWorkspaceArtifactsDialog(c *ui.Context) {
	c.Key("clean-workspace-artifacts-dialog")
	result := studioDestructiveAlertDialog(c, &s.cleanArtifactsConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "settings.workspace.cleanup.title")},
		Title:         tr(c, "settings.workspace.cleanup.title"),
		Message:       tr(c, "settings.workspace.cleanup.message"),
		ConfirmLabel:  tr(c, "settings.workspace.cleanup.confirm"),
		CancelLabel:   tr(c, "common.cancel"),
	})
	if result == components.DialogConfirmed {
		s.cleanWorkspaceArtifacts()
	}
}

func (s *viewState) cleanWorkspaceArtifacts() {
	if !s.canCleanWorkspaceArtifacts() {
		return
	}
	if s.simulate(previewCleanWorkspaceArtifacts{}) {
		return
	}
	doneMsg := l10n.TR("settings.workspace.cleanup.done")
	s.workAs("clean-workspace-artifacts", func() (string, *core.ApiError) {
		var apiErr *core.ApiError
		if s.cleanWorkspaceArtifactsFn != nil {
			_, apiErr = s.cleanWorkspaceArtifactsFn()
		} else {
			_, apiErr = s.service.CleanWorkspaceArtifacts()
		}
		if apiErr != nil {
			return "", apiErr
		}
		s.updateView(func() {
			s.clearCleanedWorkspaceState()
		})
		return doneMsg, nil
	})
}

func (s *viewState) clearCleanedWorkspaceState() {
	s.clearBenchmark()
	s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	s.failure = nil
	s.failurePage = ""
	s.refreshFailure = ""
	s.refreshFailureCode = ""
	s.cleanArtifactsSeq.Add(1)
	s.bumpSettingsEpoch()
	s.progress = nil
	s.candidate = ""
	s.bindingStale = false
	s.history = core.RunHistoryResponse{}
	s.historyError = nil
	s.tasks = nil
	s.reports = nil
	s.reportsError = nil
	s.detail = nil
	s.detailError = nil
	s.detailID = ""
	s.detailMu.Lock()
	s.requestedDetail = ""
	s.detailMu.Unlock()
	s.closeOpenedReport()
}
