package pages

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
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
		aEngine := a.task.Kind == "engine" || a.task.Kind == "polystrata" || a.task.Kind == "engine_update"
		bEngine := b.task.Kind == "engine" || b.task.Kind == "polystrata" || b.task.Kind == "engine_update"
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
		ui.Text(c, tr(c, "downloads.tasks.empty"))
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
		return l10n.TR("downloads.stage.downloading")
	case "verifying":
		return l10n.TR("downloads.stage.verifying")
	case "extracting":
		return l10n.TR("downloads.stage.extracting")
	case "preparing":
		return l10n.TR("downloads.stage.preparing")
	case "preflight":
		return l10n.TR("downloads.stage.preflight")
	case "downloading_runtime":
		return l10n.TR("downloads.stage.downloading_runtime")
	case "setup_engine":
		return l10n.TR("downloads.stage.setup_engine")
	case "upgrading":
		return l10n.TR("downloads.stage.upgrading")
	case "downloading_model":
		return l10n.TR("downloads.stage.downloading_model")
	case "verifying_installation":
		return l10n.TR("downloads.stage.verifying_installation")
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
		return l10n.TR("downloads.eta.almost_done")
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
	kind := tr(c, "downloads.kind.model")
	if task.Kind == "engine" {
		kind = tr(c, "downloads.kind.engine")
	} else if task.Kind == "engine_update" {
		kind = tr(c, "downloads.kind.engine_update")
	}
	engineIndeterminate := (task.Kind == "engine" && task.TotalBytes == nil && task.DownloadedBytes == 0 && task.BytesPerSecond == nil && task.Stage == "") || task.Kind == "engine_update"
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
				rateReason = trf(c, "downloads.eta.remaining", "eta", formatDownloadETA(float64(total-task.DownloadedBytes)/(*task.BytesPerSecond)))
			}
		} else if task.State == "downloading" && task.Stage != "" && task.Stage != "downloading" {
			rateVal, rateReason = stageText, ""
		}
		metrics = []taskMetric{
			{tr(c, "downloads.metric.progress"), fmt.Sprintf("%s / %s", formatDownloadBytes(task.DownloadedBytes), formatDownloadBytes(total)), fmt.Sprintf("%.1f%%", pct)},
			{tr(c, "downloads.metric.rate"), rateVal, rateReason},
			{tr(c, "downloads.metric.stage"), stageText, ""},
		}
		active = false
	default:
		stageReason := downloadStageLabel(task.Stage)
		metrics = []taskMetric{
			{tr(c, "downloads.metric.downloaded"), formatDownloadBytes(task.DownloadedBytes), stageReason},
			{tr(c, "downloads.metric.rate"), "—", ""},
			{tr(c, "downloads.metric.total"), "—", ""},
		}
		if task.BytesPerSecond != nil {
			metrics[1] = taskMetric{tr(c, "downloads.metric.rate"), formatRate(*task.BytesPerSecond), ""}
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
					progressBar(c, progressValue(task.DownloadedBytes, task.TotalBytes), tr(c, "downloads.progress.download_label")).Height(tokens(c).Density.Tight)
				}
				s.downloadErrorView(c, task.Error)
			},
			actions: func() { s.downloadActions(c, task) },
		})
	})
}

func engineTaskStatus(task core.DownloadTask) string {
	if task.Kind == "engine_update" {
		switch task.State {
		case "downloading":
			return l10n.TR("downloads.engine_update.downloading")
		case "done":
			return l10n.TR("downloads.engine_update.done")
		case "failed":
			return l10n.TR("downloads.engine_update.failed")
		case "cancelled":
			return l10n.TR("downloads.engine_update.cancelled")
		}
		return l10n.TR("downloads.engine_update.unknown")
	}
	switch task.State {
	case "downloading":
		return l10n.TR("downloads.engine_archive.downloading")
	case "done":
		return l10n.TR("downloads.engine_archive.done")
	case "failed":
		return l10n.TR("downloads.engine_archive.failed")
	case "cancelled":
		return l10n.TR("downloads.engine_archive.cancelled")
	}
	return l10n.TR("downloads.engine_archive.unknown")
}

