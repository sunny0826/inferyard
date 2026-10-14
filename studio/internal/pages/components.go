package pages

import "github.com/egoist/mygo/ui"

const (
	panelRadius  = 12
	panelPadding = 18
)

func statusPanel(c *ui.Context, code, title, description string, color ui.Color) {
	ui.Column(c).Gap(8).Padding(panelPadding).Radius(panelRadius).Background(color.Mix(c.Theme().Surface, 0.93)).Children(func() {
		statusBadge(c, code, color)
		ui.Text(c, title).Bold().FontSize(20)
		ui.Text(c, description).TextColor(c.Theme().TextMuted)
	})
}

func kvGrid(c *ui.Context, rows [][2]string, tooltips map[string]string) {
	ui.Grid(c).Columns(2).Gap(10).Children(func() {
		for _, row := range rows {
			ui.Text(c, row[0]).TextColor(c.Theme().TextMuted).Tooltip(tooltips[row[0]])
			ui.Text(c, row[1]).Selectable().Label(row[0])
		}
	})
}

func stepsRow(c *ui.Context, steps []string, current int, color ui.Color) {
	ui.Row(c).Wrap().Gap(4).Label("引擎服务状态流转").Children(func() {
		for i, step := range steps {
			if i > 0 {
				ui.Text(c, "→").TextColor(c.Theme().TextMuted).FontSize(10)
			}
			e := ui.Text(c, step).FontSize(10).TextColor(c.Theme().TextMuted)
			if i == current {
				e.Bold().TextColor(color)
			}
		}
	})
}

type taskMetric struct {
	label, value, reason string
}

type taskCardOptions struct {
	kind, name, state string
	color             ui.Color // 与 state 对应的状态色。
	active            bool
	metrics           []taskMetric
	actions, details  func()
}

func taskCard(c *ui.Context, options taskCardOptions) {
	ui.Column(c).Gap(12).Padding(panelPadding).Radius(panelRadius).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, options.kind).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, options.name).Bold().FontSize(16)
			})
			if options.active {
				ui.Spinner(c).Label("下载中")
			}
			statusBadge(c, options.state, options.color).Tooltip(map[string]string{
				"下载中": "任务正在下载，总量未知。",
				"已结束": "下载任务已结束。",
				"失败":  "下载任务失败，输出目录保留。",
				"已取消": "下载任务已取消，输出目录保留。",
			}[options.state])
		})
		ui.Grid(c).Columns(3).Gap(12).Children(func() {
			for _, metric := range options.metrics {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, metric.label).FontSize(12).TextColor(c.Theme().TextMuted).Tooltip(metric.reason)
					ui.Text(c, metric.value).Bold().FontSize(19)
					if metric.reason != "" {
						ui.Text(c, metric.reason).FontSize(12).TextColor(c.Theme().TextMuted)
					}
				})
			}
		})
		if options.active {
			ui.Progress(c, -1).Label("下载任务活动").Height(4)
		}
		options.actions()
		ui.Text(c, "观测依据与输出").FontSize(12).TextColor(c.Theme().TextMuted)
		options.details()
	})
}
