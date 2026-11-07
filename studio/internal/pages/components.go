package pages

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// card is the shared surface card: a second-level heading with related content
// and nearby actions. Edge adds the optional hairline a scheme may define.
func card(c *ui.Context, title string, fn func()) {
	t := tokens(c)
	ui.Column(c).MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding).Background(c.Theme().Surface).Radius(t.Radius.Surface).Border(t.Card.Edge, c.Theme().Border).Children(func() { heading(c, title, 2); fn() })
}

// statusBadge renders a state label; the color comes from the semantic state
// tokens at the call site.
func statusBadge(c *ui.Context, label string, color ui.Color) ui.Element {
	badge := components.Badge(c, label).TextColor(color).Background(color.Alpha(0.10))
	if c.Preferences().HighContrast {
		badge.Background(c.Theme().Surface).Border(1, color)
	}
	return badge
}

// phaseProgressOptions describes a labeled phase sequence.
type phaseProgressOptions struct {
	Labels      []string
	Current     int
	RowLabel    string
	BarLabel    string
	Description string
}

// phaseProgress renders a labeled phase sequence with one current phase and a
// bounded progress bar. It never expresses sample completion.
func phaseProgress(c *ui.Context, o phaseProgressOptions) {
	t := tokens(c)
	ui.Row(c).Wrap().Gap(t.Density.Row).Label(o.RowLabel).Children(func() {
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

// metricCard shows one report metric with its value, source detail and note.
func metricCard(c *ui.Context, label, value, detail, note string) {
	t := tokens(c)
	ui.Column(c).MinWidth(0).Gap(t.Metric.Gap).Padding(t.Metric.PaddingY, t.Metric.PaddingX).
		Background(c.Theme().Surface).Radius(t.Radius.Surface).Children(func() {
		ui.Text(c, label)
		ui.Text(c, value).Bold().FontSize(t.Type.MetricLarge).Selectable()
		ui.Text(c, detail).TextColor(c.Theme().TextMuted)
		if note != "" {
			ui.Text(c, note).TextColor(c.Theme().TextMuted)
		}
	})
}

// pairedBarValue is one side of an A/B metric comparison. Value is a 0–1
// fraction and Text is the displayed measurement.
type pairedBarValue struct {
	Label string
	Value float64
	Text  string
}

// pairedBar renders both sides of one metric. Pages do not use it yet; the
// experiment comparison view renders through it.
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
	color             ui.Color // 与 state 对应的状态色。
	active            bool
	metrics           []taskMetric
	actions, details  func()
}

func taskCard(c *ui.Context, options taskCardOptions) {
	t := tokens(c)
	ui.Column(c).Gap(t.Density.Stack).Padding(t.Card.Padding).Radius(t.Radius.Surface).Border(t.Card.Border, c.Theme().Border).Children(func() {
		ui.Row(c).Wrap().Gap(t.Density.CardGap).Children(func() {
			ui.Column(c).Grow(1).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, options.kind).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
				ui.Text(c, options.name).Bold().FontSize(t.Type.Section)
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
		if options.active {
			progressBar(c, -1, "下载任务活动").Height(t.Density.Tight)
		}
		options.actions()
		ui.Text(c, "观测依据与输出").FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
		options.details()
	})
}

// stepTrack places a numbered rail beside content on the page background.
func stepTrack(c *ui.Context, number, title string, fn func()) {
	t := tokens(c)
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Stretch).Gap(t.Density.Section).Label("阶段步骤 " + number).Children(func() {
		ui.Column(c).Width(theme.Space(7)).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Box(c).Width(theme.Space(7)).Height(theme.Space(7)).Radius(theme.Space(3.5)).Background(stateColor(c, stateActive)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, number).Bold().TextColor(theme.Surface)
			})
			ui.Box(c).Width(theme.Space(.5)).Grow(1).Background(theme.Border)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.CardGap).Children(func() {
			heading(c, title, 2)
			fn()
		})
	})
}
