package pages

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func polystrataRuntimeNetworkFailure(task core.DownloadTask) bool {
	if task.Kind != "polystrata" || task.State != "failed" || task.Error == nil {
		return false
	}
	failureStage, _ := task.Error.Details["failure_stage"].(string)
	inRuntimeStage := task.Stage == "downloading_runtime" ||
		failureStage == "downloading_runtime" ||
		failureStage == "download_open" ||
		failureStage == "download_body" ||
		slices.Contains(task.Error.Limitations, "engine_acquire_incomplete")
	if !inRuntimeStage {
		return false
	}
	cause, _ := task.Error.Details["failure_cause"].(string)
	return cause == "connection_error" || cause == "network_timeout"
}

func polystrataRuntimeProxyHint(task core.DownloadTask, proxyConfigured bool) string {
	if !polystrataRuntimeNetworkFailure(task) {
		return ""
	}
	if !proxyConfigured {
		return l10n.TR("downloads.polystrata.proxy_hint.unconfigured")
	}
	return l10n.TR("downloads.polystrata.proxy_hint.configured")
}

func (s *viewState) polystrataRuntimeProxyGuidance(c *ui.Context, task core.DownloadTask) {
	proxyConfigured := strings.TrimSpace(emptyText(s.settings.Settings.DownloadProxy)) != ""
	hint := polystrataRuntimeProxyHint(task, proxyConfigured)
	if hint == "" {
		return
	}
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("polystrata-proxy-guidance-"+task.TaskId)).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
		ui.Text(c, hint).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		outlineButton(c, tr(c, "downloads.polystrata.proxy_hint.action")).Label(trf(c, "downloads.polystrata.proxy_hint.action_label", "taskId", task.TaskId)).OnClick(func() {
			s.selected = "settings"
		})
	})
}
