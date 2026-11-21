package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) loadProxySettings(v core.SettingsResponse) {
	s.downloadProxy = strings.TrimSpace(emptyText(v.Settings.DownloadProxy))
}

func (s *viewState) proxyEdited() bool {
	return strings.TrimSpace(s.downloadProxy) != strings.TrimSpace(emptyText(s.settings.Settings.DownloadProxy))
}

func (s *viewState) proxyValid() bool {
	return downloadProxyError(s.downloadProxy) == ""
}

func (s *viewState) canSaveProxy() bool {
	return s.settingsLoaded && !s.busy && s.proxyEdited() && s.proxyValid()
}

func downloadProxyError(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !core.ValidateDownloadProxy(trimmed) {
		return "请输入形如 http://127.0.0.1:7890 的代理源站（不含路径、查询或凭据）。"
	}
	return ""
}

func (s *viewState) syncSavedProxy(patch map[string]json.RawMessage, submitted settingDraft, value core.SettingsResponse) {
	if _, ok := patch["download_proxy"]; !ok {
		return
	}
	if strings.TrimSpace(s.downloadProxy) == strings.TrimSpace(submitted.downloadProxy) {
		s.loadProxySettings(value)
	}
}

func (s *viewState) proxySettingsView(c *ui.Context) {
	settingsFormSection(c, settingsFormOptions{
		Title:       "网络与代理",
		Description: "可选；仅显式用于模型解析与资源下载请求，跨源重定向时不会透传凭据。",
		Dirty:       s.proxyEdited(),
		Fields: func() {
			field(c, "下载代理地址（可选）", &s.downloadProxy).
				Placeholder("http://127.0.0.1:7890").
				Error(downloadProxyError(s.downloadProxy))
			settingsNotice(c, s.proxyEdited(), "代理设置已修改，点击保存后生效。")
		},
		Actions: func() {
			if primaryActionButton(c, "保存代理设置", s.actionBusy("save-proxy")).Disabled(!s.canSaveProxy()).Clicked() {
				trimmed := strings.TrimSpace(s.downloadProxy)
				var value any
				if trimmed != "" {
					value = strings.TrimRight(trimmed, "/")
				}
				patch := map[string]json.RawMessage{}
				patch["download_proxy"], _ = json.Marshal(value)
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				s.workAs("save-proxy", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, "代理设置已保存")
				})
			}
			button(c, "清空代理").Disabled(s.busy || (strings.TrimSpace(s.downloadProxy) == "" && s.settings.Settings.DownloadProxy == nil)).OnClick(func() {
				s.downloadProxy = ""
				if s.settings.Settings.DownloadProxy == nil {
					return
				}
				patch := map[string]json.RawMessage{"download_proxy": json.RawMessage("null")}
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				s.workAs("save-proxy", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, "已清空下载代理")
				})
			})
		},
	})
}
