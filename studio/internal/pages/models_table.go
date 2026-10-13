package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsView(c *ui.Context) {
	ui.Text(c, "1 MiB = 1,048,576 bytes；速率为 bytes/s，总量未知。")
	if len(s.downloads) == 0 {
		ui.Text(c, "暂无下载任务。")
	}
	selectableTable(&s.downloadsTable, func(i int) any { return s.downloads[i].TaskId }, func(i int) string { return s.downloads[i].OutName })
	c.Key("downloads-table")
	ui.Table(c, &s.downloadsTable, []ui.TableColumn{{Title: "名称", Width: 240}, {Title: "状态", Width: 120}, {Title: "已下载 MiB", Width: 160}, {Title: "速率 bytes/s", Width: 180}}, len(s.downloads), func(row, col int) {
		task := s.downloads[row]
		ui.Text(c, []string{task.OutName, stateLabel(task.State), fmt.Sprintf("%.2f", float64(task.DownloadedBytes)/1048576), number(task.BytesPerSecond)}[col])
	}).Height(220)
	index := selectedRow(&s.downloadsTable, len(s.downloads))
	if index < 0 {
		return
	}
	task := s.downloads[index]
	ui.Text(c, "输出目录："+task.OutDir).Selectable()
	ui.Text(c, "文件："+text(task.FileName))
	ui.Text(c, fmt.Sprintf("已下载 %d bytes", task.DownloadedBytes))
	if task.Result != nil {
		ui.Text(c, fmt.Sprintf("实测大小：%d bytes · SHA-256：%s", task.Result.Bytes, task.Result.Sha256)).Selectable()
	}
	errorView(c, task.Error)
	if task.State == "failed" || task.State == "cancelled" {
		ui.Text(c, "输出目录保留；再次下载请更换目录名。")
	}
	if task.State == "downloading" {
		ui.Button(c, "取消下载").Disabled(s.busy).OnClick(func() {
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.CancelDownload(task.TaskId)
				return "已请求取消下载。", e
			})
		})
	}
}
