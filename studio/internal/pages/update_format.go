package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

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
			return l10n.T(locale, "update.badge.available"), stateActive, reicon.ArrowUp, false
		}
		return l10n.Tf(locale, "update.badge.available_version", "version", ver), stateActive, reicon.ArrowUp, false
	case core.UpdateDownloading:
		if st.TotalBytes != nil && *st.TotalBytes > 0 && st.DownloadedBytes >= 0 {
			pct := float64(st.DownloadedBytes) * 100 / float64(*st.TotalBytes)
			if pct > 100 {
				pct = 100
			}
			return l10n.Tf(locale, "update.badge.downloading_pct", "pct", fmt.Sprintf("%.0f", pct)), stateActive, reicon.Download, true
		}
		return l10n.T(locale, "update.badge.downloading"), stateActive, reicon.Download, true
	case core.UpdateVerifying:
		return l10n.T(locale, "update.badge.verifying"), stateActive, reicon.RefreshCw, true
	case core.UpdateReadyToApply:
		ver := ""
		if st.Release != nil {
			ver = st.Release.Version
		}
		if ver == "" {
			return l10n.T(locale, "update.badge.ready"), stateSuccess, reicon.Download, false
		}
		return l10n.Tf(locale, "update.badge.ready_version", "version", ver), stateSuccess, reicon.Download, false
	case core.UpdateApplying:
		return l10n.T(locale, "update.badge.applying"), stateActive, reicon.RefreshCw, true
	case core.UpdateError:
		if st.Release != nil {
			return l10n.T(locale, "update.badge.error"), stateDanger, reicon.CircleAlert, false
		}
		return "", stateNeutral, reicon.Info, false
	default:
		return "", stateNeutral, reicon.Info, false
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

func updateReleaseURL(rel *core.UpdateRelease) string {
	if rel == nil {
		return ""
	}
	raw := strings.TrimSpace(rel.ReleaseURL)
	if strings.HasPrefix(raw, "https://") {
		return raw
	}
	return ""
}

func updateChangelogURL(st core.UpdateState) string {
	if releaseURL := updateReleaseURL(st.Release); releaseURL != "" {
		return releaseURL
	}
	return core.DefaultReleasesURL
}

func formatReleasePublishedDate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2006-01-02")
}
