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
}
func (s *viewState) settingsView(c *ui.Context) {
	s.header(c, "设置")
	card(c, "路径设置", func() {
		fields := func() {
			field(c, "CLI 绝对路径（留空使用 PATH）", &s.cliPath).Description("可留空，使用 PATH 中的 inferyard。").Error(pathError(s.cliPath))
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
		if primaryButton(c, "保存设置").AlignSelf(ui.Start).Disabled(s.busy || !s.settingsLoaded).Clicked() {
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
	card(c, "生效值与探测状态", func() {
		ui.Text(c, "当前生效工作区："+text(s.settings.Effective.WorkspaceRoot)).Selectable()
		componentFold(c, "原始生效值", func() { kvview(c, "kv-effective", s.settings.Effective) })
	})
}

func (s *viewState) settingsEdited() bool {
	return s.cliPath != emptyText(s.settings.Settings.InferyardPath) ||
		s.workspace != emptyText(s.settings.Effective.WorkspaceRoot) ||
		s.results != emptyText(s.settings.Settings.ResultsRoot) ||
		s.models != emptyText(s.settings.Settings.ModelsRoot)
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
