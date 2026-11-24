package pages

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
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
	return downloadProxyErrorFor("", raw)
}

func downloadProxyErrorFor(locale, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !core.ValidateDownloadProxy(trimmed) {
		return l10n.T(locale, "settings.proxy.invalid")
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
	loc := contextLocale(c)
	settingsFormSection(c, settingsFormOptions{
		Title:       tr(c, "settings.proxy.title"),
		Description: tr(c, "settings.proxy.description"),
		Dirty:       s.proxyEdited(),
		Fields: func() {
			field(c, tr(c, "settings.proxy.url_label"), &s.downloadProxy).
				Placeholder("http://127.0.0.1:7890").
				Error(downloadProxyErrorFor(loc, s.downloadProxy))
		},
		Actions: func() {
			if primaryActionButton(c, tr(c, "settings.proxy.save"), s.actionBusy("save-proxy")).Disabled(!s.canSaveProxy()).Clicked() {
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
				savedMsg := tr(c, "settings.proxy.saved")
				s.workAs("save-proxy", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, savedMsg)
				})
			}
			button(c, tr(c, "settings.proxy.clear")).Disabled(s.busy || (strings.TrimSpace(s.downloadProxy) == "" && s.settings.Settings.DownloadProxy == nil)).OnClick(func() {
				s.downloadProxy = ""
				if s.settings.Settings.DownloadProxy == nil {
					return
				}
				patch := map[string]json.RawMessage{"download_proxy": json.RawMessage("null")}
				if s.simulate(previewSettings{patch: patch}) {
					return
				}
				submitted := s.settingsDraft()
				clearedMsg := tr(c, "settings.proxy.cleared")
				s.workAs("save-proxy", func() (string, *core.ApiError) {
					return s.persistSettings(patch, submitted, clearedMsg)
				})
			})
		},
	})
}
