package pages

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
	"inferyard/studio/internal/core"
)

type resourceSeries struct {
	source core.ResourceSource
	id     string
	points []core.ResourceSample
}

type resourceChartState struct {
	source string
	chart  chart.State
}

func resourceSeriesData(samples []core.ResourceSample) []resourceSeries {
	groups := map[core.ResourceSource][]core.ResourceSample{}
	for _, sample := range samples {
		if _, err := time.Parse(time.RFC3339Nano, sample.Utc); err != nil {
			continue
		}
		groups[sample.Source] = append(groups[sample.Source], sample)
	}
	series := make([]resourceSeries, 0, len(groups))
	for source, points := range groups {
		sort.SliceStable(points, func(i, j int) bool {
			if points[i].ReadStartedNs != points[j].ReadStartedNs {
				return points[i].ReadStartedNs < points[j].ReadStartedNs
			}
			return points[i].Seq < points[j].Seq
		})
		raw, _ := json.Marshal(source)
		series = append(series, resourceSeries{source: source, id: string(raw), points: points})
	}
	sort.Slice(series, func(i, j int) bool { return series[i].id < series[j].id })
	return series
}

func finite(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }

func resourceLineData(series resourceSeries, color ui.Color) chart.Data {
	data := chart.Data{Labels: make([]string, len(series.points))}
	values := make([]float64, len(series.points))
	var origin int64
	for i, point := range series.points {
		midpoint := point.ReadStartedNs + (point.ReadFinishedNs-point.ReadStartedNs)/2
		if i == 0 {
			origin = midpoint
		}
		elapsed := max(int64(0), (midpoint-origin+50_000_000)/100_000_000)
		data.Labels[i] = fmt.Sprintf("%02d:%02d", elapsed/600, elapsed/10%60)
		if elapsed%10 != 0 {
			data.Labels[i] += fmt.Sprintf(".%d", elapsed%10)
		}
		values[i] = math.NaN()
		if finite(point.Value) && point.MissingReason == nil {
			values[i] = *point.Value
			if series.source.Unit == "bytes" {
				values[i] /= 1073741824
			}
		}
	}
	data.Series = []chart.Series{{ID: series.id, Label: series.source.Name, Color: color, Values: values}}
	return data
}

func sourceLabel(source core.ResourceSource) string {
	if source.Name == "" {
		return "来源未记录"
	}
	return source.Name
}

