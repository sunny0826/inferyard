package pages

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) loadSettings(v core.SettingsResponse) {
	s.loadThemeSettings(v)
	s.loadLanguageSettings(v)
	s.loadTraySettings(v)
	s.loadPathSettings(v)
	s.loadDownloadNames(v)
	s.loadProxySettings(v)
}

func (s *viewState) loadPathSettings(v core.SettingsResponse) {
	s.cliPath = emptyText(v.Settings.InferyardPath)
	s.workspace = emptyText(v.Settings.WorkspaceRoot)
	if s.workspace == "" {
		s.workspace = emptyText(v.Effective.WorkspaceRoot)
	}
	s.results = emptyText(v.Settings.ResultsRoot)
	s.models = emptyText(v.Settings.ModelsRoot)
}

func (s *viewState) loadDownloadNames(v core.SettingsResponse) {
	s.modelName, s.engineName = v.Settings.ModelDownloadName, v.Settings.EngineDownloadName
	if s.modelName == "" {
		s.modelName = "model"
	}
	if s.engineName == "" {
		s.engineName = "engine"
	}
}

func settingsPatchGroups(patch map[string]json.RawMessage) (paths, names bool) {
	for _, key := range []string{"inferyard_path", "workspace_root", "results_root", "models_root"} {
		if _, ok := patch[key]; ok {
			paths = true
		}
	}
	for _, key := range []string{"model_download_name", "engine_download_name"} {
		if _, ok := patch[key]; ok {
			names = true
		}
	}
	return paths, names
}

type settingDraft struct {
	cliPath, workspace, results, models, modelName, engineName, downloadProxy string
}

func (s *viewState) settingsDraft() settingDraft {
	return settingDraft{
		cliPath: s.cliPath, workspace: s.workspace, results: s.results, models: s.models,
		modelName: s.modelName, engineName: s.engineName, downloadProxy: s.downloadProxy,
	}
}

func savedPathFields(v core.SettingsResponse) (cliPath, workspace, results, models string) {
	workspace = emptyText(v.Settings.WorkspaceRoot)
	if workspace == "" {
		workspace = emptyText(v.Effective.WorkspaceRoot)
	}
	return emptyText(v.Settings.InferyardPath), workspace, emptyText(v.Settings.ResultsRoot), emptyText(v.Settings.ModelsRoot)
}

