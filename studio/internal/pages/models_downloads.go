package pages

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadDisplayPriority(task core.DownloadTask, index int) int {
	switch task.State {
	case "downloading":
		return 0
	case "failed":
		if !s.downloadResolved(task, index) {
			return 1
		}
	}
	return 2
}

func (s *viewState) orderedDownloads() []core.DownloadTask {
	type rankedTask struct {
		task     core.DownloadTask
		priority int
	}
	ranked := make([]rankedTask, len(s.downloads))
	for i, task := range s.downloads {
		ranked[i] = rankedTask{task: task, priority: s.downloadDisplayPriority(task, i)}
	}
	slices.SortStableFunc(ranked, func(a, b rankedTask) int {
		if a.priority != b.priority {
			return cmp.Compare(a.priority, b.priority)
		}
		if a.task.Kind == "engine" && b.task.Kind != "engine" {
			return -1
		}
		if a.task.Kind != "engine" && b.task.Kind == "engine" {
			return 1
		}
		return 0
	})
	tasks := make([]core.DownloadTask, len(ranked))
	for i, item := range ranked {
		tasks[i] = item.task
	}
	return tasks
}

func (s *viewState) downloadsView(c *ui.Context) {
	if len(s.downloads) == 0 {
		ui.Text(c, "暂无下载任务。")
		return
	}
	componentFold(c, "单位与进度口径", func() {
		ui.Text(c, "文件大小按 1 MiB = 1,048,576 bytes、1 GiB = 1,073,741,824 bytes 换算；速率按 bytes/s 记录。")
		ui.Text(c, "总量未知时不显示百分比或剩余时间，活动指示只表示任务仍在进行。")
	})
	for _, task := range s.orderedDownloads() {
		s.downloadTaskCard(c, task)
	}
}

func downloadDisplayName(task core.DownloadTask) string {
	if task.DisplayName != "" {
		return task.DisplayName
	}
	return task.OutName
}

func downloadStageLabel(stage string) string {
	switch stage {
	case "downloading":
		return "下载中"
	case "verifying":
		return "校验中"
	case "extracting":
		return "解压中"
	case "preparing":
		return "准备中"
	}
	return ""
}

func formatDownloadBytes(v int64) string {
	switch {
	case v >= 1073741824:
		return fmt.Sprintf("%.2f GiB", float64(v)/1073741824)
	case v >= 1048576:
		return fmt.Sprintf("%.2f MiB", float64(v)/1048576)
	case v >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(v)/1024)
	default:
		return fmt.Sprintf("%d B", v)
	}
}

func formatDownloadETA(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 1 {
		return "即将完成"
	}
	total := int64(math.Round(seconds))
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	if total < 3600 {
		return fmt.Sprintf("%dm %ds", total/60, total%60)
	}
	return fmt.Sprintf("%dh %dm", total/3600, (total%3600)/60)
}

