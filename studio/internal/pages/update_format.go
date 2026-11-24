package pages

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

var releaseHTMLCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

func updateBadgePresentation(st core.UpdateState) (label string, kind stateKind, icon func() *ui.SVG, animated bool) {
	return updateBadgePresentationFor("", st)
}

func updateBadgePresentationFor(locale string, st core.UpdateState) (label string, kind stateKind, icon func() *ui.SVG, animated bool) {
	switch st.State {
	case core.UpdateAvailable:
		ver := ""
		if st.Release != nil {
			ver = st.Release.Version
		}
		if ver == "" {
			return l10n.T(locale, "update.badge.available"), stateActive, lucide.ArrowUp, false
		}
		return l10n.Tf(locale, "update.badge.available_version", "version", ver), stateActive, lucide.ArrowUp, false
	case core.UpdateDownloading:
		if st.TotalBytes != nil && *st.TotalBytes > 0 && st.DownloadedBytes >= 0 {
			pct := float64(st.DownloadedBytes) * 100 / float64(*st.TotalBytes)
			if pct > 100 {
				pct = 100
			}
			return l10n.Tf(locale, "update.badge.downloading_pct", "pct", fmt.Sprintf("%.0f", pct)), stateActive, lucide.Download, true
		}
		return l10n.T(locale, "update.badge.downloading"), stateActive, lucide.Download, true
	case core.UpdateVerifying:
		return l10n.T(locale, "update.badge.verifying"), stateActive, lucide.RefreshCw, true
	case core.UpdateReadyToApply:
		ver := ""
		if st.Release != nil {
			ver = st.Release.Version
		}
		if ver == "" {
			return l10n.T(locale, "update.badge.ready"), stateSuccess, lucide.Download, false
		}
		return l10n.Tf(locale, "update.badge.ready_version", "version", ver), stateSuccess, lucide.Download, false
	case core.UpdateApplying:
		return l10n.T(locale, "update.badge.applying"), stateActive, lucide.RefreshCw, true
	case core.UpdateError:
		if st.Release != nil {
			return l10n.T(locale, "update.badge.error"), stateDanger, lucide.CircleAlert, false
		}
		return "", stateNeutral, lucide.Info, false
	default:
		return "", stateNeutral, lucide.Info, false
	}
}

func updateSettingsStatus(st core.UpdateState) (string, stateKind) {
	return updateSettingsStatusFor("", st)
}

func updateSettingsStatusFor(locale string, st core.UpdateState) (string, stateKind) {
	switch st.State {
	case core.UpdateChecking:
		if st.ManualCheck {
			return l10n.T(locale, "update.status.checking"), stateActive
		}
		return "", stateNeutral
	case core.UpdateUpToDate:
		return l10n.T(locale, "update.status.up_to_date"), stateSuccess
	case core.UpdateAvailable:
		if st.Release != nil {
			return l10n.Tf(locale, "update.status.available_version", "version", st.Release.Version), stateActive
		}
		return l10n.T(locale, "update.badge.available"), stateActive
	case core.UpdateDownloading:
		return l10n.Tf(locale, "update.status.downloading", "progress", formatUpdateProgress(st)), stateActive
	case core.UpdateVerifying:
		return l10n.T(locale, "update.status.verifying"), stateActive
	case core.UpdateReadyToApply:
		if st.Release != nil {
			return l10n.Tf(locale, "update.status.ready_version", "version", st.Release.Version), stateSuccess
		}
		return l10n.T(locale, "update.badge.ready"), stateSuccess
	case core.UpdateApplying:
		return l10n.T(locale, "update.status.applying"), stateActive
	case core.UpdateError:
		if st.ErrorMessage != "" {
			return st.ErrorMessage, stateDanger
		}
		return l10n.T(locale, "update.status.check_failed"), stateDanger
	default:
		return "", stateNeutral
	}
}

func isUpdateCheckError(code string) bool {
	switch code {
	case "update_check_failed",
		"update_network_error",
		"update_timeout",
		"update_rate_limited",
		"update_parse_failed",
		"update_invalid_proxy",
		"update_invalid_url",
		"update_version_invalid":
		return true
	default:
		return false
	}
}

func formatUpdateProgress(st core.UpdateState) string {
	done := formatBytes(&st.DownloadedBytes)
	if st.TotalBytes != nil && *st.TotalBytes > 0 {
		done += " / " + formatBytes(st.TotalBytes)
	}
	if st.BytesPerSecond != nil && *st.BytesPerSecond > 0 {
		bps := int64(*st.BytesPerSecond)
		done += " · " + formatBytes(&bps) + "/s"
	}
	return done
}

func formatUpdateWorkloadWarning(w core.UpdateActiveWorkloads) string {
	return formatUpdateWorkloadWarningFor("", w)
}

func formatUpdateWorkloadWarningFor(locale string, w core.UpdateActiveWorkloads) string {
	switch {
	case w.ActiveRuns > 0 && w.ActiveDownloads > 0:
		return l10n.Tf(locale, "update.workload.both", "runs", w.ActiveRuns, "downloads", w.ActiveDownloads)
	case w.ActiveRuns > 0:
		return l10n.Tf(locale, "update.workload.runs_only", "runs", w.ActiveRuns)
	case w.ActiveDownloads > 0:
		return l10n.Tf(locale, "update.workload.downloads_only", "downloads", w.ActiveDownloads)
	default:
		return ""
	}
}

func summarizeReleaseNotes(notes string) string {
	return summarizeReleaseNotesFor("", notes)
}

func summarizeReleaseNotesFor(locale, notes string) string {
	cleaned := releaseHTMLCommentRe.ReplaceAllString(notes, "")
	lines := strings.Split(strings.ReplaceAll(cleaned, "\r\n", "\n"), "\n")
	out := make([]string, 0, 12)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
		if len(out) >= 12 {
			break
		}
	}
	if len(out) == 0 {
		return l10n.T(locale, "update.notes.empty")
	}
	summary := strings.Join(out, "\n")
	if len(lines) > len(out) {
		summary += "\n…"
	}
	const maxRunes = 600
	if utf8.RuneCountInString(summary) > maxRunes {
		runes := []rune(summary)
		return string(runes[:maxRunes-1]) + "…"
	}
	return summary
}
