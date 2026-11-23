package pages

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

var polystrataPhaseLabels = []string{"预检", "获取运行时", "构建环境", "模型与核验"}

func polystrataPhaseIndex(task core.DownloadTask) int {
	if task.State == "done" {
		return len(polystrataPhaseLabels)
	}
	if task.Step != nil && (task.StepTotal == nil || *task.StepTotal == len(polystrataPhaseLabels)) &&
		*task.Step >= 1 && *task.Step <= len(polystrataPhaseLabels) {
		return *task.Step - 1
	}
	switch task.Stage {
	case "preflight":
		return 0
	case "downloading_runtime", "extracting":
		return 1
	case "setup_engine":
		return 2
	case "downloading_model", "verifying", "verifying_installation":
		return 3
	default:
		if task.Step != nil && *task.Step > 0 {
			return min(max(*task.Step-1, 0), len(polystrataPhaseLabels)-1)
		}
		return 0
	}
}

func (s *viewState) polystrataDownloadTaskCard(c *ui.Context, task core.DownloadTask) {
	t := tokens(c)
	stageText := downloadStageLabel(task.Stage)
	if stageText == "" {
		stageText = stateLabel(task.State)
	}
	stepVal, stepSub := stageText, ""
	if task.Step != nil && task.StepTotal != nil && *task.StepTotal > 0 {
		stepVal = fmt.Sprintf("%d / %d", *task.Step, *task.StepTotal)
		stepSub = stageText
	}
	hasByteProgress := task.TotalBytes != nil && *task.TotalBytes > 0
	hasPercent := task.Percent != nil
	progressVal, progressSub := stageText, ""
	switch {
	case hasByteProgress:
		pct := progressValue(task.DownloadedBytes, task.TotalBytes) * 100
		if pct < 0 {
			pct = 0
		}
		progressVal = fmt.Sprintf("%s / %s", formatDownloadBytes(task.DownloadedBytes), formatDownloadBytes(*task.TotalBytes))
		progressSub = fmt.Sprintf("%.1f%%", pct)
	case hasPercent:
		progressVal = fmt.Sprintf("%.0f%%", math.Max(0, math.Min(100, *task.Percent)))
	}
	metrics := []taskMetric{
		{"步骤", stepVal, stepSub},
		{"进度", progressVal, progressSub},
	}
	if task.BytesPerSecond != nil {
		rateSub := ""
		if task.State == "downloading" && hasByteProgress && *task.BytesPerSecond > 0 && task.DownloadedBytes < *task.TotalBytes {
			rateSub = "剩余 " + formatDownloadETA(float64(*task.TotalBytes-task.DownloadedBytes)/(*task.BytesPerSecond))
		}
		metrics = append(metrics, taskMetric{"速率", formatRate(*task.BytesPerSecond), rateSub})
	}

	active := task.State == "downloading" && !hasByteProgress && !hasPercent
	color := downloadStateColor(c, task.State)
	ui.Column(c.Key(task.TaskId)).Children(func() {
		taskCard(c, taskCardOptions{
			kind: "PolyStrata 引导安装", name: downloadDisplayName(task), state: stateLabel(task.State), color: color,
			active:  active,
			metrics: metrics,
			notice: func() {
				if task.State == "downloading" {
					phaseProgress(c, phaseProgressOptions{
						Labels:   polystrataPhaseLabels,
						Current:  polystrataPhaseIndex(task),
						RowLabel: "PolyStrata 安装阶段",
						BarLabel: "安装阶段进度",
					})
					if hasByteProgress {
						progressBar(c, progressValue(task.DownloadedBytes, task.TotalBytes), "下载进度").Height(t.Density.Tight)
					} else if hasPercent {
						ratio := math.Max(0, math.Min(1, *task.Percent/100))
						progressBar(c, ratio, "安装进度").Height(t.Density.Tight)
					}
				}
				if task.Detail != nil && strings.TrimSpace(*task.Detail) != "" {
					ui.Text(c, strings.TrimSpace(*task.Detail)).SingleLine().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
				if task.LogPath != nil && strings.TrimSpace(*task.LogPath) != "" {
					logPath := strings.TrimSpace(*task.LogPath)
					componentFoldKeyed(c, "polystrata-live-log-"+task.TaskId, "查看安装日志", func() {
						s.renderDownloadLogTail(c, task.TaskId, logPath)
					})
				}
				s.downloadErrorView(c, task.Error)
				s.polystrataRuntimeProxyGuidance(c, task)
			},
			actions: func() { s.polystrataDownloadActions(c, task) },
			details: func() { s.downloadDetails(c, task) },
		})
	})
}

func (s *viewState) polystrataDownloadActions(c *ui.Context, task core.DownloadTask) {
	t := tokens(c)
	pendingHere := s.pendingDestructive.kind == string(destructiveCancelDownload) && s.pendingDestructive.id == task.TaskId
	if task.State == "downloading" || pendingHere {
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveCancelDownload, id: task.TaskId, label: "取消下载", confirmLabel: "确认取消下载",
			prompt:   "取消本次下载？已下载文件将保留。",
			disabled: s.busy,
		}) {
			if s.simulate(previewCancelDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.CancelDownload(task.TaskId)
				return "已请求取消下载", e
			})
		}
	}
	if task.State != "failed" && task.State != "cancelled" {
		return
	}
	retryDisabled := s.busy || s.downloadRetryActive(task)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
		if primaryButton(c, "断点续装").Disabled(retryDisabled).Clicked() {
			if s.simulate(previewRetryDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.RetryDownload(task.TaskId)
				return "已继续 PolyStrata 安装", e
			})
		}
		if outlineButton(c, "更换方案或下载源").Disabled(s.busy).Clicked() {
			s.openPolystrataRecoveryDrawer(task)
		}
	})
}

func (s *viewState) openPolystrataRecoveryDrawer(task core.DownloadTask) {
	fam, ok := s.engineCatalogRegistry().FamilyByID("polystrata")
	if !ok || len(fam.Releases) == 0 {
		return
	}
	st := s.ensureDownloadsUI()
	if src := strings.TrimSpace(task.ModelSource); src != "" {
		st.setPolystrataModelSource(src)
	}
	hostPlatform := s.downloadsHostPlatform()
	releases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(releases) == 0 {
		releases = fam.Releases
	}
	rel := resolveEngineRelease(st.selectedRelease[fam.ID], releases)
	build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)

	parts := strings.Split(task.DisplayName, " · ")
	if len(parts) >= 2 && strings.TrimSpace(parts[0]) == "PolyStrata" {
		wantFamily := strings.TrimSpace(parts[1])
		wantModel := ""
		if len(parts) >= 3 {
			wantModel = strings.TrimSpace(parts[2])
		}
		for _, opt := range s.polystrataDrawerOptions(hostPlatform) {
			if strings.EqualFold(opt.Family, wantFamily) && (wantModel == "" || strings.EqualFold(opt.Size, wantModel)) {
				st.polystrataDrawerModelID = opt.ModelID
				break
			}
		}
	}
	s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
}

func (s *viewState) renderDownloadLogTail(c *ui.Context, taskID, logPath string) {
	var snippet string
	if s.preview != nil {
		snippet = s.preview.downloadLogTail(taskID)
	} else {
		snippet = readLogTail(logPath, 4096)
	}
	if snippet == "" {
		ui.Text(c, "暂无可显示的日志内容。").FontSize(tokens(c).Type.Caption).TextColor(tokens(c).Color.FgSecondary)
		return
	}
	ui.Text(c, snippet).Selectable().FontSize(tokens(c).Type.Caption)
}
