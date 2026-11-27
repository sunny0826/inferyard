package pages

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	base "github.com/egoist/mygo/ui/chart"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
)

type resourceHoverGroupState struct {
	hoverChartID  uint64
	hoverSince    time.Time
	pointerActive bool
	lastActiveAt  time.Time
	anyOverInPass bool
}

type resourcePlotLocal struct {
	px, py    float32
	wasOver   bool
	byPointer bool
	dismissed bool
	last      int
}

type resourcePlotOptions struct {
	id          string
	label       string
	description string
	height      float32
	detail      func(*ui.Context, int)
}

func resourcePlotPrepare(data chart.Data, state *chart.State) ([]int, [][]float64, base.Linear, bool) {
	var seriesIdx []int
	var allValues [][]float64
	lo, hi := 0.0, 0.0
	valid := false
	for j, s := range data.Series {
		if state != nil && state.Hidden[s.ID] {
			continue
		}
		values := make([]float64, len(data.Labels))
		for i := range values {
			if i >= len(s.Values) || math.IsNaN(s.Values[i]) || math.IsInf(s.Values[i], 0) {
				values[i] = math.NaN()
				continue
			}
			v := s.Values[i]
			values[i], valid = v, true
			lo, hi = min(lo, v), max(hi, v)
		}
		seriesIdx = append(seriesIdx, j)
		allValues = append(allValues, values)
	}
	if lo == hi {
		hi = lo + 1
	}
	y := base.NewLinear(lo, hi).Nice(4)
	if math.IsNaN(y.Lo) || math.IsInf(y.Lo, 0) || math.IsNaN(y.Hi) || math.IsInf(y.Hi, 0) || math.IsInf(y.Hi-y.Lo, 0) {
		valid = false
	}
	return seriesIdx, allValues, y, valid
}

func resourceDescribePoint(data chart.Data, seriesIdx []int, idx int) string {
	if idx < 0 || idx >= len(data.Labels) {
		return ""
	}
	parts := []string{data.Labels[idx]}
	for _, j := range seriesIdx {
		s := data.Series[j]
		if idx < len(s.Values) && !math.IsNaN(s.Values[idx]) && !math.IsInf(s.Values[idx], 0) {
			parts = append(parts, s.Label+": "+strconv.FormatFloat(s.Values[idx], 'g', 5, 64))
		}
	}
	return strings.Join(parts, "; ")
}

func resourceLinePlot(c *ui.Context, grp *resourceHoverGroupState, state *chart.State, data chart.Data, opts resourcePlotOptions) ui.Element {
	e := ui.Column(c).FillWidth().MinWidth(0).Gap(c.Theme().Space(3))
	if state == nil {
		state = ui.Local(e, "resource-plot-state", func() chart.State { return chart.State{Index: -1} })
	}
	h := opts.height
	if h <= 0 {
		h = 140
	}
	e.Children(func() {
		seriesIdx, allValues, y, valid := resourcePlotPrepare(data, state)
		if state.Index < -1 || state.Index >= len(data.Labels) || !valid {
			state.Index = -1
		}
		if !valid {
			ui.Column(c).FillWidth().Height(h).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				components.Text(c, "—").TextColor(c.Theme().TextMuted)
			})
			return
		}
		renderResourcePlotBody(c, grp, state, data, opts, h, seriesIdx, allValues, y)
	})
	return e
}

