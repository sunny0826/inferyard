package pages

import "github.com/egoist/mygo/ui"

func componentFold(c *ui.Context, title string, fn func()) {
	container := ui.Column(c.Key(title))
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() { ui.Collapsible(c, title, open, fn) })
}

func statusBadge(c *ui.Context, label string, color ui.Color) {
	ui.Badge(c, label).TextColor(color).Background(color.Alpha(0.10)).AlignSelf(ui.Start)
}

func statusPanel(c *ui.Context, code, title, description string, color ui.Color) {
	ui.Column(c).Gap(8).Padding(16).Radius(10).Background(color.Mix(c.Theme().Surface, 0.93)).Children(func() {
		statusBadge(c, code, color)
		ui.Text(c, title).Bold().FontSize(20)
		ui.Text(c, description).TextColor(c.Theme().TextMuted)
	})
}

func kvGrid(c *ui.Context, rows [][2]string) {
	ui.Grid(c).Columns(2).Gap(10).Children(func() {
		for _, row := range rows {
			ui.Text(c, row[0]).TextColor(c.Theme().TextMuted)
			ui.Text(c, row[1]).Selectable()
		}
	})
}

func stepsRow(c *ui.Context, steps []string, current string, color ui.Color) {
	ui.Row(c).Wrap().Gap(4).Children(func() {
		for i, step := range steps {
			if i > 0 {
				ui.Text(c, "→").TextColor(c.Theme().TextMuted).FontSize(10)
			}
			e := ui.Text(c, step).FontSize(10).TextColor(c.Theme().TextMuted)
			if step == current {
				e.Bold().TextColor(color)
			}
		}
	})
}

type taskMetric struct {
	label, value, reason string
}

func taskCard(c *ui.Context, kind, name, state string, color ui.Color, active bool, metrics []taskMetric, actions, details func()) {
	ui.Column(c).Gap(12).Padding(15).Radius(10).Border(1, c.Theme().Border).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, kind).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Text(c, name).Bold().FontSize(16)
			})
			statusBadge(c, state, color)
		})
		ui.Grid(c).Columns(3).Gap(12).Children(func() {
			for _, metric := range metrics {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, metric.label).FontSize(12).TextColor(c.Theme().TextMuted)
					ui.Text(c, metric.value).Bold().FontSize(19)
					if metric.reason != "" {
						ui.Text(c, metric.reason).FontSize(12).TextColor(c.Theme().TextMuted)
					}
				})
			}
		})
		if active {
			ui.Box(c.Key("task-activity")).Label("下载任务活动").Height(4).Radius(4).Background(color.Alpha(0.08)).Stripes(color.Alpha(0.35), 18, 14, 100)
		}
		actions()
		componentFold(c, "观测依据与输出", details)
	})
}
