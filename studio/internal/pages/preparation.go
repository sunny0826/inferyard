package pages

import (
	"encoding/json"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) loadSettings(v core.SettingsResponse) {
	s.loadThemeSettings(v)
	s.loadLanguageSettings(v)
	s.loadReasoningSettings(v)
	s.loadTraySettings(v)
	s.loadWorkspaceSettings(v)
	s.loadDownloadSourceSettings(v)
	s.loadProxySettings(v)
}

func (s *viewState) loadWorkspaceSettings(v core.SettingsResponse) {
	s.workspace = savedWorkspaceField(v)
}

func savedWorkspaceField(v core.SettingsResponse) string {
	if ws := emptyText(v.Settings.WorkspaceRoot); ws != "" {
		return ws
	}
	return emptyText(v.Effective.WorkspaceRoot)
}

type settingDraft struct {
	workspace, downloadProxy string
}

func (s *viewState) settingsDraft() settingDraft {
	return settingDraft{
		workspace:     s.workspace,
		downloadProxy: s.downloadProxy,
	}
}

func (s *viewState) refreshBatchCurrent(epoch uint64) bool {
	return epoch == s.settingsSequence()
}

func (s *viewState) syncSavedGroups(patch map[string]json.RawMessage, submitted settingDraft, value core.SettingsResponse) {
	previousWorkspace := emptyText(s.settings.Effective.WorkspaceRoot)
	s.settings = value
	s.settingsEpochMu.Lock()
	s.settingsEpoch++
	s.settingsEpochMu.Unlock()
	if _, ok := patch["workspace_root"]; ok {
		if s.workspace == submitted.workspace {
			s.workspace = savedWorkspaceField(value)
		}
		if emptyText(value.Effective.WorkspaceRoot) != previousWorkspace {
			s.resetWorkspace()
		}
	}
	s.syncSavedProxy(patch, submitted, value)
}

func (s *viewState) applySavedGroups(patch map[string]json.RawMessage, submitted settingDraft, value core.SettingsResponse) {
	update := s.win.Update
	if s.updateWindow != nil {
		update = s.updateWindow
	}
	if s.win == nil && s.updateWindow == nil {
		s.syncSavedGroups(patch, submitted, value)
		return
	}
	update(func() { s.syncSavedGroups(patch, submitted, value) })
}

func (s *viewState) persistSettings(patch map[string]json.RawMessage, submitted settingDraft, message string) (string, *core.ApiError) {
	v, e := s.service.UpdateSettings(patch)
	if e == nil {
		s.applySavedGroups(patch, submitted, v)
	}
	return message, e
}

const (
	settingsTabGeneral = iota
	settingsTabAbout
)

func (s *viewState) settingsView(c *ui.Context) {
	s.header(c, tr(c, "nav.settings"))
	if s.settingsTab < settingsTabGeneral || s.settingsTab > settingsTabAbout {
		s.settingsTab = settingsTabGeneral
	}
	ui.Column(c.Key("settings-tabs")).Gap(tokens(c).Density.Section).Children(func() {
		components.Tabs(c, &s.settingsTab, components.TabsUnderline, tr(c, "settings.tab.general"), tr(c, "settings.tab.about"))
		if s.settingsTab == settingsTabAbout {
			s.aboutSettingsView(c)
			return
		}
		s.generalSettingsView(c)
	})
}

func (s *viewState) generalSettingsView(c *ui.Context) {
	s.appearanceSettingsView(c)
	s.languageSettingsView(c)
	s.traySettingsView(c)
	if s.service != nil && s.service.BoundPaths() != nil {
		p := s.service.BoundPaths()
		card(c, tr(c, "settings.dev_paths.title"), func() {
			pathKeyValueRow(c, tr(c, "settings.dev_paths.source"), p.Source)
			pathKeyValueRow(c, "CLI", p.CLI)
			pathKeyValueRow(c, tr(c, "settings.dev_paths.workspace"), p.Workspace)
			pathKeyValueRow(c, tr(c, "settings.dev_paths.results"), p.Results)
			pathKeyValueRow(c, tr(c, "settings.dev_paths.models"), p.Models)
			pathKeyValueRow(c, tr(c, "settings.dev_paths.engines"), p.Engines)
			ui.Row(c).Gap(tokens(c).Density.Row).Children(func() {
				s.cleanWorkspaceArtifactsButton(c)
			})
		})
		s.localInstallationsView(c)
		s.downloadSourceSettingsView(c)
		s.proxySettingsView(c)
		s.updateSettingsView(c)
		return
	}
	settingsFormSection(c, settingsFormOptions{
		Title: tr(c, "settings.workspace.title"),
		Dirty: s.workspaceEdited(),
		Fields: func() {
			s.workspaceField(c)
		},
		Actions: func() {
			if primaryActionButton(c, tr(c, "common.save"), s.actionBusy("save-workspace")).Disabled(!s.canSaveWorkspace()).Clicked() {
				if !s.validateSettings() {
					return
				}
				patch := map[string]json.RawMessage{}
				if s.workspace != emptyText(s.settings.Settings.WorkspaceRoot) {
					patch["workspace_root"], _ = json.Marshal(s.workspace)
				}
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				savedMsg := tr(c, "settings.workspace.saved")
				s.workAs("save-workspace", func() (string, *core.ApiError) { return s.persistSettings(patch, submitted, savedMsg) })
			}
			s.cleanWorkspaceArtifactsButton(c)
		},
	})
	s.localInstallationsView(c)
	s.downloadSourceSettingsView(c)
	s.proxySettingsView(c)
	s.updateSettingsView(c)
}