func (s *viewState) downloadTaskCard(c *ui.Context, task core.DownloadTask) {
	kind := "模型"
	if task.Kind == "engine" {
		kind = "引擎"
	}
	engineIndeterminate := task.Kind == "engine" && task.TotalBytes == nil && task.DownloadedBytes == 0 && task.BytesPerSecond == nil && task.Stage == ""
	var metrics []taskMetric
	var status string
	active := task.State == "downloading"
	switch {
	case engineIndeterminate:
		status = engineTaskStatus(task)
	case task.TotalBytes != nil:
		total := *task.TotalBytes
		pct := progressValue(task.DownloadedBytes, task.TotalBytes) * 100
		if pct < 0 {
			pct = 0
		}
		stageText := downloadStageLabel(task.Stage)
		if stageText == "" {
			stageText = stateLabel(task.State)
		}
		rateVal, rateReason := "缺测", "无速率读数"
		if task.BytesPerSecond != nil {
			rateVal = formatRate(*task.BytesPerSecond)
			rateReason = ""
			if task.State == "downloading" && *task.BytesPerSecond > 0 && task.DownloadedBytes < total && (task.Stage == "" || task.Stage == "downloading") {
				rateReason = "剩余 " + formatDownloadETA(float64(total-task.DownloadedBytes)/(*task.BytesPerSecond))
			}
		} else if task.State == "downloading" && task.Stage != "" && task.Stage != "downloading" {
			rateVal, rateReason = stageText, ""
		}
		metrics = []taskMetric{
			{"进度", fmt.Sprintf("%s / %s", formatDownloadBytes(task.DownloadedBytes), formatDownloadBytes(total)), fmt.Sprintf("%.1f%%", pct)},
			{"速率", rateVal, rateReason},
			{"阶段", stageText, ""},
		}
		active = false
	default:
		stageReason := downloadStageLabel(task.Stage)
		metrics = []taskMetric{
			{"已观测", formatDownloadBytes(task.DownloadedBytes), stageReason},
			{"速率", "缺测", "无速率读数"},
			{"总量", "总量未知", ""},
		}
		if task.BytesPerSecond != nil {
			metrics[1] = taskMetric{"速率", formatRate(*task.BytesPerSecond), ""}
		}
	}
	color := stateColor(c, stateNeutral)
	switch task.State {
	case "done":
		color = stateColor(c, stateSuccess)
	case "failed":
		color = stateColor(c, stateDanger)
	case "downloading":
		color = stateColor(c, stateActive)
	}
	ui.Column(c.Key(task.TaskId)).Children(func() {
		taskCard(c, taskCardOptions{
			kind: kind, name: downloadDisplayName(task), state: stateLabel(task.State), color: color,
			active:  active,
			metrics: metrics,
			status:  status,
			notice: func() {
				if task.State == "downloading" && task.TotalBytes != nil {
					progressBar(c, progressValue(task.DownloadedBytes, task.TotalBytes), "下载进度").Height(tokens(c).Density.Tight)
				}
				downloadErrorView(c, task.Error)
			},
			actions: func() { s.downloadActions(c, task) },
			details: func() { downloadDetails(c, task) },
		})
	})
}

func engineTaskStatus(task core.DownloadTask) string {
	switch task.State {
	case "downloading":
		return "正在下载引擎归档…"
	case "done":
		return "引擎归档下载完成。"
	case "failed":
		return "引擎归档下载失败。"
	case "cancelled":
		return "引擎归档下载已取消。"
	}
	return "引擎归档状态未知。"
}

func downloadTaskSameSource(a, b core.DownloadTask) bool {
	if a.Kind != b.Kind {
		return false
	}
	for _, left := range []string{a.SourceUrl, a.RequestedSourceUrl} {
		if left == "" {
			continue
		}
		for _, right := range []string{b.SourceUrl, b.RequestedSourceUrl} {
			if right != "" && left == right {
				return true
			}
		}
	}
	return false
}

func (s *viewState) downloadRetryActive(task core.DownloadTask) bool {
	for _, other := range s.downloads {
		if other.TaskId == task.TaskId || other.State != "downloading" {
			continue
		}
		if downloadTaskSameSource(task, other) {
			return true
		}
	}
	return false
}

func (s *viewState) downloadActions(c *ui.Context, task core.DownloadTask) {
	pendingHere := s.pendingDestructive.kind == string(destructiveCancelDownload) && s.pendingDestructive.id == task.TaskId
	if task.State == "downloading" || pendingHere {
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveCancelDownload, id: task.TaskId, label: "取消下载", confirmLabel: "确认取消下载",
			prompt:   "将取消本次下载；已写入的输出目录保留，再次下载会使用新的目录名。",
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
	retryDisabled := s.busy || s.downloadRetryActive(task)
	if task.State == "cancelled" {
		button(c, "继续下载").Disabled(retryDisabled).OnClick(func() {
			if s.simulate(previewRetryDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.RetryDownload(task.TaskId)
				return "已继续下载任务", e
			})
		})
		return
	}
	if task.State != "failed" {
		return
	}
	button(c, "重新创建下载任务").Disabled(retryDisabled).OnClick(func() {
		if s.simulate(previewRetryDownload{id: task.TaskId}) {
			return
		}
		s.workDownload(func() (string, *core.ApiError) {
			_, e := s.service.RetryDownload(task.TaskId)
			return "已重新创建下载任务", e
		})
	})
}

