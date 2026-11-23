package pages

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

var releaseHTMLCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

func updateBadgePresentation(st core.UpdateState) (label string, kind stateKind, icon func() *ui.SVG, animated bool) {
	switch st.State {
	case core.UpdateAvailable:
		ver := ""
		if st.Release != nil {
			ver = st.Release.Version
		}
		if ver == "" {
			return "发现新版本", stateActive, lucide.ArrowUpRight, false
		}
		return "新版本 v" + ver, stateActive, lucide.ArrowUpRight, false
	case core.UpdateDownloading:
		if st.TotalBytes != nil && *st.TotalBytes > 0 && st.DownloadedBytes >= 0 {
			pct := float64(st.DownloadedBytes) * 100 / float64(*st.TotalBytes)
			if pct > 100 {
				pct = 100
			}
			return fmt.Sprintf("下载更新 %.0f%%", pct), stateActive, lucide.Download, true
		}
		return "下载更新中", stateActive, lucide.Download, true
	case core.UpdateVerifying:
		return "校验更新", stateActive, lucide.RefreshCw, true
	case core.UpdateReadyToApply:
		ver := ""
		if st.Release != nil {
			ver = st.Release.Version
		}
		if ver == "" {
			return "更新已就绪", stateSuccess, lucide.Download, false
		}
		return "待安装 v" + ver, stateSuccess, lucide.Download, false
	case core.UpdateApplying:
		return "正在安装更新", stateActive, lucide.RefreshCw, true
	case core.UpdateError:
		if st.Release != nil {
			return "更新失败", stateDanger, lucide.CircleAlert, false
		}
		return "", stateNeutral, lucide.Info, false
	default:
		return "", stateNeutral, lucide.Info, false
	}
}

func updateSettingsStatus(st core.UpdateState) (string, stateKind) {
	switch st.State {
	case core.UpdateChecking:
		if st.ManualCheck {
			return "正在检查更新…", stateActive
		}
		return "", stateNeutral
	case core.UpdateUpToDate:
		return "已是最新版本", stateSuccess
	case core.UpdateAvailable:
		if st.Release != nil {
			return "发现新版本 v" + st.Release.Version, stateActive
		}
		return "发现新版本", stateActive
	case core.UpdateDownloading:
		return "正在下载更新：" + formatUpdateProgress(st), stateActive
	case core.UpdateVerifying:
		return "正在校验安装包…", stateActive
	case core.UpdateReadyToApply:
		if st.Release != nil {
			return "v" + st.Release.Version + " 已校验就绪", stateSuccess
		}
		return "更新已就绪", stateSuccess
	case core.UpdateApplying:
		return "正在安装更新并准备重启…", stateActive
	case core.UpdateError:
		if st.ErrorMessage != "" {
			return st.ErrorMessage, stateDanger
		}
		return "检查更新失败，请重试", stateDanger
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
	parts := make([]string, 0, 2)
	if w.ActiveRuns > 0 {
		parts = append(parts, fmt.Sprintf("%d 个测评任务", w.ActiveRuns))
	}
	if w.ActiveDownloads > 0 {
		parts = append(parts, fmt.Sprintf("%d 个下载任务", w.ActiveDownloads))
	}
	if len(parts) == 0 {
		return ""
	}
	return "当前有 " + strings.Join(parts, "和 ") + "正在运行，安装更新前需先中止。"
}

func summarizeReleaseNotes(notes string) string {
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
		return "暂无发布说明。"
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
