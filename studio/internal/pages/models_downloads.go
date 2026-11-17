package pages

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// downloadsView lists download tasks. Units and progress semantics stay under
// one fold at the top; each card keeps one observation line plus, at most, one
// explanation of what the source cannot report.
func (s *viewState) downloadsView(c *ui.Context) {
	if len(s.downloads) == 0 {
		ui.Text(c, "暂无下载任务。")
		return
	}
	componentFold(c, "单位与进度口径", func() {
		ui.Text(c, "文件大小按 1 MiB = 1,048,576 bytes、1 GiB = 1,073,741,824 bytes 换算；速率按 bytes/s 记录。")
		ui.Text(c, "总量未知时不显示百分比或剩余时间，活动指示只表示任务仍在进行。")
	})
	tasks := slices.Clone(s.downloads)
	slices.SortStableFunc(tasks, func(a, b core.DownloadTask) int {
		if a.Kind == "engine" && b.Kind != "engine" {
			return -1
		}
		if a.Kind != "engine" && b.Kind == "engine" {
			return 1
		}
		return 0
	})
	for _, task := range tasks {
		s.downloadTaskCard(c, task)
	}
}

func (s *viewState) downloadTaskCard(c *ui.Context, task core.DownloadTask) {
	kind := "模型任务"
	if task.Kind == "engine" {
		kind = "引擎任务"
	}
	metrics := []taskMetric{{"已观测", "缺测", ""}, {"速率", "缺测", ""}, {"总量", "总量未知", ""}}
	note := "归档获取接口未提供字节读数。"
	if task.Kind != "engine" {
		note = ""
		metrics = []taskMetric{
			{"已观测", fmt.Sprintf("%.2f MiB", float64(task.DownloadedBytes)/1048576), ""},
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
			kind: kind, name: task.OutName, state: stateLabel(task.State), color: color,
			active:  task.State == "downloading",
			metrics: metrics,
			note:    note,
			notice:  func() { downloadErrorView(c, task.Error) },
			actions: func() { s.downloadActions(c, task) },
			details: func() { downloadDetails(c, task) },
		})
	})
}

func (s *viewState) downloadActions(c *ui.Context, task core.DownloadTask) {
	if task.State == "downloading" {
		if s.destructiveAction(c, destructiveOption{
			kind: destructiveCancelDownload, id: task.TaskId, label: "取消下载", confirmLabel: "确认取消下载",
			prompt:   "将取消本次下载；已写入的输出目录保留，再次下载会使用新的目录名。",
			disabled: s.busy,
		}) {
			if s.simulate(previewCancelDownload{id: task.TaskId}) {
				return
			}
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.CancelDownload(task.TaskId)
				return "已请求取消下载。", e
			})
		}
	}
	if task.State != "failed" {
		return
	}
	button(c, "重新创建下载任务").Disabled(s.busy).OnClick(func() {
		if s.simulate(previewRetryDownload{id: task.TaskId}) {
			return
		}
		s.work(func() (string, *core.ApiError) {
			var e *core.ApiError
			if task.Kind == "engine" {
				_, e = s.service.CreateEngineDownload(task.SourceUrl, "")
			} else {
				_, e = s.service.CreateModelDownload(core.CreateDownloadRequest{SourceUrl: task.SourceUrl}, "")
			}
			return "已重新创建下载任务，使用新目录名。", e
		})
	})
}

// downloadDetails holds the raw values and output paths; the card keeps the
// readable sentence outside.
func downloadDetails(c *ui.Context, task core.DownloadTask) {
	ui.Text(c, "实际目录名："+task.OutName).Selectable()
	ui.Text(c, "文件："+displayName(text(task.FileName)))
	if task.Kind == "engine" {
		ui.Text(c, "已下载 缺测 · 速率 缺测")
	} else {
		ui.Text(c, fmt.Sprintf("已下载 %d bytes", task.DownloadedBytes))
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
		ui.Text(c, "输出目录保留；再次下载自动使用新目录名。")
	}
}

// downloadErrorView keeps the operator-facing sentence and the readable
// limitation text; the raw code stays in the folded details.
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
	if len(e.Details) > 0 {
		kvview(c, "kv-error-details", e.Details)
	}
}