func downloadDetails(c *ui.Context, task core.DownloadTask) {
	ui.Text(c, "实际目录名："+task.OutName).Selectable()
	ui.Text(c, "文件："+displayName(text(task.FileName)))
	if task.Kind == "engine" && task.TotalBytes == nil && task.DownloadedBytes == 0 && task.BytesPerSecond == nil {
		ui.Text(c, "已下载 缺测 · 速率 缺测")
	} else {
		if task.TotalBytes != nil {
			ui.Text(c, fmt.Sprintf("已下载 %d / %d bytes", task.DownloadedBytes, *task.TotalBytes))
		} else {
			ui.Text(c, fmt.Sprintf("已下载 %d bytes", task.DownloadedBytes))
		}
		if task.BytesPerSecond != nil {
			ui.Text(c, fmt.Sprintf("速率 %s bytes/s", groupedInt(int64(*task.BytesPerSecond))))
		}
	}
	if task.EnginePath != nil {
		ui.Text(c, "引擎："+displayName(*task.EnginePath)).Selectable()
	}
	if task.Result != nil {
		ui.Text(c, fmt.Sprintf("实测大小：%d bytes · SHA-256：%s", task.Result.Bytes, task.Result.Sha256)).Selectable()
	}
	if task.Error != nil {
		ui.Text(c, "错误码："+task.Error.Code).Selectable()
	}
	if task.State == "failed" || task.State == "cancelled" {
		ui.Text(c, "输出目录与未完成分片保留；重试时将自动续传或使用新目录名。")
	}
}

func downloadFailureStageLabel(stage string) string {
	switch stage {
	case "token":
		return "读取访问凭据"
	case "download_open":
		return "建立下载连接"
	case "download_body":
		return "传输下载数据"
	case "disk_write":
		return "写入本地文件"
	case "checksum", "verify":
		return "校验文件完整性"
	default:
		return stage
	}
}

func remainingDownloadErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	extra := map[string]any{}
	for k, v := range details {
		if k == "failure_cause" || k == "failure_stage" {
			continue
		}
		if k == "reason" {
			if reason, ok := v.(string); ok && reason != "" {
				if label := conflictReasonLabel(reason); label != "" {
					extra["原因"] = label
				} else {
					extra["原因"] = human(reason)
				}
				continue
			}
		}
		extra[k] = v
	}
	return extra
}

func downloadErrorView(c *ui.Context, e *core.ApiError) {
	if e == nil {
		return
	}
	message := e.Message
	if message == "" {
		message = human(e.Code)
	}
	ui.Text(c, message).TextColor(stateColor(c, stateDanger))
	for _, v := range e.Limitations {
		ui.Text(c, human(v)).TextColor(stateColor(c, stateDanger))
	}
	if cause, ok := e.Details["failure_cause"].(string); ok && cause != "" {
		ui.Text(c, human(cause)).TextColor(stateColor(c, stateDanger))
	}
	if stage, ok := e.Details["failure_stage"].(string); ok && stage != "" {
		ui.Text(c, "失败阶段："+downloadFailureStageLabel(stage)).
			FontSize(tokens(c).Type.Caption).
			TextColor(tokens(c).Color.FgSecondary)
	}
	if extra := remainingDownloadErrorDetails(e.Details); len(extra) > 0 {
		kvview(c, "kv-error-details", extra)
	}
}
