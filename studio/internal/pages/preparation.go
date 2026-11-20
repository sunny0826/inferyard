package pages

import (
	"encoding/json"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) loadSettings(v core.SettingsResponse) {
	s.cliPath = emptyText(v.Settings.InferyardPath)
	s.workspace = emptyText(v.Settings.WorkspaceRoot)
	if s.workspace == "" {
		s.workspace = emptyText(v.Effective.WorkspaceRoot)
	}
	s.results = emptyText(v.Settings.ResultsRoot)
	s.models = emptyText(v.Settings.ModelsRoot)
	s.modelName, s.engineName = v.Settings.ModelDownloadName, v.Settings.EngineDownloadName
	if s.modelName == "" {
		s.modelName = "model"
	}
	if s.engineName == "" {
		s.engineName = "engine"
	}
}
func (s *viewState) settingsView(c *ui.Context) {
	s.header(c, "设置")
	if s.service != nil && s.service.BoundPaths() != nil {
		p := s.service.BoundPaths()
		card(c, "开发环境 · 固定路径", func() {
			for _, line := range []string{"来源：" + p.Source, "CLI：" + p.CLI, "工作区：" + p.Workspace, "结果：" + p.Results, "模型：" + p.Models, "引擎：" + p.Engines} {
				ui.Text(c, line)
			}
		})
		s.localInstallationsView(c)
		s.downloadNamesView(c)
		return
	}
	card(c, "路径设置", func() {
		// All Settings cards stack their fields the same way: label above the
		// control. The form alignment stays identical at any window width and
		// text scale, instead of switching between inline and stacked layouts.
		s.pathFields(c)
		ui.Text(c, "保存不会自动创建目录；新建工作区请手动输入完整路径。").TextColor(c.Theme().TextMuted)
		settingsNotice(c, s.pathsEdited(), "路径有未保存的修改，点击保存后生效。")
		if primaryButton(c, "保存").Disabled(!s.canSavePaths()).Clicked() {
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
			s.work(func() (string, *core.ApiError) {
				v, e := s.service.UpdateSettings(patch)
				if e == nil {
					s.win.Update(func() { s.loadSettings(v) })
				}
				return "设置已保存。", e
			})
		}
	})
	s.localInstallationsView(c)
	s.downloadNamesView(c)
}

// pathsEdited reports whether the path form differs from the saved settings,
// so the path card only offers a save when there is something to write.
func (s *viewState) pathsEdited() bool {
	return s.cliPath != emptyText(s.settings.Settings.InferyardPath) ||
		s.workspace != emptyText(s.settings.Effective.WorkspaceRoot) ||
		s.results != emptyText(s.settings.Settings.ResultsRoot) ||
		s.models != emptyText(s.settings.Settings.ModelsRoot)
}

// downloadNamesEdited reports whether the download names differ from the
// saved values. An unset saved name means the default, so the defaults alone
// are not an edit.
func (s *viewState) downloadNamesEdited() bool {
	return s.modelName != defaultDownloadName(s.settings.Settings.ModelDownloadName, "model") ||
		s.engineName != defaultDownloadName(s.settings.Settings.EngineDownloadName, "engine")
}

func (s *viewState) canSavePaths() bool {
	return s.settingsLoaded && !s.busy && s.pathsEdited()
}

func (s *viewState) canSaveDownloadNames() bool {
	return s.settingsLoaded && !s.busy && s.downloadNamesEdited() && s.downloadNamesValid()
}

func (s *viewState) downloadNamesValid() bool {
	return validName(s.modelName) && validName(s.engineName)
}

func (s *viewState) settingsEdited() bool {
	return s.pathsEdited() || s.downloadNamesEdited()
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

// settingsNotice explains that edits are only in the form, not yet persisted.
// It renders nothing while the fields match the saved values.
func settingsNotice(c *ui.Context, dirty bool, message string) {
	if !dirty {
		return
	}
	ui.Text(c, message).Role(ui.RoleStatus).TextColor(stateColor(c, stateWarning))
}

// pathFields renders the four path inputs with their browse buttons.
func (s *viewState) pathFields(c *ui.Context) {
	ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
		s.pathField(c, "CLI", &s.cliPath, browseCLI).Description("留空则自动从 PATH 及常用本地目录查找。").Error(s.browseError(browseCLI.key, pathError(s.cliPath)))
		s.pathField(c, "工作区目录", &s.workspace, browseWorkspace).Description(directoryState(s.workspace, s.ready.Workspace, emptyText(s.settings.Effective.WorkspaceRoot))).Error(s.browseError(browseWorkspace.key, settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot))))
		s.pathField(c, "结果目录", &s.results, browseResults).Description(directoryState(s.results, s.ready.Results, emptyText(s.settings.Settings.ResultsRoot))).Error(s.browseError(browseResults.key, settingsPathError(s.results, emptyText(s.settings.Settings.ResultsRoot))))
		s.pathField(c, "模型目录", &s.models, browseModels).Description(directoryState(s.models, s.ready.Models, emptyText(s.settings.Settings.ModelsRoot))).Error(s.browseError(browseModels.key, settingsPathError(s.models, emptyText(s.settings.Settings.ModelsRoot))))
	})
}

func (s *viewState) downloadNamesView(c *ui.Context) {
	card(c, "下载目录名", func() {
		ui.Column(c).Gap(tokens(c).Density.Row).Children(func() {
			field(c, "模型下载目录名", &s.modelName).Error(downloadNameError(s.modelName))
			field(c, "引擎下载目录名", &s.engineName).Error(downloadNameError(s.engineName))
		})
		ui.Text(c, "仅允许字母、数字、.、_、-；重名时自动追加序号（默认 model / engine）。").TextColor(c.Theme().TextMuted)
		settingsNotice(c, s.downloadNamesEdited(), downloadNamesNotice(s.modelName, s.engineName))
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
			if primaryButton(c, "保存下载目录名").Disabled(!s.canSaveDownloadNames()).Clicked() {
				patch := map[string]json.RawMessage{}
				patch["model_download_name"], _ = json.Marshal(s.modelName)
				patch["engine_download_name"], _ = json.Marshal(s.engineName)
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				s.work(func() (string, *core.ApiError) {
					_, e := s.service.UpdateSettings(patch)
					return "下载目录名已保存。", e
				})
			}
			button(c, "恢复默认值").Disabled(s.busy).OnClick(func() { s.modelName, s.engineName = "model", "engine" })
		})
	})
}

// downloadNamesNotice states that reverting or editing the names only filled
// the form: the defaults are not persisted until the save button runs.
func downloadNamesNotice(modelName, engineName string) string {
	if modelName == "model" && engineName == "engine" {
		return "已填入默认值，请点击保存生效。"
	}
	return "下载目录名已修改，请点击保存生效。"
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

// directoryState describes what the settings field currently points at, so the
// user does not have to save and reopen to learn that a path is missing.
func directoryState(value string, readiness core.DirectoryReadiness, previous string) string {
	switch {
	case value == "" && previous == "":
		return "尚未配置，保存时使用默认值"
	case value == "":
		return "当前为空；已配置的目录不能清空"
	}
	state := "路径已修改，保存后生效"
	if samePath(value, previous) {
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
