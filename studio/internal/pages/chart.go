package pages

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
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

func sourceLabelFor(c *ui.Context, source core.ResourceSource) string {
	if source.Name == "" {
		return tr(c, "chart.source.unrecorded")
	}
	return source.Name
}

func (s *viewState) resourceView(c *ui.Context, samples []core.ResourceSample) {
	card(c, tr(c, "chart.resource.title"), func() {
		series := resourceSeriesData(samples)
		ui.Text(c, trf(c, "chart.resource.sample_count", "count", len(samples)))
		if len(series) == 0 {
			ui.Text(c, tr(c, "chart.resource.waiting"))
			return
		}
		theme := c.Theme()
		metrics := resourceMetrics(c, theme)
		selected := []core.ResourceSample{}
		draw := func(metric resourceMetric) {
			c.Key(metric.id)
			var options []resourceSeries
			for _, source := range series {
				if source.source.Metric == metric.id {
					options = append(options, source)
				}
			}
			if len(options) == 0 {
				ui.Text(c, metric.label+" · —").TextColor(theme.TextMuted)
				return
			}
			container := ui.Column(c).FillWidth().Gap(tokens(c).Density.Row)
			state := ui.Local(container, "resource-chart", func() resourceChartState {
				return resourceChartState{chart: chart.State{Index: -1}}
			})
			container.Children(func() {
				ui.Text(c, metric.label).TextColor(metric.color)
				chosen, data := defaultResourceSeries(options, metric.color)
				if state.source != chosen.id {
					state.source, state.chart.Index = chosen.id, -1
				}
				if label := secondarySourceLabel(c, metric.id, chosen.source); label != "" {
					ui.Text(c, label).TextColor(theme.TextMuted)
				}
				for _, option := range options {
					selected = append(selected, option.points...)
				}
				if !chartHasValues(data) {
					reason := tr(c, "chart.resource.no_valid_samples")
					for _, point := range chosen.points {
						if point.MissingReason != nil {
							reason = *point.MissingReason
						}
					}
					ui.Text(c, trf(c, "chart.resource.missing_reason", "reason", reason)).TextColor(theme.TextMuted)
					return
				}
				chart.Line(c, &state.chart, data, chart.Options{
					HideLegend: true, Label: trf(c, "chart.resource.curve_label", "metric", metric.label), PlotHeight: 140, AnimationDuration: -1,
					Description: tr(c, "chart.resource.curve_description"),
					Detail: func(c *ui.Context, selection chart.Selection) {
						ui.Column(c).Children(func() {
							if selection.Index >= 0 && selection.Index < len(chosen.points) {
								ui.Text(c, formatResourceDetail(c, chosen.points[selection.Index])).Selectable()
							}
						})
					},
				})
			})
		}
		cols := 2
		if compactLayout(c) {
			cols = 1
		}
		ui.Grid(c).Columns(cols).Gap(tokens(c).Density.Stack).Children(func() {
			for _, metric := range metrics {
				draw(metric)
			}
		})
		s.resourceSampleFold(c, selected)
	})
}

func defaultResourceSeries(options []resourceSeries, color ui.Color) (resourceSeries, chart.Data) {
	chosen := options[0]
	chosenData := resourceLineData(chosen, color)
	if chartHasValues(chosenData) {
		return chosen, chosenData
	}
	for _, option := range options[1:] {
		data := resourceLineData(option, color)
		if chartHasValues(data) {
			return option, data
		}
	}
	return chosen, chosenData
}

func secondarySourceLabel(c *ui.Context, metricID string, source core.ResourceSource) string {
	name := sourceLabelFor(c, source)
	if metricID == "service_cpu_percent" {
		return tr(c, "chart.resource.service_cpu_note")
	}
	if metricID == "temperature" || !strings.Contains(name, ":") {
		return name
	}
	return ""
}

type resourceMetric struct {
	id, label string
	color     ui.Color
}