func renderResourcePlotBody(
	c *ui.Context,
	grp *resourceHoverGroupState,
	state *chart.State,
	data chart.Data,
	opts resourcePlotOptions,
	plotH float32,
	seriesIdx []int,
	allValues [][]float64,
	y base.Linear,
) {
	theme := c.Theme()
	tok := tokens(c)
	coord := syncHoverCoordinator(c)
	now := frameNow(c)

	x := base.NewBand(data.Labels...).Padding(.3, .3)
	xs := make([]float32, len(data.Labels))
	for i := range xs {
		xs[i] = x.At(i)
	}
	holder := ui.Column(c).FillWidth().MinWidth(0).Height(plotH).Padding(8, 8, 0, 0)
	width := holder.LayoutBounds().W
	if width <= 0 {
		width = 240
	}
	axisSize := theme.FontSize - 2
	tickWidth := theme.Space(11)
	for _, label := range data.Labels {
		w, _ := c.MeasureText(0, ui.Span{Text: label, Size: axisSize})
		tickWidth = max(tickWidth, min(theme.Space(21), w+theme.Space(3)))
	}
	limit := max(1, int((width-48)/tickWidth))
	step := max(1, int(math.Ceil(float64(len(xs))/float64(limit))))
	var ticks []base.Tick
	for i := 0; i < len(xs); i += step {
		ticks = append(ticks, base.Tick{At: xs[i], Label: data.Labels[i]})
	}
	axes := base.Axes{Left: y.Ticks(4), Bottom: ticks, Label: func(side base.Side, tick base.Tick) {
		lbl := components.Text(c, tick.Label).NoWrap().FontSize(axisSize).TextColor(theme.TextMuted)
		if side == base.Bottom {
			lbl.MaxWidth(max(24, tickWidth-8)).Ellipsis("…")
		}
	}}

	holder.Children(func() {
		base.Plot(c, axes, func(area ui.Element) {
			area.Label(opts.label).Description(opts.description).Focusable()
			st := ui.Local(area, "resource-plot-sel", func() resourcePlotLocal { return resourcePlotLocal{last: -1} })
			n := len(xs)
			positions := xs
			if aw, raw := area.Bounds().W, area.LayoutBounds().W; aw > 0 && raw > 0 && aw != raw {
				positions = make([]float32, n)
				for i, xv := range xs {
					positions[i] = xv * raw / aw
				}
			}

			announce := false
			moveKey := func(i int) {
				state.Index = max(0, min(n-1, i))
				st.byPointer = false
				st.dismissed = false
				announce = true
			}
			switch {
			case n == 0:
			case area.Shortcut(0, ui.KeyRight):
				if state.Index < 0 {
					moveKey(0)
				} else {
					moveKey(state.Index + 1)
				}
			case area.Shortcut(0, ui.KeyLeft):
				if state.Index < 0 {
					moveKey(n - 1)
				} else {
					moveKey(state.Index - 1)
				}
			case area.Shortcut(0, ui.KeyHome):
				moveKey(0)
			case area.Shortcut(0, ui.KeyEnd):
				moveKey(n - 1)
			case state.Index >= 0 && area.Shortcut(0, ui.KeyEscape):
				state.Index = -1
				st.byPointer = false
				st.dismissed = true
			}

			px, py, over := area.PointerPosition()
			clicked := !area.IsDisabled() && (area.Pressed() || area.Clicked())
			if clicked && n > 0 {
				if aw := area.Bounds().W; aw > 0 {
					state.Index = base.Nearest(positions, px/aw)
				} else {
					state.Index = 0
				}
				st.byPointer = false
				st.dismissed = false
				if grp != nil {
					grp.pointerActive = true
					grp.lastActiveAt = now
				}
			}

			if over && !area.IsDisabled() && n > 0 {
				if grp != nil {
					grp.anyOverInPass = true
				}
				moved := !st.wasOver || px != st.px || py != st.py
				if moved {
					st.dismissed = false
				}
				id := area.ID()
				if grp != nil && grp.hoverChartID != id {
					grp.hoverChartID = id
					grp.hoverSince = now
				}
				if grp != nil && !st.dismissed && !clicked {
					warm := grp.pointerActive || (!grp.lastActiveAt.IsZero() && now.Sub(grp.lastActiveAt) <= labelWarmWindow)
					if warm {
						grp.pointerActive = true
						grp.lastActiveAt = now
					} else if wait := labelOpenDelay - now.Sub(grp.hoverSince); wait > 0 {
						c.After(wait)
					} else if !coord.isFast(c, now) {
						grp.pointerActive = true
						grp.lastActiveAt = now
					}
					if grp.pointerActive && !announce {
						if aw := area.Bounds().W; aw > 0 && (!st.byPointer || moved || state.Index < 0) {
							state.Index = base.Nearest(positions, px/aw)
							st.byPointer = true
						}
						grp.lastActiveAt = now
					}
				}
				st.wasOver, st.px, st.py = true, px, py
			} else if st.wasOver {
				st.wasOver = false
				st.dismissed = false
				if st.byPointer {
					state.Index = -1
					st.byPointer = false
				}
			}

			selected := state.Index
			if selected >= 0 {
				desc := resourceDescribePoint(data, seriesIdx, selected)
				area.Value(desc)
				if announce && selected != st.last {
					c.Announce(desc)
				}
			}
			st.last = selected

			area.Draw(func(painter *ui.Painter, r ui.Rect) {
				for _, tick := range axes.Left {
					shade := theme.Border.Alpha(.45)
					if tick.Value == 0 {
						shade = theme.Border.Alpha(.8)
					}
					painter.Line(r.X, base.Y(r, tick.At), r.X+r.W, base.Y(r, tick.At), 1, shade)
				}
				if selected >= 0 {
					w := max(12, r.W/float32(len(xs))*.8)
					painter.Fill(ui.Rect{X: base.X(r, xs[selected]) - w/2, Y: r.Y, W: w, H: r.H}, theme.Text.Alpha(.025), 6)
				}
			})

			for k, j := range seriesIdx {
				s := data.Series[j]
				shade := s.Color
				if shade == (ui.Color{}) {
					shade = theme.Accent
				}
				vals := allValues[k]
				ui.Box(c).Key(s.ID).Absolute().Left(0).Right(0).Top(0).Bottom(0).Children(func() {
					ys := make([]float32, len(vals))
					for i, v := range vals {
						ys[i] = y.At(v)
					}
					ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Draw(func(p *ui.Painter, r ui.Rect) {
						p.StrokePath(base.Line(r, xs, ys, base.CurveMonotone), 2, shade)
					})
					for i, v := range vals {
						if math.IsNaN(v) || math.IsInf(v, 0) || (len(xs) > 32 && i != selected) {
							continue
						}
						if i == selected {
							base.Point(c, xs[i], ys[i], 16, 16).Radius(8).Background(shade.Alpha(.16))
						}
						base.Point(c, xs[i], ys[i], 6, 6).Radius(3).Background(shade)
					}
				})
			}

			if selected >= 0 {
				base.RuleX(c, xs[selected], 1).Background(theme.TextMuted.Alpha(.3))
			}

			pointerReady := grp != nil && grp.pointerActive
			showTip := selected >= 0 && !st.dismissed &&
				((st.byPointer && over && pointerReady) || (!st.byPointer && area.Focused()))
			if !showTip {
				return
			}
			var anchor ui.Element
			if st.byPointer && over {
				anchor = ui.Box(c).Absolute().Left(px).Top(py).Size(0, 0).PassThrough()
			} else {
				anchor = base.RuleX(c, xs[selected], 0).PassThrough()
			}
			reduced := c.Preferences().ReduceMotion
			ui.Overlay(c, func() {
				ui.Column(c.Key("resource-chart-tip-"+opts.id)).Absolute().PassThrough().Role(ui.RoleTooltip).
					AttachTo(anchor, ui.AnchorEnd, ui.AnchorStart).
					AttachEdgeMargin(tok.Density.Row).
					MarginStart(theme.Space(3)).
					MinWidth(theme.Space(30)).MaxWidth(theme.Space(80)).
					Padding(theme.Space(2), theme.Space(2.5)).Gap(theme.Space(1)).
					Radius(tok.Radius.Floating).
					Background(tok.Color.Elevated).Border(1, tok.Color.BorderDefault).
					TextColor(tok.Color.FgPrimary).
					Shadow(0, theme.Space(1), theme.Space(4), 0, ui.RGBA(0, 0, 0, .14)).
					Transition(ui.ElementTransition{
						Duration:     labelEnterDuration,
						ExitDuration: labelExitDuration,
						Position:     !reduced,
						Colors:       reduced,
						Enter:        &ui.Motion{Opacity: 0},
						Exit:         &ui.Motion{Opacity: 0},
					}).Children(func() {
					if opts.detail != nil {
						opts.detail(c, selected)
					}
				})
			})
		}).Fill().MinWidth(0).Gap(10)
	})
}
