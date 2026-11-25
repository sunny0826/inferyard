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

type aboutInfoRow struct {
	label string
	value string
}

func aboutFieldValue(locale, value string) string {
	if value == "" {
		return l10n.T(locale, "settings.about.unknown")
	}
	return value
}

func aboutInfoRows(locale string, info core.BuildInfo) []aboutInfoRow {
	return []aboutInfoRow{
		{label: l10n.T(locale, "settings.about.version"), value: aboutFieldValue(locale, formatAboutVersion(info.Version))},
		{label: l10n.T(locale, "settings.about.build_time"), value: aboutFieldValue(locale, formatAboutBuildTime(info.BuildTime))},
		{label: l10n.T(locale, "settings.about.git_commit"), value: aboutFieldValue(locale, formatAboutCommit(info.GitCommit, info.Dirty))},
	}
}

func formatAboutCopyText(locale string, info core.BuildInfo) string {
	rows := aboutInfoRows(locale, info)
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = fmt.Sprintf("%s: %s", row.label, row.value)
	}
	return strings.Join(lines, "\n")
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
	rows := aboutInfoRows(contextLocale(c), info)
	t := tokens(c)
	card(c, tr(c, "settings.about.title"), func() {
		for _, row := range rows {
			keyValueRow(c, row.label, row.value, true)
		}
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Tight, 0, 0, 0).Children(func() {
			if outlineButton(c, tr(c, "settings.about.copy")).Clicked() {
				s.copyAboutInfo(c, info)
			}
		})
	})
}