func (s *viewState) resourceView(c *ui.Context, samples []core.ResourceSample) {
	card(c, "资源曲线", func() {
		series := resourceSeriesData(samples)
		ui.Text(c, fmt.Sprintf("采样记录：%d · 每个来源保留最近 600 点", len(samples)))
		if len(series) == 0 {
			ui.Text(c, "等待资源采样，缺测不补零。")
			return
		}
		theme := c.Theme()
		ui.Text(c, "各曲线按自己的采样时间显示，采样点等距排列；缺测保留断点。").TextColor(theme.TextMuted)
		metrics := []struct {
			id, label string
			color     ui.Color
		}{
			{"system_mem_available", "可用内存 GiB", theme.Accent},
			{"service_rss", "服务 RSS GiB", theme.Success},
			{"service_cpu_percent", "服务 CPU %", theme.Warning},
			{"temperature", "传感器温度 °C", theme.Danger},
		}
		selected := []core.ResourceSample{}
		for _, metric := range metrics {
			c.Key(metric.id)
			var options []resourceSeries
			for _, source := range series {
				if source.source.Metric == metric.id {
					options = append(options, source)
				}
			}
			if len(options) == 0 {
				ui.Text(c, metric.label+" · 此运行未记录该指标").TextColor(theme.TextMuted)
				continue
			}
			container := ui.Column(c).FillWidth().Gap(theme.Space(2))
			state := ui.Local(container, "resource-chart", func() resourceChartState {
				return resourceChartState{chart: chart.State{Index: -1}}
			})
			container.Children(func() {
				ui.Text(c, metric.label).TextColor(metric.color)
				current := 0
				for i, option := range options {
					if state.source == option.id {
						current = i
					}
				}
				if state.source != options[current].id {
					state.source, state.chart.Index = options[current].id, -1
				}
				if len(options) > 1 {
					picker := components.Select(c, &state.source)
					picker.Trigger.Label(metric.label + "来源").Children(func() {
						ui.Text(c, sourceLabel(options[current].source)).SingleLine()
					})
					picker.Popup(func(ui.Element) {
						for _, option := range options {
							picker.Item(option.id).Label(sourceLabel(option.source)).Children(func() {
								ui.Text(c, sourceLabel(option.source)).SingleLine()
								ui.Text(c, option.source.Collector+" · "+option.source.ClockID).TextColor(theme.TextMuted)
							})
						}
					})
					if picker.Trigger.Changed() {
						state.chart.Index = -1
						s.samplesTable = pageTableState{}
						s.resourceTablePage = 0
					}
				} else {
					ui.Text(c, sourceLabel(options[current].source)).TextColor(theme.TextMuted)
				}
				if metric.id == "service_cpu_percent" {
					ui.Text(c, "单核等效：100% 为占满一个逻辑核，可超过 100%；仅绑定服务进程，不含子进程。").TextColor(theme.TextMuted)
				}
				chosen := options[current]
				selected = append(selected, chosen.points...)
				data := resourceLineData(chosen, metric.color)
				if !chartHasValues(data) {
					reason := "暂无有效采样"
					for _, point := range chosen.points {
						if point.MissingReason != nil {
							reason = *point.MissingReason
						}
					}
					ui.Text(c, "缺测："+reason).TextColor(theme.TextMuted)
					return
				}
				chart.Line(c, &state.chart, data, chart.Options{
					HideLegend: true, Label: metric.label + "曲线", PlotHeight: 140, AnimationDuration: -1,
					Description: "本来源独立刻度与时间，左右方向键选择样本。",
					Detail: func(c *ui.Context, selection chart.Selection) {
						ui.Column(c).MinHeight(theme.Space(6)).Children(func() {
							if selection.Index >= 0 && selection.Index < len(chosen.points) {
								point := chosen.points[selection.Index]
								value := number(point.Value)
								if point.MissingReason != nil {
									value = "缺测：" + *point.MissingReason
								}
								ui.Text(c, point.Utc+" · "+value+" "+chosen.source.Unit).Selectable()
							}
						})
					},
				})
			})
		}
		s.resourceTable(c, selected)
	})
}

func (s *viewState) resourceTable(c *ui.Context, samples []core.ResourceSample) {
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Seq < samples[j].Seq })
	heading(c, "当前来源数值（内存原始单位 bytes）", 3)
	const pageSize = 50
	pages := max(1, (len(samples)+pageSize-1)/pageSize)
	s.resourceTablePage = min(max(0, s.resourceTablePage), pages-1)
	ui.Row(c).Gap(c.Theme().Space(2)).Children(func() {
		if button(c, "上一页采样").Disabled(s.resourceTablePage == 0).Clicked() {
			s.resourceTablePage--
		}
		ui.Text(c, fmt.Sprintf("第 %d / %d 页 · 当前来源共 %d 条记录", s.resourceTablePage+1, pages, len(samples)))
		if button(c, "下一页采样").Disabled(s.resourceTablePage == pages-1).Clicked() {
			s.resourceTablePage++
		}
	})
	start := s.resourceTablePage * pageSize
	samples = samples[start:min(start+pageSize, len(samples))]
	cols := []ui.TableColumn{{Title: "UTC", Width: 220}, {Title: "来源", Width: 250}, {Title: "数值", Width: 140}, {Title: "单位", Width: 80}, {Title: "缺测原因", Width: 180}}
	s.samplesTable.Key = func(i int) any { return samples[i].Seq }
	pageTable(c, &s.samplesTable, "资源采样数值表", cols, len(samples), func(row, col int) {
		p := samples[row]
		value := number(p.Value)
		if p.Value != nil && p.Source.Unit == "bytes" {
			value = fmt.Sprintf("%.0f", *p.Value)
		}
		values := []string{p.Utc, sourceLabel(p.Source), value, p.Source.Unit, emptyText(p.MissingReason)}
		ui.Text(c, values[col]).Selectable()
	}).Height(180).Label("资源采样数值表")
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
