package pages

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

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
		aEngine := a.task.Kind == "engine" || a.task.Kind == "polystrata"
		bEngine := b.task.Kind == "engine" || b.task.Kind == "polystrata"
		if aEngine && !bEngine {
			return -1
		}
		if !aEngine && bEngine {
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
	case "preflight":
		return "环境预检"
	case "downloading_runtime":
		return "下载运行时"
	case "setup_engine":
		return "安装引擎环境"
	case "downloading_model":
		return "下载模型"
	case "verifying_installation":
		return "核验安装"
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

func downloadStateColor(c *ui.Context, state string) ui.Color {
	switch state {
	case "done":
		return stateColor(c, stateSuccess)
	case "failed":
		return stateColor(c, stateDanger)
	case "downloading":
		return stateColor(c, stateActive)
	default:
		return stateColor(c, stateNeutral)
	}
}

func (s *viewState) downloadTaskCard(c *ui.Context, task core.DownloadTask) {
	if task.Kind == "polystrata" {
		s.polystrataDownloadTaskCard(c, task)
		return
	}
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
		rateVal, rateReason := "—", ""
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
			{"已下载", formatDownloadBytes(task.DownloadedBytes), stageReason},
			{"速率", "—", ""},
			{"总量", "—", ""},
		}
		if task.BytesPerSecond != nil {
			metrics[1] = taskMetric{"速率", formatRate(*task.BytesPerSecond), ""}
		}
	}
	color := downloadStateColor(c, task.State)
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
				s.downloadErrorView(c, task.Error)
			},
			actions: func() { s.downloadActions(c, task) },
			details: func() { s.downloadDetails(c, task) },
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

func downloadTaskSamePinnedSource(a, b core.DownloadTask) bool {
	return a.Kind == b.Kind && a.SourceUrl != "" && a.SourceUrl == b.SourceUrl
}

