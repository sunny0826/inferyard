package pages

import (
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"strings"
)

func (s *viewState) readinessView(c *ui.Context) {
	card(c, "环境准备", func() {
		cli := "请安装 CLI 或在设置中配置路径"
		if s.ready.CLI.Status == "ok" {
			cli = "已就绪 · " + text(s.ready.CLI.Version)
		}
		ui.Text(c, "CLI："+cli)
		for _, entry := range []struct {
			label string
			dir   core.DirectoryReadiness
		}{{"工作区", s.ready.Workspace}, {"结果目录", s.ready.Results}, {"模型目录", s.ready.Models}} {
			state := "待配置"
			if entry.dir.Exists {
				state = "目录可用"
			} else if entry.dir.Configured {
				state = "待创建"
			}
			if entry.label == "工作区" && entry.dir.Exists {
				state = "待初始化"
				if entry.dir.Initialized {
					state = "已初始化"
				}
			}
			ui.Text(c, entry.label+"："+text(entry.dir.Path)+" · "+state)
		}
		ui.Row(c).Gap(10).Children(func() {
			if ui.Button(c, "准备目录").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.provision()
			}
			if ui.Button(c, "修改设置").AlignSelf(ui.Start).Clicked() {
				s.selected = "settings"
			}
		})

	})
}
func (s *viewState) loadSettings(v core.SettingsResponse) {
	s.cliPath = emptyText(v.Settings.InferyardPath)
	s.workspace = emptyText(v.Settings.WorkspaceRoot)
	if s.workspace == "" {
		s.workspace = emptyText(v.Effective.WorkspaceRoot)
	}
	s.results = emptyText(v.Settings.ResultsRoot)
	s.models = emptyText(v.Settings.ModelsRoot)
}
func (s *viewState) provision() {
	s.guideWork(func() (string, *core.ApiError) {
		v, e := s.service.Provision()
		if e == nil {
			s.win.Update(func() { s.loadSettings(v) })
		}
		return "目录已准备，请在设置页初始化工作区。", e
	})
}
func (s *viewState) settingsView(c *ui.Context) {
	s.header(c, "设置")
	card(c, "路径设置", func() {
		ui.Form(c, func() {
			field(c, "CLI 绝对路径（留空使用 PATH）", &s.cliPath)
			field(c, "工作区目录", &s.workspace)
			field(c, "结果目录", &s.results)
			field(c, "模型目录", &s.models)
		})
		ui.Text(c, "保存设置不创建目录。目录缺失仍可浏览所有页面。")
		if ui.PrimaryButton(c, "保存设置").AlignSelf(ui.Start).Disabled(s.busy || !s.settingsLoaded).Clicked() {
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
	s.setupGuide(c)
	s.readinessView(c)
	card(c, "生效值与探测状态", func() { kvview(c, "kv-effective", s.settings.Effective) })
}
func (s *viewState) deviceView(c *ui.Context) {
	s.header(c, "设备")
	if s.progress != nil && s.progress.NextAction == "device_check" {
		card(c, "检查设备", func() {
			ui.Text(c, "模型与引擎可在「模型与下载」页准备。")
			if ui.PrimaryButton(c, "检查设备").AlignSelf(ui.Start).Disabled(s.busy).Clicked() {
				s.execute(s.nextCommand(), false)
			}
		})
	}
	if ui.PrimaryButton(c, "刷新设备").AlignSelf(ui.Start).Disabled(s.busy || s.device.Refreshing).Clicked() {
		s.work(func() (string, *core.ApiError) { return "正在采集设备信息…", s.service.RefreshDevice() })
	}
	errorView(c, s.device.LastError)
	ui.Text(c, "设备快照 · "+text(s.device.CapturedAt))
	if s.device.Refreshing {
		ui.Text(c, "正在采集设备信息…")
	}
	var snapshot map[string]json.RawMessage
	if len(s.device.Snapshot) == 0 || strings.TrimSpace(string(s.device.Snapshot)) == "null" {
		card(c, "尚无设备快照", func() { ui.Text(c, "请先在设置中配置模型目录，然后刷新设备。") })
		return
	}
	if e := json.Unmarshal(s.device.Snapshot, &snapshot); e != nil {
		ui.Text(c, "设备快照无法读取。").TextColor(c.Theme().Danger)
		return
	}
	for _, section := range []struct{ key, title string }{{"hardware", "硬件、GPU、缺测与来源"}, {"recommendation", "推荐配置"}, {"models", "本地 GGUF 模型"}, {"platform_capabilities", "平台能力"}, {"next_steps", "下一步"}} {
		card(c, section.title, func() { kvview(c, "kv-"+section.key, snapshot[section.key]) })
	}
	ui.Text(c, fmt.Sprintf("可运行：%s · 模型请求数：%s", rawValue(snapshot["ready_to_run"]), rawValue(snapshot["model_requests_sent"])))
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

func rawValue(value json.RawMessage) string {
	text := strings.TrimSpace(string(value))
	if text == "" || text == "null" {
		return "缺测"
	}
	return text
}
