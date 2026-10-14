package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chart"
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
		theme := c.Theme()
		colors := []ui.Color{theme.Accent, theme.Success, theme.Warning, theme.Danger}
		describe := func(i int) string {
			p := points[i]
			return fmt.Sprintf("%s · 可用内存 %s GiB · RSS %s GiB · CPU %s %% · 温度 %s °C", p.utc, number(p.values[0]), number(p.values[1]), number(p.values[2]), number(p.values[3]))
		}
		ui.Text(c, "内存 GiB、CPU %、温度 °C 各用独立刻度分图展示。横轴按 UTC 样本顺序等距排列，实际时间见采样值。")
		labels := []string{"内存 GiB", "服务 CPU %", "温度 °C"}
		columns := [][]int{{0, 1}, {2}, {3}}
		for i, cols := range columns {
			c.Key(labels[i])
			heading(c, labels[i], 3)
			state := &s.resourceCharts[i]
			state.Index = s.chartIndex
			before := state.Index
			data := resourceChartData(points, colors, cols...)
			chart.Line(c, state, data, chart.Options{
				HideLegend:        true,
				Label:             labels[i] + "曲线",
				Description:       labels[i] + "独立刻度；缺测保留断点。左右方向键选择样本。",
				PlotHeight:        160,
				AnimationDuration: -1,
				EmptyText:         "此指标暂无有效测量；缺测不补零。",
				Detail: func(c *ui.Context, selection chart.Selection) {
					if selection.Index >= 0 {
						ui.Text(c, describe(selection.Index)).Selectable()
					}
				},
			})
			if state.Index != before && chartHasValues(data) {
				s.chartIndex = state.Index
			}
		}
		ui.Row(c).Wrap().Gap(c.Theme().Space(4)).Children(func() {
			for i, label := range []string{"可用内存 GiB（内存图）", "服务 RSS GiB（内存图）", "服务 CPU %（CPU 图）", "温度 °C（独立刻度）"} {
				ui.Text(c, label).TextColor(colors[i])
			}
		})
		if s.chartIndex >= 0 && s.chartIndex < len(points) {
			ui.Text(c, describe(s.chartIndex)).Selectable()
		}
		heading(c, "采样数值（内存原始单位 bytes）", 3)
		cols := []ui.TableColumn{{Title: "UTC", Width: 220}, {Title: "可用内存 bytes", Width: 150}, {Title: "RSS bytes", Width: 130}, {Title: "CPU %", Width: 100}, {Title: "温度 °C", Width: 100}}
		s.samplesTable.Key = func(i int) any { return points[i].utc }
		pageTable(c, &s.samplesTable, "资源采样数值表", cols, len(points), func(row, col int) {
			p := points[row].sample
			values := []string{p.Utc, number(p.MemAvailableBytes), number(p.ServiceRssBytes), number(p.ServiceCpuPercent), number(p.TemperatureCelsius)}
			ui.Text(c, values[col]).Selectable()
		}).Height(180).Label("资源采样数值表")
	})
}

func resourceChartData(points []resourcePoint, colors []ui.Color, columns ...int) chart.Data {
	data := chart.Data{Labels: make([]string, len(points))}
	names := []string{"可用内存 GiB", "服务 RSS GiB", "服务 CPU %", "温度 °C"}
	ids := []string{"available-memory", "service-rss", "service-cpu", "temperature"}
	for i, p := range points {
		data.Labels[i] = p.utc
	}
	for _, col := range columns {
		series := chart.Series{ID: ids[col], Label: names[col], Color: colors[col], Values: make([]float64, len(points))}
		for i, p := range points {
			series.Values[i] = math.NaN()
			if finite(p.values[col]) {
				series.Values[i] = *p.values[col]
			}
		}
		data.Series = append(data.Series, series)
	}
	return data
}

func chartHasValues(data chart.Data) bool {
	for _, series := range data.Series {
		for _, v := range series.Values {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				return true
			}
		}
	}
	return false
}
