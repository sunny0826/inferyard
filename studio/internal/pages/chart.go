package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
	"inferyard/studio/internal/core"
	"math"
	"sort"
	"time"
)

type resourcePoint struct {
	utc     string
	seconds float64
	values  [4]*float64
	sample  core.ResourceSample
}

func resourcePoints(samples []core.ResourceSample) []resourcePoint {
	byTime := map[string]resourcePoint{}
	for _, sample := range samples {
		date, e := time.Parse(time.RFC3339Nano, sample.Utc)
		if e != nil {
			continue
		}
		p := resourcePoint{utc: sample.Utc, seconds: float64(date.Unix()) + float64(date.Nanosecond())/1e9, sample: sample}
		if sample.MemAvailableBytes != nil {
			v := float64(*sample.MemAvailableBytes) / 1073741824
			p.values[0] = &v
		}
		if sample.ServiceRssBytes != nil {
			v := float64(*sample.ServiceRssBytes) / 1073741824
			p.values[1] = &v
		}
		p.values[2] = sample.ServiceCpuPercent
		p.values[3] = sample.TemperatureCelsius
		byTime[sample.Utc] = p
	}
	out := make([]resourcePoint, 0, len(byTime))
	for _, p := range byTime {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seconds < out[j].seconds })
	if len(out) > 600 {
		out = out[len(out)-600:]
	}
	return out
}
func resourceScale(points []resourcePoint, columns ...int) chart.Linear {
	lo, hi := 0.0, 0.0
	for _, p := range points {
		for _, column := range columns {
			v := p.values[column]
			if v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
				lo = math.Min(lo, *v)
				hi = math.Max(hi, *v)
			}
		}
	}
	if lo == hi {
		hi = lo + 1
	}
	return chart.NewLinear(lo, hi).Nice(4)
}
func finite(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func (s *viewState) resourceView(c *ui.Context, samples []core.ResourceSample) {
	card(c, "资源曲线", func() {
		ui.Text(c, "内存由 bytes 换算为 GiB（1 GiB = 1,073,741,824 bytes）。CPU 为服务端 ticks 差分百分比；温度为 °C，独立刻度。缺测保留断点。")
		points := resourcePoints(samples)
		ui.Text(c, fmt.Sprintf("采样点：%d", len(points)))
		if len(points) == 0 {
			ui.Text(c, "等待资源采样，缺测不补零。")
			return
		}
		low, high := points[0].seconds, points[len(points)-1].seconds
		if low == high {
			high = low + 1
		}
		x := chart.NewLinear(low, high)
		memory, cpu, temperature := resourceScale(points, 0, 1), resourceScale(points, 2), resourceScale(points, 3)
		ticks := []chart.Tick{}
		for i := 0; i <= 4; i++ {
			value := low + (high-low)*float64(i)/4
			ticks = append(ticks, chart.Tick{At: x.At(value), Label: time.Unix(int64(value), 0).UTC().Format("15:04:05")})
		}
		theme := c.Theme()
		colors := []ui.Color{theme.Accent, theme.Success, theme.Warning, theme.Danger}
		scales := []chart.Linear{memory, memory, cpu, temperature}
		xs := make([]float32, len(points))
		for i, p := range points {
			xs[i] = x.At(p.seconds)
		}
		describe := func(i int) string {
			p := points[i]
			return fmt.Sprintf("%s · 可用内存 %s GiB · RSS %s GiB · CPU %s %% · 温度 %s °C", p.utc, number(p.values[0]), number(p.values[1]), number(p.values[2]), number(p.values[3]))
		}
		chart.Plot(c, chart.Axes{Left: memory.Ticks(4), Right: cpu.Ticks(4), Bottom: ticks}, func(area ui.Element) {
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, tick := range memory.Ticks(4) {
					p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, c.Theme().Border)
				}
				for column := 0; column < 4; column++ {
					for i, point := range points {
						v := point.values[column]
						if !finite(v) {
							continue
						}
						px, py := chart.X(r, xs[i]), chart.Y(r, scales[column].At(*v))
						p.Fill(ui.Rect{X: px - 2, Y: py - 2, W: 4, H: 4}, colors[column], 2)
						if i > 0 && finite(points[i-1].values[column]) {
							var line ui.Path
							line.MoveTo(chart.X(r, xs[i-1]), chart.Y(r, scales[column].At(*points[i-1].values[column]))).LineTo(px, py)
							p.StrokePath(&line, 2, colors[column])
						}
					}
				}
			})
			chart.Select(c, area, &s.chartIndex, xs, describe)
			if s.chartIndex >= 0 && s.chartIndex < len(xs) {
				chart.RuleX(c, xs[s.chartIndex], 1).Background(c.Theme().TextMuted)
			}
		}).Height(260).Gap(8).Label("资源曲线").Description("左轴内存 GiB，右轴 CPU 百分比；温度独立刻度，缺测保留断点。")
		ui.Row(c).Gap(16).Children(func() {
			for i, label := range []string{"可用内存 GiB（左轴）", "服务 RSS GiB（左轴）", "服务 CPU %（右轴）", "温度 °C（独立刻度）"} {
				ui.Text(c, label).TextColor(colors[i])
			}
		})
		ui.Text(c, fmt.Sprintf("温度刻度：%.1f 至 %.1f °C · 时间轴：UTC", temperature.Lo, temperature.Hi))
		if s.chartIndex >= 0 && s.chartIndex < len(points) {
			ui.Text(c, describe(s.chartIndex)).Selectable()
		}
		ui.Text(c, "采样数值（内存原始单位 bytes）").Bold()
		cols := []ui.TableColumn{{Title: "UTC", Width: 220}, {Title: "可用内存 bytes", Width: 150}, {Title: "RSS bytes", Width: 130}, {Title: "CPU %", Width: 100}, {Title: "温度 °C", Width: 100}}
		ui.Table(c, &s.samplesTable, cols, len(points), func(row, col int) {
			p := points[row].sample
			values := []string{p.Utc, number(p.MemAvailableBytes), number(p.ServiceRssBytes), number(p.ServiceCpuPercent), number(p.TemperatureCelsius)}
			ui.Text(c, values[col]).Selectable()
		}).Height(180)
	})
}
