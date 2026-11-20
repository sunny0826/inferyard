package pages

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func card(c *ui.Context, title string, fn func()) {
	t := tokens(c)
	ui.Column(c).MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding).Background(c.Theme().Surface).Radius(t.Radius.Surface).Border(t.Card.Edge, c.Theme().Border).Children(func() { heading(c, title, 2); fn() })
}

func statusBadge(c *ui.Context, label string, color ui.Color) ui.Element {
	t := c.Theme()
	background, foreground := badgeSurfaceColors(t, color)
	badge := components.Badge(c, label).TextColor(foreground).Background(background)
	if c.Preferences().HighContrast {
		badge.Background(t.Surface).Border(1, color)
	}
	return badge
}

func badgeSurfaceColors(t *ui.Theme, tint ui.Color) (background, foreground ui.Color) {
	return t.Surface.Mix(tint, .12), tint.Mix(t.Text, .3)
}

type phaseProgressOptions struct {
	Labels      []string
	Current     int
	RowLabel    string
	BarLabel    string
	Description string
}

func phaseProgress(c *ui.Context, o phaseProgressOptions) {
	t := tokens(c)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Label(o.RowLabel).Children(func() {
		for i, label := range o.Labels {
			switch {
			case i < o.Current:
				ui.Text(c, "✓  "+label).TextColor(stateColor(c, stateSuccess))
			case i == o.Current:
				statusBadge(c, label, stateColor(c, stateActive)).Description("当前阶段：" + label)
			default:
				ui.Text(c, label).TextColor(stateColor(c, stateNeutral))
			}
		}
	})
	value := float64(0)
	if len(o.Labels) > 0 {
		value = float64(o.Current) / float64(len(o.Labels))
	}
	progressBar(c, value, o.BarLabel).Description(o.Description)
}

func metricCard(c *ui.Context, label, value, detail, note string) {
	t := tokens(c)
	ui.Column(c).MinWidth(0).Gap(t.Metric.Gap).Padding(t.Metric.PaddingY, t.Metric.PaddingX).
		Background(c.Theme().Surface).Radius(t.Radius.Surface).Border(t.Card.Edge, c.Theme().Border).Children(func() {
		ui.Text(c, label)
		ui.Text(c, value).Bold().FontSize(t.Type.MetricLarge).Selectable()
		ui.Text(c, detail).TextColor(c.Theme().TextMuted)
		if note != "" {
			ui.Text(c, note).TextColor(c.Theme().TextMuted)
		}
	})
}

type pairedBarValue struct {
	Label string
	Value float64
	Text  string
}

func pairedBar(c *ui.Context, title string, a, b pairedBarValue) {
	t := tokens(c)
	ui.Column(c).Gap(t.Density.Tight).Label(title).Children(func() {
		ui.Text(c, title)
		pairedBarRow(c, sideA, a)
		pairedBarRow(c, sideB, b)
	})
}

func pairedBarRow(c *ui.Context, side sideKind, value pairedBarValue) {
	t := tokens(c)
	fraction := value.Value
	if math.IsNaN(fraction) || math.IsInf(fraction, 0) {
		fraction = 0
	}
	fraction = max(0, min(1, fraction))
	ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Label(value.Label).Children(func() {
		ui.Text(c, value.Label).Width(t.Density.Page * 2)
		ui.Box(c).Grow(1).Height(8).Radius(4).Background(c.Theme().Border).Children(func() {
			ui.Box(c).WidthPercent(float32(fraction) * 100).Height(8).Radius(4).Background(sideColor(c, side))
		})
		ui.Text(c, value.Text).TextColor(stateColor(c, stateNeutral))
	})
}

type taskMetric struct {
	label, value, reason string
}

type taskCardOptions struct {
	kind, name, state string
	color             ui.Color
	active            bool
	metrics           []taskMetric
	status            string
	notice            func()
	actions, details  func()
}

func taskCard(c *ui.Context, options taskCardOptions) {
	t := tokens(c)
	ui.Column(c).Label(options.name).Gap(t.Density.Stack).Padding(t.Card.Padding).Radius(t.Radius.Inset).Border(t.Card.Border, c.Theme().Border).Children(func() {
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, options.name).Bold().FontSize(t.Type.Section)
				if options.kind != "" {
					ui.Text(c, options.kind).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
				}
			})
			statusBadge(c, options.state, options.color).Tooltip(map[string]string{
				"下载中": "任务正在下载，总量未知。",
				"已结束": "下载任务已结束。",
				"失败":  "下载任务失败，输出目录保留。",
				"已取消": "下载任务已取消，输出目录保留。",
			}[options.state])
		})
		if options.status != "" {
			ui.Text(c, options.status).TextColor(c.Theme().TextMuted)
		} else {
			columns := max(1, min(3, len(options.metrics)))
			width, _ := c.Size()
			if width < 900 || c.Preferences().TextScale > 1 {
				columns = 1
			}
			ui.Grid(c).Columns(columns).Gap(t.Density.Stack).Children(func() {
				for _, metric := range options.metrics {
					ui.Column(c).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, metric.label).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted).Tooltip(metric.reason)
						ui.Text(c, metric.value).Bold().FontSize(t.Type.Metric)
						if metric.reason != "" {
							ui.Text(c, metric.reason).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
						}
					})
				}
			})
		}
		if options.active {
			progressBar(c, -1, "下载任务活动").Height(t.Density.Tight)
		}
		if options.notice != nil {
			options.notice()
		}
		options.actions()
		componentFold(c, "观测依据与输出", func() { options.details() })
	})
}

func stepTrack(c *ui.Context, number, title string, fn func()) {
	t := tokens(c)
	theme := c.Theme()
	size := max(theme.Space(7), theme.Rem(1.6))
	ui.Row(c).AlignItems(ui.Stretch).Gap(t.Density.Section).Label("阶段步骤 " + number).Children(func() {
		ui.Column(c).MinWidth(size).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Box(c).Label("阶段编号 "+number).Size(size, size).Radius(size / 2).Background(stateColor(c, stateActive)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, number).Bold().TextColor(theme.Surface)
			})
			ui.Box(c).Width(theme.Space(.5)).Grow(1).Background(theme.Border)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.CardGap).Children(func() {
			ui.Row(c).MinHeight(size).AlignItems(ui.Center).Children(func() {
				heading(c, title, 2)
			})
			fn()
		})
	})
}

func selectField[T comparable](c *ui.Context, name, description string, selected *T, options []components.NativeSelectOption[T]) {
	f := ui.Field(c, name, func() {
		current := ""
		for _, option := range options {
			if option.Value == *selected {
				current = option.Label
				break
			}
		}
		components.NativeSelect(c, selected, options).Trigger.Label(name).Value(current)
	})
	if description != "" {
		f.Description(description)
	}
}