func savedDownloadNames(v core.SettingsResponse) (modelName, engineName string) {
	return defaultDownloadName(v.Settings.ModelDownloadName, "model"), defaultDownloadName(v.Settings.EngineDownloadName, "engine")
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
	paths, names := settingsPatchGroups(patch)
	if paths {
		cliPath, workspace, results, models := savedPathFields(value)
		if s.cliPath == submitted.cliPath {
			s.cliPath = cliPath
		}
		if s.workspace == submitted.workspace {
			s.workspace = workspace
		}
		if s.results == submitted.results {
			s.results = results
		}
		if s.models == submitted.models {
			s.models = models
		}
		if emptyText(value.Effective.WorkspaceRoot) != previousWorkspace {
			s.resetWorkspace()
		}
	}
	if names {
		modelName, engineName := savedDownloadNames(value)
		if s.modelName == submitted.modelName {
			s.modelName = modelName
		}
		if s.engineName == submitted.engineName {
			s.engineName = engineName
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
func (s *viewState) settingsView(c *ui.Context) {
	s.header(c, tr(c, "nav.settings"))
	s.appearanceSettingsView(c)
	s.languageSettingsView(c)
	s.traySettingsView(c)
	if s.service != nil && s.service.BoundPaths() != nil {
		p := s.service.BoundPaths()
		card(c, tr(c, "settings.dev_paths.title"), func() {
			keyValueRow(c, tr(c, "settings.dev_paths.source"), p.Source, false)
			keyValueRow(c, "CLI", p.CLI, false)
			keyValueRow(c, tr(c, "settings.dev_paths.workspace"), p.Workspace, false)
			keyValueRow(c, tr(c, "settings.dev_paths.results"), p.Results, false)
			keyValueRow(c, tr(c, "settings.dev_paths.models"), p.Models, false)
			keyValueRow(c, tr(c, "settings.dev_paths.engines"), p.Engines, false)
		})
		s.localInstallationsView(c)
		s.downloadNamesView(c)
		s.proxySettingsView(c)
		s.updateSettingsView(c)
		return
	}
	settingsFormSection(c, settingsFormOptions{
		Title:       tr(c, "settings.paths.title"),
		Description: tr(c, "settings.paths.description"),
		Dirty:       s.pathsEdited(),
		Fields: func() {
			s.pathFields(c)
		},
		Actions: func() {
			if primaryActionButton(c, tr(c, "common.save"), s.actionBusy("save-paths")).Disabled(!s.canSavePaths()).Clicked() {
				if !s.validateSettings() {
					return
				}
				patch := map[string]json.RawMessage{}
				for _, entry := range []struct{ key, value, previous string }{{"inferyard_path", s.cliPath, emptyText(s.settings.Settings.InferyardPath)}, {"workspace_root", s.workspace, emptyText(s.settings.Effective.WorkspaceRoot)}, {"results_root", s.results, emptyText(s.settings.Settings.ResultsRoot)}, {"models_root", s.models, emptyText(s.settings.Settings.ModelsRoot)}} {
					if entry.value != entry.previous {
						var value any = entry.value
						if entry.key == "inferyard_path" && entry.value == "" {
							value = nil
						}
						patch[entry.key], _ = json.Marshal(value)
					}
				}
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				savedMsg := tr(c, "settings.paths.saved")
				s.workAs("save-paths", func() (string, *core.ApiError) { return s.persistSettings(patch, submitted, savedMsg) })
			}
		},
	})
	s.localInstallationsView(c)
	s.downloadNamesView(c)
	s.proxySettingsView(c)
	s.updateSettingsView(c)
}

func (s *viewState) pathsEdited() bool {
	return s.cliPath != emptyText(s.settings.Settings.InferyardPath) ||
		s.workspace != emptyText(s.settings.Effective.WorkspaceRoot) ||
		s.results != emptyText(s.settings.Settings.ResultsRoot) ||
		s.models != emptyText(s.settings.Settings.ModelsRoot)
}

func (s *viewState) downloadNamesEdited() bool {
	return s.modelName != defaultDownloadName(s.settings.Settings.ModelDownloadName, "model") ||
		s.engineName != defaultDownloadName(s.settings.Settings.EngineDownloadName, "engine")
}

func (s *viewState) canSavePaths() bool {
	return s.settingsLoaded && !s.busy && s.pathsEdited() && s.pathsValid()
}

func (s *viewState) pathsValid() bool {
	for _, entry := range [][3]string{
		{s.cliPath, emptyText(s.settings.Settings.InferyardPath), ""},
		{s.workspace, emptyText(s.settings.Effective.WorkspaceRoot), "workspace"},
		{s.results, emptyText(s.settings.Settings.ResultsRoot), "workspace"},
		{s.models, emptyText(s.settings.Settings.ModelsRoot), "workspace"},
	} {
		message := pathError(entry[0])
		if entry[2] != "" {
			message = settingsPathError(entry[0], entry[1])
		}
		if message != "" {
			return false
		}
	}
	return true
}

func (s *viewState) canSaveDownloadNames() bool {
	return s.settingsLoaded && !s.busy && s.downloadNamesEdited() && s.downloadNamesValid()
}

func (s *viewState) downloadNamesValid() bool {
	return validName(s.modelName) && validName(s.engineName)
}

func (s *viewState) settingsEdited() bool {
	return s.pathsEdited() || s.downloadNamesEdited() || s.proxyEdited()
}

func (s *viewState) settingsUnsaved() bool {
	if !s.settingsLoaded {
		return false
	}
	if s.service != nil && s.service.BoundPaths() != nil {
		return s.downloadNamesEdited() || s.proxyEdited()
	}
	return s.settingsEdited()
}

func (s *viewState) settingsDiscardDialog(c *ui.Context) {
	c.Key("settings-discard-dialog")
	result := components.AlertDialog(c, &s.settingsLeaveConfirm, components.AlertDialogOptions{
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
	cliUntouched := s.settingsLoaded && s.cliPath == emptyText(s.settings.Settings.InferyardPath)
	if emptyText(s.settings.Effective.WorkspaceRoot) != emptyText(value.Effective.WorkspaceRoot) {
		s.resetWorkspace()
	}
	s.settings = value
	if updateForm {
		s.loadSettings(value)
		s.settingsLoaded = true
	} else {
		if cliUntouched {
			s.cliPath = emptyText(value.Settings.InferyardPath)
		}
		s.loadThemeSettings(value)
		s.loadLanguageSettings(value)
		s.loadTraySettings(value)
	}
}

func (s *viewState) pathFields(c *ui.Context) {
	loc := contextLocale(c)
	ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
		s.pathField(c, "CLI", &s.cliPath, browseCLI).Description(cliPathStateFor(loc, s.cliPath, emptyText(s.settings.Settings.InferyardPath), s.ready.CLI)).Error(s.browseError(browseCLI.key, pathError(s.cliPath)))
		s.pathField(c, tr(c, "settings.paths.workspace_label"), &s.workspace, browseWorkspace).Description(directoryStateFor(loc, s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot))).Error(s.browseError(browseWorkspace.key, settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot))))
		s.pathField(c, tr(c, "settings.paths.results_label"), &s.results, browseResults).Description(directoryStateFor(loc, s.results, s.ready.Results, emptyText(s.settings.Settings.ResultsRoot))).Error(s.browseError(browseResults.key, settingsPathError(s.results, emptyText(s.settings.Settings.ResultsRoot))))
		s.pathField(c, tr(c, "settings.paths.models_label"), &s.models, browseModels).Description(directoryStateFor(loc, s.models, s.ready.Models, emptyText(s.settings.Settings.ModelsRoot))).Error(s.browseError(browseModels.key, settingsPathError(s.models, emptyText(s.settings.Settings.ModelsRoot))))
	})
}

