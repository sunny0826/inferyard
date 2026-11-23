package pages

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) loadSettings(v core.SettingsResponse) {
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
	s.header(c, "设置")
	if s.service != nil && s.service.BoundPaths() != nil {
		p := s.service.BoundPaths()
		card(c, "开发环境 · 固定路径", func() {
			keyValueRow(c, "来源", p.Source, false)
			keyValueRow(c, "CLI", p.CLI, false)
			keyValueRow(c, "工作区", p.Workspace, false)
			keyValueRow(c, "结果", p.Results, false)
			keyValueRow(c, "模型", p.Models, false)
			keyValueRow(c, "引擎", p.Engines, false)
		})
		s.localInstallationsView(c)
		s.downloadNamesView(c)
		s.proxySettingsView(c)
		return
	}
	settingsFormSection(c, settingsFormOptions{
		Title:       "路径设置",
		Description: "新建工作区请手动输入完整路径；保存后在任务页点击「准备环境」创建。",
		Dirty:       s.pathsEdited(),
		Fields: func() {
			s.pathFields(c)
		},
		Actions: func() {
			if primaryActionButton(c, "保存", s.actionBusy("save-paths")).Disabled(!s.canSavePaths()).Clicked() {
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
				s.workAs("save-paths", func() (string, *core.ApiError) { return s.persistSettings(patch, submitted, "设置已保存") })
			}
		},
	})
	s.localInstallationsView(c)
	s.downloadNamesView(c)
	s.proxySettingsView(c)
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
		DialogOptions: components.DialogOptions{Label: "放弃未保存的设置"},
		Title:         "放弃未保存的设置？",
		Message:       "路径或下载目录名有尚未保存的修改，离开后将恢复为已保存的设置。",
		ConfirmLabel:  "放弃修改并离开",
		CancelLabel:   "留在设置页",
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
	}
}

func (s *viewState) pathFields(c *ui.Context) {
	ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
		s.pathField(c, "CLI", &s.cliPath, browseCLI).Description("留空自动查找本地 CLI").Error(s.browseError(browseCLI.key, pathError(s.cliPath)))
		s.pathField(c, "工作区目录", &s.workspace, browseWorkspace).Description(directoryState(s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot))).Error(s.browseError(browseWorkspace.key, settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot))))
		s.pathField(c, "结果目录", &s.results, browseResults).Description(directoryState(s.results, s.ready.Results, emptyText(s.settings.Settings.ResultsRoot))).Error(s.browseError(browseResults.key, settingsPathError(s.results, emptyText(s.settings.Settings.ResultsRoot))))
		s.pathField(c, "模型目录", &s.models, browseModels).Description(directoryState(s.models, s.ready.Models, emptyText(s.settings.Settings.ModelsRoot))).Error(s.browseError(browseModels.key, settingsPathError(s.models, emptyText(s.settings.Settings.ModelsRoot))))
	})
}

func (s *viewState) downloadNamesView(c *ui.Context) {
	settingsFormSection(c, settingsFormOptions{
		Title:       "下载目录名",
		Description: "仅限字母、数字、.、_、-，默认 model / engine",
		Dirty:       s.downloadNamesEdited(),
		Fields: func() {
			ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
				field(c, "模型下载目录名", &s.modelName).Error(downloadNameError(s.modelName))
				field(c, "引擎下载目录名", &s.engineName).Error(downloadNameError(s.engineName))
			})
		},
		Actions: func() {
			if primaryActionButton(c, "保存下载目录名", s.actionBusy("save-download-names")).Disabled(!s.canSaveDownloadNames()).Clicked() {
				patch := map[string]json.RawMessage{}
				patch["model_download_name"], _ = json.Marshal(s.modelName)
				patch["engine_download_name"], _ = json.Marshal(s.engineName)
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				s.workAs("save-download-names", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, "下载目录名已保存")
				})
			}
			button(c, "恢复默认值").Disabled(s.busy).OnClick(func() {
				prevModel, prevEngine := s.modelName, s.engineName
				if prevModel == "model" && prevEngine == "engine" {
					return
				}
				s.modelName, s.engineName = "model", "engine"
				s.enqueueActionToast("undo-download-names", "已恢复默认目录名", "撤销", "已恢复原目录名", 6*time.Second, func() {
					s.modelName, s.engineName = prevModel, prevEngine
				})
			})
		},
	})
}

func downloadNameError(name string) string {
	if !validName(name) {
		return "请输入有效目录名，不能为空或全为点。"
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
	switch {
	case value == "" && previous == "":
		return "尚未配置，保存时使用默认值"
	case value == "":
		return "当前为空；已配置的目录不能清空"
	}
	state := "路径已修改，保存后生效"
	if samePath(value, previous) {
		if readiness.Path == nil || !samePath(value, *readiness.Path) {
			return "等待检查"
		}
		state = "目录不存在"
		if readiness.Exists {
			state = "目录已存在"
			if readiness.Initialized {
				state = "目录已存在，工作区已初始化"
			}
		}
	}
	return state
}

func samePath(value, previous string) bool {
	return previous != "" && filepath.Clean(value) == filepath.Clean(previous)
}