func downloadTaskSamePinnedSource(a, b core.DownloadTask) bool {
	return a.Kind == b.Kind && a.SourceUrl != "" && a.SourceUrl == b.SourceUrl
}

func (s *viewState) downloadTaskInCurrentWorkspace(task core.DownloadTask) bool {
	if task.Kind == "polystrata" {
		return true
	}
	root := emptyText(s.settings.Effective.ModelsRoot)
	if task.Kind == "engine" {
		if s.service != nil && s.service.BoundPaths() != nil {
			root = s.service.BoundPaths().Engines
		} else if ws := emptyText(s.settings.Effective.WorkspaceRoot); ws != "" {
			root = filepath.Join(ws, "engines")
		} else {
			root = ""
		}
	}
	if root == "" || task.OutDir == "" {
		return true
	}
	if !filepath.IsAbs(task.OutDir) {
		return false
	}
	if s.preview != nil {
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(task.OutDir))
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	_, err := core.Resolve(root, task.OutDir)
	return err == nil
}

func (s *viewState) downloadRetryActive(task core.DownloadTask) bool {
	for _, other := range s.downloads {
		if other.TaskId == task.TaskId || other.State != "downloading" {
			continue
		}
		if task.Kind == "polystrata" && other.Kind == "polystrata" {
			return true
		}
		if downloadTaskSamePinnedSource(task, other) && s.downloadTaskInCurrentWorkspace(other) {
			return true
		}
	}
	return false
}

func (s *viewState) downloadActions(c *ui.Context, task core.DownloadTask) {
	pendingHere := s.pendingDestructive.kind == string(destructiveCancelDownload) && s.pendingDestructive.id == task.TaskId
	if task.State == "downloading" || pendingHere {
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveCancelDownload, id: task.TaskId, label: tr(c, "downloads.action.cancel"), confirmLabel: tr(c, "downloads.action.cancel_confirm"),
			prompt:   tr(c, "downloads.action.cancel_prompt"),
			disabled: s.busy,
		}) {
			if s.simulate(previewCancelDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.CancelDownload(task.TaskId)
				return l10n.TR("downloads.toast.cancel_requested"), e
			})
		}
	}
	if task.Kind == "engine_update" {
		return
	}
	retryDisabled := s.busy || s.downloadRetryActive(task)
	if task.State == "cancelled" {
		button(c, tr(c, "downloads.action.resume")).Disabled(retryDisabled).OnClick(func() {
			if s.simulate(previewRetryDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.RetryDownload(task.TaskId)
				return l10n.TR("downloads.toast.resumed"), e
			})
		})
		return
	}
	if task.State != "failed" {
		return
	}
	button(c, tr(c, "downloads.action.recreate")).Disabled(retryDisabled).OnClick(func() {
		if s.simulate(previewRetryDownload{id: task.TaskId}) {
			return
		}
		s.workDownload(func() (string, *core.ApiError) {
			_, e := s.service.RetryDownload(task.TaskId)
			return l10n.TR("downloads.toast.recreated"), e
		})
	})
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
		return l10n.TR("downloads.failure_stage.token")
	case "download_open":
		return l10n.TR("downloads.failure_stage.download_open")
	case "download_body":
		return l10n.TR("downloads.failure_stage.download_body")
	case "disk_write":
		return l10n.TR("downloads.failure_stage.disk_write")
	case "checksum", "verify":
		return l10n.TR("downloads.failure_stage.checksum")
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
				reasonKey := l10n.TR("downloads.error.reason_key")
				if label := conflictReasonLabel(reason); label != "" {
					extra[reasonKey] = label
				} else {
					extra[reasonKey] = human(reason)
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
		ui.Text(c, trf(c, "downloads.error.failure_stage", "stage", downloadFailureStageLabel(failureStage))).
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
					outlineButton(c, tr(c, "downloads.error.copy_fix_cmd")).Label(trf(c, "downloads.error.copy_fix_cmd_label", "index", strconv.Itoa(idx))).OnClick(func() {
						s.copyDownloadText(c, tr(c, "downloads.copy_subject.fix_cmd"), item.fixCommand)
					})
				})
			}
		})
	}
	if extra := remainingDownloadErrorDetails(e.Details); len(extra) > 0 {
		kvview(c, "kv-error-details", extra)
	}
}
