package pages

import (
	"encoding/json"
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
	card(c, "路径设置", func() {
		fields := func() {
			field(c, "CLI", &s.cliPath).Description("空值使用发现到的可执行文件，顺序是 PATH、~/.local/bin，macOS 还有 /opt/homebrew/bin 和 /usr/local/bin。").Error(pathError(s.cliPath))
			field(c, "工作区目录", &s.workspace).Description("绝对路径；保存不创建目录。").Error(settingsPathError(s.workspace, emptyText(s.settings.Effective.WorkspaceRoot)))
			field(c, "结果目录", &s.results).Description("绝对路径；保存不创建目录。").Error(settingsPathError(s.results, emptyText(s.settings.Settings.ResultsRoot)))
			field(c, "模型目录", &s.models).Description("绝对路径；保存不创建目录。").Error(settingsPathError(s.models, emptyText(s.settings.Settings.ModelsRoot)))
		}
		if compactLayout(c) {
			fields()
		} else {
			ui.Form(c, fields)
		}
		ui.Text(c, "保存设置不创建目录。目录缺失仍可浏览所有页面。")
		if primaryButton(c, "保存").AlignSelf(ui.Start).Disabled(s.busy || !s.settingsLoaded).Clicked() {
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
			s.work(func() (string, *core.ApiError) {
				v, e := s.service.UpdateSettings(patch)
				if e == nil {
					s.win.Update(func() { s.loadSettings(v) })
				}
				return "设置已保存。", e
			})
		}
	})
	s.downloadNamesView(c)
}

func (s *viewState) settingsEdited() bool {
	return s.cliPath != emptyText(s.settings.Settings.InferyardPath) ||
		s.workspace != emptyText(s.settings.Effective.WorkspaceRoot) ||
		s.results != emptyText(s.settings.Settings.ResultsRoot) ||
		s.models != emptyText(s.settings.Settings.ModelsRoot) ||
		s.modelName != defaultDownloadName(s.settings.Settings.ModelDownloadName, "model") || s.engineName != defaultDownloadName(s.settings.Settings.EngineDownloadName, "engine")
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

func (s *viewState) downloadNamesView(c *ui.Context) {
	card(c, "下载目录名", func() {
		ui.Text(c, "新下载任务自动使用基础名，无需每次填写。")
		field(c, "模型下载目录名", &s.modelName).Error(downloadNameError(s.modelName))
		field(c, "引擎下载目录名", &s.engineName).Error(downloadNameError(s.engineName))
		ui.Text(c, "仅允许字母、数字、点、下划线、连字符；不能为空，也不能全为点。默认 model / engine。")
		ui.Text(c, "名称被占用时自动追加 -2、-3；实际名称显示在下载任务详情中。")
		button(c, "保存下载目录名").Disabled(s.busy || !validName(s.modelName) || !validName(s.engineName)).OnClick(func() {
			patch := map[string]json.RawMessage{}
			patch["model_download_name"], _ = json.Marshal(s.modelName)
			patch["engine_download_name"], _ = json.Marshal(s.engineName)
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.UpdateSettings(patch)
				return "下载目录名已保存。", e
			})
		})
		button(c, "恢复默认值").OnClick(func() { s.modelName, s.engineName = "model", "engine" })
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
