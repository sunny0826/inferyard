package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"slices"
)

func (s *viewState) downloadsView(c *ui.Context) {
	ui.Text(c, "测量单位").Bold()
	ui.Text(c, "1 MiB = 1,048,576 bytes；速率为 bytes/s，总量未知。")
	ui.Text(c, "内存、文件大小以 bytes 存储，速率以 bytes/s 存储。展示换算：1 GiB = 1,073,741,824 bytes。总量未知不显示百分比或剩余时间；活动条仅表示任务活动。")
	if len(s.downloads) == 0 {
		ui.Text(c, "暂无下载任务。")
	}
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
		kind, amount, rate := "模型任务", fmt.Sprintf("%.2f MiB", float64(task.DownloadedBytes)/1048576), "缺测"
		if task.BytesPerSecond != nil {
			rate = formatRate(*task.BytesPerSecond)
		}
		reason := ""
		if task.Kind == "engine" {
			kind, amount, rate = "引擎任务", "缺测", "缺测"
			reason = "归档获取接口未提供字节读数"
		}
		rateReason := reason
		if task.Kind != "engine" && task.BytesPerSecond == nil {
			rateReason = "无速率读数"
		}
		color := c.Theme().TextMuted
		switch task.State {
		case "done":
			color = c.Theme().Success
		case "failed":
			color = c.Theme().Danger
		case "downloading":
			color = c.Theme().Accent
		}
		ui.Column(c.Key(task.TaskId)).Children(func() {
			taskCard(c, taskCardOptions{
				kind: kind, name: task.OutName, state: stateLabel(task.State), color: color,
				active: task.State == "downloading",
				metrics: []taskMetric{
					{"已观测", amount, reason}, {"速率", rate, rateReason}, {"总量", "总量未知", "总量未知"},
				}, actions: func() {
					if task.State == "downloading" {
						button(c, "取消下载").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
							if s.simulate(previewCancelDownload{id: task.TaskId}) {
								return
							}
							s.work(func() (string, *core.ApiError) {
								_, e := s.service.CancelDownload(task.TaskId)
								return "已请求取消下载。", e
							})
						})
					}
					if task.State == "failed" {
						button(c, "重新创建下载任务").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
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
				}, details: func() {
					ui.Text(c, "实际目录名："+task.OutName).Selectable()
					ui.Text(c, "文件："+displayName(text(task.FileName)))
					if task.Kind == "engine" {
						ui.Text(c, "已下载 缺测 · 速率 缺测 · 归档获取接口未提供字节读数")
						if task.State == "done" && task.EnginePath != nil {
							ui.Text(c, "引擎："+displayName(*task.EnginePath)).Selectable()
						}
					} else {
						ui.Text(c, fmt.Sprintf("已下载 %d bytes", task.DownloadedBytes))
						if task.BytesPerSecond != nil {
							ui.Text(c, fmt.Sprintf("速率 %s bytes/s", groupedInt(int64(*task.BytesPerSecond))))
						}
					}
					if task.Result != nil {
						ui.Text(c, fmt.Sprintf("实测大小：%d bytes · SHA-256：%s", task.Result.Bytes, task.Result.Sha256)).Selectable()
					}
					errorView(c, task.Error)
					if task.State == "failed" || task.State == "cancelled" {
						ui.Text(c, "输出目录保留；再次下载自动使用新目录名。")
					}
				},
			})
		})
	}
}
