package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsView(c *ui.Context) {
	ui.Text(c, "测量单位").Bold()
	ui.Text(c, "1 MiB = 1,048,576 bytes；速率为 bytes/s，总量未知。")
	ui.Text(c, "内存、文件大小以 bytes 存储，速率以 bytes/s 存储。展示换算：1 GiB = 1,073,741,824 bytes。总量未知不显示百分比或剩余时间；活动条仅表示任务活动。")
	if len(s.downloads) == 0 {
		ui.Text(c, "暂无下载任务。")
	}
	for _, task := range s.downloads {
		kind, amount, rate := "模型任务", fmt.Sprintf("%.2f MiB", float64(task.DownloadedBytes)/1048576), "缺测"
		if task.BytesPerSecond != nil {
			rate = formatRate(*task.BytesPerSecond)
		}
		reason := ""
		if task.Kind == "engine" {
			kind, amount, rate = "引擎任务", "null", "null"
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
					{"已观测", amount, reason}, {"速率", rate, rateReason}, {"总量", "null", "总量未知"},
				}, actions: func() {
					if task.State == "downloading" {
						ui.Button(c, "取消下载").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
							s.work(func() (string, *core.ApiError) {
								_, e := s.service.CancelDownload(task.TaskId)
								return "已请求取消下载。", e
							})
						})
					}
				}, details: func() {
					ui.Text(c, "输出目录："+task.OutDir).Selectable()
					ui.Text(c, "文件："+text(task.FileName))
					if task.Kind == "engine" {
						ui.Text(c, "已下载 null · 速率 null · 归档获取接口未提供字节读数")
						if task.State == "done" && task.EnginePath != nil {
							ui.Text(c, "引擎路径："+*task.EnginePath).Selectable()
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
						ui.Text(c, "输出目录保留；再次下载请更换目录名。")
					}
				},
			})
		})
	}
}