func cliPathState(value, previous string, cli core.InferyardProbe) string {
	return cliPathStateFor("", value, previous, cli)
}

func cliPathStateFor(locale, value, previous string, cli core.InferyardProbe) string {
	switch {
	case value == "" && previous == "":
		if cli.Status == "outdated" {
			return cliOutdatedSummary(cli.Version)
		}
		if cli.Status == "ok" && emptyText(cli.Version) != "" {
			return l10n.Tf(locale, "settings.cli.auto_current", "version", *cli.Version)
		}
		return l10n.T(locale, "settings.cli.auto")
	case value == "":
		return l10n.T(locale, "settings.cli.cleared")
	case !samePath(value, previous):
		return l10n.T(locale, "settings.paths.modified_pending_save")
	case cli.Path != nil && !samePath(value, *cli.Path):
		return l10n.T(locale, "settings.paths.pending_check")
	}
	switch cli.Status {
	case "ok":
		if emptyText(cli.Version) != "" {
			return l10n.Tf(locale, "settings.cli.ok_version", "version", *cli.Version)
		}
		return l10n.T(locale, "settings.cli.ok")
	case "outdated":
		return cliOutdatedSummary(cli.Version)
	case "not_executable":
		return l10n.T(locale, "settings.cli.not_executable")
	case "version_failed":
		return l10n.T(locale, "settings.cli.version_failed")
	case "not_found":
		return l10n.T(locale, "settings.cli.not_found")
	default:
		return l10n.T(locale, "settings.paths.pending_check")
	}
}

func (s *viewState) downloadNamesView(c *ui.Context) {
	loc := contextLocale(c)
	settingsFormSection(c, settingsFormOptions{
		Title:       tr(c, "settings.download_names.title"),
		Description: tr(c, "settings.download_names.description"),
		Dirty:       s.downloadNamesEdited(),
		Fields: func() {
			ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
				field(c, tr(c, "settings.download_names.model_label"), &s.modelName).Error(downloadNameErrorFor(loc, s.modelName))
				field(c, tr(c, "settings.download_names.engine_label"), &s.engineName).Error(downloadNameErrorFor(loc, s.engineName))
			})
		},
		Actions: func() {
			if primaryActionButton(c, tr(c, "settings.download_names.save"), s.actionBusy("save-download-names")).Disabled(!s.canSaveDownloadNames()).Clicked() {
				patch := map[string]json.RawMessage{}
				patch["model_download_name"], _ = json.Marshal(s.modelName)
				patch["engine_download_name"], _ = json.Marshal(s.engineName)
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				savedMsg := tr(c, "settings.download_names.saved")
				s.workAs("save-download-names", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, savedMsg)
				})
			}
			button(c, tr(c, "settings.download_names.reset")).Disabled(s.busy).OnClick(func() {
				prevModel, prevEngine := s.modelName, s.engineName
				if prevModel == "model" && prevEngine == "engine" {
					return
				}
				s.modelName, s.engineName = "model", "engine"
				s.enqueueActionToast("undo-download-names", tr(c, "settings.download_names.restored_default"), tr(c, "common.undo"), tr(c, "settings.download_names.restored_previous"), 6*time.Second, func() {
					s.modelName, s.engineName = prevModel, prevEngine
				})
			})
		},
	})
}

func downloadNameError(name string) string {
	return downloadNameErrorFor("", name)
}

func downloadNameErrorFor(locale, name string) string {
	if !validName(name) {
		return l10n.T(locale, "settings.download_names.invalid")
	}
	return ""
}

func defaultDownloadName(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
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
	state := l10n.T(locale, "settings.paths.modified_pending_save")
	if samePath(value, previous) {
		if readiness.Path == nil || !samePath(value, *readiness.Path) {
			return l10n.T(locale, "settings.paths.pending_check")
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
