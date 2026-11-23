package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
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
		task.Error.Code == "engine_acquire_incomplete"
	if !inRuntimeStage {
		return false
	}
	cause, _ := task.Error.Details["failure_cause"].(string)
	switch cause {
	case "connection_error", "network_timeout", "transfer_error":
		return true
	case "":
		return task.Error.Code == "engine_acquire_incomplete"
	default:
		return false
	}
}

func polystrataRuntimeProxyHint(task core.DownloadTask, proxyConfigured bool) string {
	if !polystrataRuntimeNetworkFailure(task) {
		return ""
	}
	if !proxyConfigured {
		return "下载 PolyStrata 运行时（GitHub）连接失败或超时。若处于受限网络环境，请前往「设置 → 下载代理」配置代理后重试（选择 ModelScope 模型源时模型下载仍会直连）。"
	}
	return "下载 PolyStrata 运行时连接失败或超时，请检查「设置 → 下载代理」配置是否可用后重试。"
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
		outlineButton(c, "前往设置代理").Label("前往设置代理 · " + task.TaskId).OnClick(func() {
			s.selected = "settings"
		})
	})
}
