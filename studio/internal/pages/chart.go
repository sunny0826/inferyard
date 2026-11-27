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
	"inferyard/studio/internal/icons/reicon"
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

type resourceViewCache struct {
	taskID  string
	count   int
	lastSeq int64
	series  []resourceSeries
	sorted  []core.ResourceSample
}

func (s *viewState) cachedResourceSeries(samples []core.ResourceSample) ([]resourceSeries, []core.ResourceSample) {
	if len(samples) == 0 {
		return nil, nil
	}
	var lastSeq int64
	lastSeq = samples[len(samples)-1].Seq
	if s.resourceCache.taskID == s.detailID && s.resourceCache.count == len(samples) && s.resourceCache.lastSeq == lastSeq && s.resourceCache.series != nil {
		return s.resourceCache.series, s.resourceCache.sorted
	}
	series := resourceSeriesData(samples)
	metricAllowed := map[string]bool{
		"system_mem_available": true,
		"service_rss":          true,
		"service_cpu_percent":  true,
		"temperature":          true,
	}
	var selected []core.ResourceSample
	for _, sr := range series {
		if metricAllowed[sr.source.Metric] {
			selected = append(selected, sr.points...)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Seq < selected[j].Seq })
	s.resourceCache = resourceViewCache{
		taskID:  s.detailID,
		count:   len(samples),
		lastSeq: lastSeq,
		series:  series,
		sorted:  selected,
	}
	return series, selected
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

func latestResourceReading(c *ui.Context, series resourceSeries) string {
	for i := len(series.points) - 1; i >= 0; i-- {
		pt := series.points[i]
		if !finite(pt.Value) || pt.MissingReason != nil {
			continue
		}
		val, unit := formatResourceSample(c, pt)
		if unit == "%" || unit == "" {
			return val + unit
		}
		return val + " " + unit
	}
	return ""
}

func (s *viewState) resourceView(c *ui.Context, samples []core.ResourceSample) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("detail-resource-card")).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		series, selected := s.cachedResourceSeries(samples)
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "chart.resource.title"), 2)
			ui.Box(c).Grow(1)
			inlineBadge(c, trf(c, "chart.resource.sample_count", "count", len(samples)), stateNeutral, badgeOptions{Size: sizeSM, Style: badgeSoft})
		})
		if len(series) == 0 {
			applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
				Padding(t.Density.Stack, t.Density.Section), materialSunken).Children(func() {
				studioIcon(c, reicon.Activity, t.Color.FgTertiary)
				ui.Text(c, tr(c, "chart.resource.waiting")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			return
		}
		theme := c.Theme()
		metrics := resourceMetrics(c, theme)
		cols := 2
		if compactLayout(c) {
			cols = 1
		}
		grid := ui.Grid(c).Columns(cols).Gap(t.Density.Stack)
		grp := ui.Local(grid, "resource-hover-group", func() resourceHoverGroupState { return resourceHoverGroupState{} })
		grp.anyOverInPass = false
		draw := func(metric resourceMetric) {
			c.Key(metric.id)
			var options []resourceSeries
			for _, source := range series {
				if source.source.Metric == metric.id {
					options = append(options, source)
				}
			}
			if len(options) == 0 {
				applyMaterial(c, ui.Column(c).FillWidth().MinHeight(184).Gap(t.Density.Row).Padding(t.Density.Stack), materialSunken).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Box(c).Size(7, 7).Radius(t.Radius.Full).Shrink(0).Background(t.Color.BorderStrong)
						ui.Text(c, metric.label+" · —").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
					})
					ui.Box(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, tr(c, "chart.resource.no_valid_samples")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
					})
				})
				return
			}
			container := applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Density.Stack), materialSunken)
			state := ui.Local(container, "resource-chart", func() resourceChartState {
				return resourceChartState{chart: chart.State{Index: -1}}
			})
			container.Children(func() {
				chosen, data := defaultResourceSeries(options, metric.color)
				if state.source != chosen.id {
					state.source, state.chart.Index = chosen.id, -1
				}
				latest := latestResourceReading(c, chosen)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Box(c).Size(7, 7).Radius(t.Radius.Full).Shrink(0).Background(metric.color)
					ui.Text(c, metric.label).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1).MinWidth(0).SingleLine()
					if latest != "" {
						ui.Text(c, latest).Bold().FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgPrimary).SingleLine()
					}
				})
				if label := secondarySourceLabel(c, metric.id, chosen.source); label != "" {
					ui.Text(c, label).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgTertiary).SingleLine()
				}
				if !chartHasValues(data) {
					reason := tr(c, "chart.resource.no_valid_samples")
					for _, point := range chosen.points {
						if point.MissingReason != nil {
							reason = *point.MissingReason
						}
					}
					ui.Box(c).FillWidth().MinHeight(120).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, trf(c, "chart.resource.missing_reason", "reason", reason)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					})
					return
				}
				resourceLinePlot(c, grp, &state.chart, data, resourcePlotOptions{
					id:          metric.id,
					label:       trf(c, "chart.resource.curve_label", "metric", metric.label),
					description: tr(c, "chart.resource.curve_description"),
					height:      140,
					detail: func(c *ui.Context, index int) {
						ui.Column(c).Children(func() {
							if index >= 0 && index < len(chosen.points) {
								ui.Text(c, formatResourceDetail(c, chosen.points[index])).FontSize(t.Type.Caption).FontFeatures("tnum").Selectable()
							}
						})
					},
				})
			})
		}
		grid.Children(func() {
			for _, metric := range metrics {
				draw(metric)
			}
		})
		if !grp.anyOverInPass {
			grp.hoverChartID = 0
			grp.pointerActive = false
		}
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
	t := tokens(c)
	const pageSize = 50
	pages := max(1, (len(samples)+pageSize-1)/pageSize)
	s.resourceTablePage = min(max(0, s.resourceTablePage), pages-1)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			ui.Text(c, trf(c, "chart.table.page_status", "page", s.resourceTablePage+1, "totalPages", pages, "count", len(samples))).
				FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			ui.Box(c).Grow(1)
			if outlineIconButton(c, reicon.ArrowLeft, tr(c, "chart.table.prev_page")).Disabled(s.resourceTablePage == 0).Clicked() {
				s.resourceTablePage--
			}
			if outlineIconButton(c, reicon.ArrowRight, tr(c, "chart.table.next_page")).Disabled(s.resourceTablePage == pages-1).Clicked() {
				s.resourceTablePage++
			}
		})
		start := s.resourceTablePage * pageSize
		pageSamples := samples[start:min(start+pageSize, len(samples))]
		cols := []ui.TableColumn{
			{Title: tr(c, "chart.table.col_time"), Width: 220},
			{Title: tr(c, "chart.table.col_source"), Width: 250},
			{Title: tr(c, "chart.table.col_value"), Width: 140},
			{Title: tr(c, "chart.table.col_unit"), Width: 80},
			{Title: tr(c, "chart.table.col_missing_reason"), Width: 180},
		}
		tableLabel := tr(c, "chart.table.aria_label")
		s.samplesTable.Key = func(i int) any { return pageSamples[i].Seq }
		applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
			pageTable(c, &s.samplesTable, tableLabel, cols, len(pageSamples), func(row, col int) {
				p := pageSamples[row]
				value, unit := formatResourceSample(c, p)
				missing := emptyText(p.MissingReason)
				values := []string{formatTimestamp(p.Utc), sourceLabelFor(c, p.Source), value, unit, missing}
				cell := ui.Text(c, values[col]).SingleLine().Selectable()
				switch col {
				case 0:
					cell.FontFeatures("tnum").TextColor(t.Color.FgSecondary)
				case 2:
					cell.FontFeatures("tnum")
					if !finite(p.Value) || p.MissingReason != nil {
						cell.TextColor(t.Color.FgTertiary)
					}
				case 3:
					cell.TextColor(t.Color.FgSecondary)
				case 4:
					if missing == "" {
						cell.TextColor(t.Color.FgTertiary)
					} else {
						cell.TextColor(stateScale(c, stateWarning).Fg)
					}
				}
			}).Height(180).Label(tableLabel)
		})
	})
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
