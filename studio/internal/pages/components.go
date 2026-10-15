package pages

import "github.com/egoist/mygo/ui"

func statusPanel(c *ui.Context, code, title, description string, color ui.Color) {
	panel := ui.Column(c).Gap(c.Theme().Space(2)).Padding(c.Theme().Space(4.5)).Radius(c.Theme().Space(3)).Background(color.Mix(c.Theme().Surface, 0.93))
	if c.Preferences().HighContrast {
		panel.Background(c.Theme().Surface).Border(1, color)
	}
	panel.Children(func() {
		statusBadge(c, code, color)
		heading(c, title, 3).FontSize(c.Theme().Rem(1.4))
		ui.Text(c, description).TextColor(c.Theme().TextMuted)
	})
}

func kvGrid(c *ui.Context, rows [][2]string, tooltips map[string]string) {
	columns := 2
	if compactLayout(c) {
		columns = 1
	}
	ui.Grid(c).Columns(columns).Gap(c.Theme().Space(2.5)).Children(func() {
		for _, row := range rows {
			ui.Text(c, row[0]).TextColor(c.Theme().TextMuted).Tooltip(tooltips[row[0]])
			ui.Text(c, row[1]).Selectable().Label(row[0] + "：" + row[1])
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
	ui.Column(c).Gap(c.Theme().Space(3)).Padding(c.Theme().Space(4.5)).Radius(c.Theme().Space(3)).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Wrap().Gap(c.Theme().Space(2.5)).Children(func() {
			ui.Column(c).Grow(1).Gap(c.Theme().Space(1)).Children(func() {
				ui.Text(c, options.kind).FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
				ui.Text(c, options.name).Bold().FontSize(c.Theme().Rem(1.1))
			})
			if options.active {
				activitySpinner(c, "下载中")
			}
			statusBadge(c, options.state, options.color).Tooltip(map[string]string{
				"下载中": "任务正在下载，总量未知。",
				"已结束": "下载任务已结束。",
				"失败":  "下载任务失败，输出目录保留。",
				"已取消": "下载任务已取消，输出目录保留。",
			}[options.state])
		})
		columns := 3
		width, _ := c.Size()
		if width < 900 || c.Preferences().TextScale > 1 {
			columns = 1
		}
		ui.Grid(c).Columns(columns).Gap(c.Theme().Space(3)).Children(func() {
			for _, metric := range options.metrics {
				ui.Column(c).Gap(c.Theme().Space(1)).Children(func() {
					ui.Text(c, metric.label).FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted).Tooltip(metric.reason)
					ui.Text(c, metric.value).Bold().FontSize(c.Theme().Rem(1.35))
					if metric.reason != "" {
						ui.Text(c, metric.reason).FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
					}
				})
			}
		})
		if options.active {
			progressBar(c, -1, "下载任务活动").Height(4)
		}
		options.actions()
		ui.Text(c, "观测依据与输出").FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
		options.details()
	})
}

// stepTrack places a numbered rail beside content on the page background.
func stepTrack(c *ui.Context, number, title string, fn func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Stretch).Gap(t.Space(4)).Label("阶段步骤 " + number).Children(func() {
		ui.Column(c).Width(t.Space(7)).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
			ui.Box(c).Width(t.Space(7)).Height(t.Space(7)).Radius(t.Space(3.5)).Background(t.Accent).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, number).Bold().TextColor(t.Surface)
			})
			ui.Box(c).Width(t.Space(.5)).Grow(1).Background(t.Border)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(2.5)).Children(func() {
			heading(c, title, 2)
			fn()
		})
	})
}