func (s *viewState) workspaceEdited() bool {
	return s.workspace != emptyText(s.settings.Effective.WorkspaceRoot)
}

func (s *viewState) canSaveWorkspace() bool {
	return s.settingsLoaded && !s.busy && (s.workspaceEdited() || s.settings.Settings.WorkspaceRoot == nil) && s.workspaceValid()
}

func (s *viewState) workspaceValid() bool {
	return settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot)) == ""
}

func (s *viewState) settingsEdited() bool {
	return s.workspaceEdited() || s.proxyEdited()
}

func (s *viewState) settingsUnsaved() bool {
	if !s.settingsLoaded {
		return false
	}
	if s.service != nil && s.service.BoundPaths() != nil {
		return s.proxyEdited()
	}
	return s.settingsEdited()
}

func (s *viewState) settingsDiscardDialog(c *ui.Context) {
	c.Key("settings-discard-dialog")
	result := studioAlertDialog(c, &s.settingsLeaveConfirm, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: tr(c, "settings.discard.label")},
		Title:         tr(c, "settings.discard.title"),
		Message:       tr(c, "settings.discard.message"),
		ConfirmLabel:  tr(c, "settings.discard.confirm"),
		CancelLabel:   tr(c, "settings.discard.cancel"),
	})
	switch result {
	case components.DialogConfirmed:
		s.loadSettings(s.settings)
		action := s.pendingNavAction
		s.pendingNavAction = nil
		if action != nil {
			action()
		}
	case components.DialogCancelled:
		s.pendingNavAction = nil
	}
}

func (s *viewState) applySettings(value core.SettingsResponse) {
	updateForm := !s.settingsLoaded || !s.settingsEdited()
	if emptyText(s.settings.Effective.WorkspaceRoot) != emptyText(value.Effective.WorkspaceRoot) {
		s.resetWorkspace()
	}
	s.settings = value
	if updateForm {
		s.loadSettings(value)
		s.settingsLoaded = true
	} else {
		s.loadThemeSettings(value)
		s.loadLanguageSettings(value)
		s.loadTraySettings(value)
		s.loadDownloadSourceSettings(value)
	}
}

func (s *viewState) workspaceField(c *ui.Context) {
	loc := contextLocale(c)
	ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
		s.pathFieldUnlabeled(c, tr(c, "settings.workspace.label"), &s.workspace, browseWorkspace).
			Description(directoryStateFor(loc, s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot))).
			Error(s.browseError(browseWorkspace.key, settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot))))
	})
}

func directoryState(value string, readiness core.DirectoryReadiness, previous string) string {
	return directoryStateFor("", value, readiness, previous)
}

func directoryStateFor(locale, value string, readiness core.DirectoryReadiness, previous string) string {
	switch {
	case value == "" && previous == "":
		return l10n.T(locale, "settings.dir.unconfigured")
	case value == "":
		return l10n.T(locale, "settings.dir.empty_configured")
	}
	state := l10n.T(locale, "settings.workspace.modified_pending_save")
	if samePath(value, previous) {
		if readiness.Path == nil || !samePath(value, *readiness.Path) {
			return l10n.T(locale, "settings.workspace.pending_check")
		}
		state = l10n.T(locale, "settings.dir.not_exists")
		if readiness.Exists {
			state = l10n.T(locale, "settings.dir.exists")
			if readiness.Initialized {
				state = l10n.T(locale, "settings.dir.initialized")
			}
		}
	}
	return state
}

func samePath(value, previous string) bool {
	return previous != "" && filepath.Clean(value) == filepath.Clean(previous)
}
