package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) aboutBuildInfo() core.BuildInfo {
	if s.buildInfo != nil {
		return s.buildInfo()
	}
	return core.CurrentBuildInfo()
}

func formatAboutVersion(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if strings.EqualFold(v, "dev") {
		return "dev"
	}
	return "v" + strings.TrimPrefix(v, "v")
}

func formatAboutBuildTime(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, v); err == nil {
			return parsed.UTC().Format("2006-01-02 15:04:05 UTC")
		}
	}
	return v
}

func formatAboutCommit(commit string, dirty bool) string {
	v := strings.TrimSpace(commit)
	if v == "" {
		return ""
	}
	if dirty {
		return v + "-dirty"
	}
	return v
}

func aboutFieldValue(locale, value string) string {
	if value == "" {
		return l10n.T(locale, "settings.about.unknown")
	}
	return value
}

func formatAboutCopyText(locale string, info core.BuildInfo) string {
	return strings.Join([]string{
		fmt.Sprintf("%s: %s", l10n.T(locale, "settings.about.version"), aboutFieldValue(locale, formatAboutVersion(info.Version))),
		fmt.Sprintf("%s: %s", l10n.T(locale, "settings.about.build_time"), aboutFieldValue(locale, formatAboutBuildTime(info.BuildTime))),
		fmt.Sprintf("%s: %s", l10n.T(locale, "settings.about.git_commit"), aboutFieldValue(locale, formatAboutCommit(info.GitCommit, info.Dirty))),
	}, "\n")
}

func (s *viewState) copyAboutInfo(c *ui.Context, info core.BuildInfo) {
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "settings.about.toast.copied_preview"))
		return
	}
	text := formatAboutCopyText(contextLocale(c), info)
	c.WriteClipboardThen(text, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("settings.about.toast.copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("settings.about.toast.copied"))
	})
}

func (s *viewState) aboutSettingsView(c *ui.Context) {
	info := s.aboutBuildInfo()
	loc := contextLocale(c)
	t := tokens(c)
	card(c, tr(c, "settings.about.title"), func() {
		keyValueRow(c, tr(c, "settings.about.version"), aboutFieldValue(loc, formatAboutVersion(info.Version)), true)
		keyValueRow(c, tr(c, "settings.about.build_time"), aboutFieldValue(loc, formatAboutBuildTime(info.BuildTime)), true)
		keyValueRow(c, tr(c, "settings.about.git_commit"), aboutFieldValue(loc, formatAboutCommit(info.GitCommit, info.Dirty)), true)
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Tight, 0, 0, 0).Children(func() {
			if outlineButton(c, tr(c, "settings.about.copy")).Clicked() {
				s.copyAboutInfo(c, info)
			}
		})
	})
}