func resourceMetrics(c *ui.Context, theme *ui.Theme) []resourceMetric {
	colors := colorTokensFor(theme, false).Chart
	return []resourceMetric{
		{"system_mem_available", tr(c, "chart.metric.system_mem_available"), colors[0]},
		{"service_rss", tr(c, "chart.metric.service_rss"), colors[1]},
		{"service_cpu_percent", tr(c, "chart.metric.service_cpu_percent"), colors[2]},
		{"temperature", tr(c, "chart.metric.temperature"), colors[4]},
	}
}

func (s *viewState) resourceSampleFold(c *ui.Context, selected []core.ResourceSample) {
	componentFold(c, tr(c, "chart.table.fold_title"), func() { s.resourceTable(c, selected) })
}

func (s *viewState) resourceTable(c *ui.Context, samples []core.ResourceSample) {
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Seq < samples[j].Seq })
	heading(c, tr(c, "chart.table.heading"), 3)
	const pageSize = 50
	pages := max(1, (len(samples)+pageSize-1)/pageSize)
	s.resourceTablePage = min(max(0, s.resourceTablePage), pages-1)
	ui.Row(c).AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
		if button(c, tr(c, "chart.table.prev_page")).Disabled(s.resourceTablePage == 0).Clicked() {
			s.resourceTablePage--
		}
		ui.Text(c, trf(c, "chart.table.page_status", "page", s.resourceTablePage+1, "totalPages", pages, "count", len(samples)))
		if button(c, tr(c, "chart.table.next_page")).Disabled(s.resourceTablePage == pages-1).Clicked() {
			s.resourceTablePage++
		}
	})
	start := s.resourceTablePage * pageSize
	samples = samples[start:min(start+pageSize, len(samples))]
	cols := []ui.TableColumn{
		{Title: tr(c, "chart.table.col_time"), Width: 220},
		{Title: tr(c, "chart.table.col_source"), Width: 250},
		{Title: tr(c, "chart.table.col_value"), Width: 140},
		{Title: tr(c, "chart.table.col_unit"), Width: 80},
		{Title: tr(c, "chart.table.col_missing_reason"), Width: 180},
	}
	tableLabel := tr(c, "chart.table.aria_label")
	s.samplesTable.Key = func(i int) any { return samples[i].Seq }
	pageTable(c, &s.samplesTable, tableLabel, cols, len(samples), func(row, col int) {
		p := samples[row]
		value, unit := formatResourceSample(c, p)
		values := []string{formatTimestamp(p.Utc), sourceLabelFor(c, p.Source), value, unit, emptyText(p.MissingReason)}
		ui.Text(c, values[col]).Selectable()
	}).Height(180).Label(tableLabel)
}

func resourceDisplayUnit(unit string) string {
	switch unit {
	case "bytes":
		return "GiB"
	case "celsius":
		return "°C"
	default:
		return unit
	}
}

func formatResourceSample(c *ui.Context, sample core.ResourceSample) (value, unit string) {
	unit = resourceDisplayUnit(sample.Source.Unit)
	if !finite(sample.Value) || sample.MissingReason != nil {
		return tr(c, "common.missing"), unit
	}
	v := *sample.Value
	if sample.Source.Unit == "bytes" {
		v /= 1073741824
	}
	rounded := math.Round(v*100) / 100
	if rounded == 0 {
		rounded = 0
	}
	return fmt.Sprintf("%.2f", rounded), unit
}

func formatResourceDetail(c *ui.Context, sample core.ResourceSample) string {
	stamp := formatTimestamp(sample.Utc)
	if sample.MissingReason != nil {
		return stamp + " · " + trf(c, "chart.resource.missing_reason", "reason", *sample.MissingReason)
	}
	value, unit := formatResourceSample(c, sample)
	if !finite(sample.Value) {
		return stamp + " · " + value
	}
	if unit == "%" || unit == "" {
		return stamp + " · " + value + unit
	}
	return stamp + " · " + value + " " + unit
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