func (s *viewState) downloadRetryActive(task core.DownloadTask) bool {
	for _, other := range s.downloads {
		if other.TaskId == task.TaskId || other.State != "downloading" {
			continue
		}
		if task.Kind == "polystrata" && other.Kind == "polystrata" {
			return true
		}
		if downloadTaskSamePinnedSource(task, other) {
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

func (s *viewState) downloadDetails(c *ui.Context, task core.DownloadTask) {
	ui.Text(c, "目录名："+task.OutName).Selectable()
	if fileName := strings.TrimSpace(text(task.FileName)); fileName != "" {
		ui.Text(c, "文件："+displayName(fileName))
	} else if task.Kind != "polystrata" {
		ui.Text(c, "文件："+displayName(""))
	}
	noBytesYet := task.TotalBytes == nil && task.DownloadedBytes == 0 && task.BytesPerSecond == nil
	if task.Kind == "engine" && noBytesYet {
		ui.Text(c, "已下载 — · 速率 —")
	} else if !(task.Kind == "polystrata" && noBytesYet) {
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
	if task.InstallationPath != nil && *task.InstallationPath != "" {
		ui.Text(c, "安装声明："+*task.InstallationPath).Selectable()
	}
	if task.ModelConfigPath != nil && *task.ModelConfigPath != "" {
		ui.Text(c, "模型配置："+*task.ModelConfigPath).Selectable()
	}
	if task.LogPath != nil && *task.LogPath != "" {
		logPath := *task.LogPath
		ui.Text(c, "安装日志："+logPath).Selectable()
		if task.Kind != "polystrata" {
			componentFoldKeyed(c, "download-log-"+task.TaskId, "查看日志尾部", func() {
				s.renderDownloadLogTail(c, task.TaskId, logPath)
			})
		}
	}
	if task.Result != nil {
		ui.Text(c, fmt.Sprintf("大小：%s · SHA-256：%s", formatDownloadBytes(task.Result.Bytes), task.Result.Sha256)).Selectable()
	}
	if task.Error != nil {
		ui.Text(c, "错误码："+task.Error.Code).Selectable()
	}
}

func readLogTail(path string, maxBytes int64) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil || len(buf) == 0 {
		return ""
	}
	if offset > 0 {
		for len(buf) > 0 && !utf8.Valid(buf[:1]) && !utf8.RuneStart(buf[0]) {
			buf = buf[1:]
		}
	}
	return strings.TrimSpace(string(buf))
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
		if label := downloadStageLabel(stage); label != "" {
			return label
		}
		return stage
	}
}

func remainingDownloadErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	failureStage, _ := details["failure_stage"].(string)
	extra := map[string]any{}
	for k, v := range details {
		if k == "failure_cause" || k == "failure_stage" || k == "blockers" {
			continue
		}
		if k == "stage" && failureStage != "" {
			if stage, ok := v.(string); ok && stage == failureStage {
				continue
			}
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

type preflightBlockerItem struct {
	code          string
	message       string
	fixCommand    string
	requiredBytes *int64
	freeBytes     *int64
}

func extractPreflightBlockers(details map[string]any) []preflightBlockerItem {
	raw, ok := details["blockers"]
	if !ok || raw == nil {
		return nil
	}
	var out []preflightBlockerItem
	appendMap := func(m map[string]any) {
		msg, _ := m["message"].(string)
		code, _ := m["code"].(string)
		fix, _ := m["fix_command"].(string)
		if strings.TrimSpace(msg) == "" && strings.TrimSpace(code) == "" {
			return
		}
		out = append(out, preflightBlockerItem{
			code:          strings.TrimSpace(code),
			message:       strings.TrimSpace(msg),
			fixCommand:    strings.TrimSpace(fix),
			requiredBytes: blockerBytesPtr(m["required_bytes"]),
			freeBytes:     blockerBytesPtr(m["free_bytes"]),
		})
	}
	switch list := raw.(type) {
	case []map[string]any:
		for _, m := range list {
			appendMap(m)
		}
	case []any:
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				appendMap(m)
			}
		}
	}
	return out
}

func (s *viewState) downloadErrorView(c *ui.Context, e *core.ApiError) {
	if e == nil {
		return
	}
	t := tokens(c)
	blockers := extractPreflightBlockers(e.Details)
	failureStage, _ := e.Details["failure_stage"].(string)
	failureStage = strings.TrimSpace(failureStage)

	blockerCodes := map[string]bool{}
	blockerLines := map[string]bool{}
	for _, b := range blockers {
		if b.code != "" {
			blockerCodes[b.code] = true
		}
		if base := strings.TrimSpace(preflightBlockerBaseText(b)); base != "" {
			blockerLines[base] = true
		}
		if full := strings.TrimSpace(formatPreflightBlockerText(b)); full != "" {
			blockerLines[full] = true
		}
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		if e.Code == "cli_blocked" && len(blockers) > 0 && failureStage == "preflight" {
			message = human("polystrata_preflight_blocked")
		} else {
			message = human(e.Code)
		}
	}
	shownLines := map[string]bool{}
	if message != "" {
		ui.Text(c, message).TextColor(stateColor(c, stateDanger))
		shownLines[message] = true
	}
	for _, raw := range e.Limitations {
		v := strings.TrimSpace(raw)
		if v == "" || blockerCodes[v] {
			continue
		}
		line := strings.TrimSpace(human(v))
		if line == "" || shownLines[line] || blockerLines[line] {
			continue
		}
		ui.Text(c, line).TextColor(stateColor(c, stateDanger))
		shownLines[line] = true
	}
	if cause, ok := e.Details["failure_cause"].(string); ok {
		cCode := strings.TrimSpace(cause)
		if cCode != "" && !blockerCodes[cCode] {
			line := strings.TrimSpace(human(cCode))
			if line != "" && !shownLines[line] && !blockerLines[line] {
				ui.Text(c, line).TextColor(stateColor(c, stateDanger))
				shownLines[line] = true
			}
		}
	}
	if failureStage != "" {
		ui.Text(c, "失败阶段："+downloadFailureStageLabel(failureStage)).
			FontSize(t.Type.Caption).
			TextColor(t.Color.FgSecondary)
	}
	seenBlockers := map[string]bool{}
	renderIdx := 0
	for _, blocker := range blockers {
		item := blocker
		text := strings.TrimSpace(formatPreflightBlockerText(item))
		dedupKey := text + "\x00" + item.fixCommand
		if text == "" || seenBlockers[dedupKey] || (item.fixCommand == "" && shownLines[text]) {
			continue
		}
		seenBlockers[dedupKey] = true
		shownLines[text] = true
		idx := renderIdx
		renderIdx++
		applyMaterial(c, ui.Column(c.Key(fmt.Sprintf("download-blocker-%d-%s", idx, item.code))).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
			ui.Text(c, text).FontSize(t.Type.Caption).TextColor(stateColor(c, stateDanger))
			if item.fixCommand != "" {
				ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Text(c, item.fixCommand).Grow(1).MinWidth(0).Selectable().FontSize(t.Type.Caption)
					outlineButton(c, "复制修复命令").Label(fmt.Sprintf("复制修复命令 · %d", idx)).OnClick(func() {
						s.copyDownloadText(c, "修复命令", item.fixCommand)
					})
				})
			}
		})
	}
	if extra := remainingDownloadErrorDetails(e.Details); len(extra) > 0 {
		kvview(c, "kv-error-details", extra)
	}
}
