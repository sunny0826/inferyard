package pages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func reportNumber[T ~float64 | ~int64](value *T) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprint(*value)
}

func reportText(value string) string {
	if value == "" {
		return "—"
	}
	return displayValue(value)
}

func reportRate(rate core.ReportCounts) string {
	return reportNumber(rate.Numerator) + " / " + reportNumber(rate.Denominator)
}

func reportScaled[T ~float64 | ~int64](value *T, divisor float64, unit string) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f %s", float64(*value)/divisor, unit)
}

func reportMemoryReason(reason *string) string {
	return reportMemoryReasonFor(nil, reason)
}

func reportMemoryReasonFor(c *ui.Context, reason *string) string {
	switch emptyText(reason) {
	case "multiple_sources":
		return tr(c, "reports.metrics.memory_reason.multiple_sources")
	case "resource_samples_missing":
		return tr(c, "reports.metrics.memory_reason.resource_samples_missing")
	case "":
		return ""
	default:
		return *reason
	}
}

func reportExcludedNote(v *int64) string {
	return reportExcludedNoteFor(nil, v)
}

func reportExcludedNoteFor(c *ui.Context, v *int64) string {
	if v == nil || *v <= 0 {
		return ""
	}
	return trf(c, "reports.metrics.excluded_count", "count", *v)
}

type reportMetricSpec struct {
	icon      func() *ui.SVG
	label     string
	value     string
	detail    string
	note      string
	noteKind  stateKind
	hasRatio  bool
	ratio     float64
	ratioKind stateKind
}

func reportMetricCard(c *ui.Context, spec reportMetricSpec) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).MinWidth(0).Gap(t.Density.Tight+1).Padding(t.Metric.PaddingY, t.Metric.PaddingX), materialCard).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, spec.label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).MinWidth(0).SingleLine()
				if spec.icon != nil {
					ui.Box(c).Size(22, 22).Radius(t.Radius.SM).Shrink(0).
						Background(t.Color.Sunken).
						AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						components.Icon(c, spec.icon).Size(13, 13).TextColor(t.Color.FgSecondary)
					})
				}
			})
			valColor := t.Color.FgPrimary
			if spec.value == "—" || spec.value == "— / —" {
				valColor = t.Color.FgTertiary
			}
			ui.Text(c, spec.value).Bold().FontSize(t.Type.MetricLarge).FontFeatures("tnum").TextColor(valColor).SingleLine().Selectable()
			if spec.hasRatio {
				barColor := stateColor(c, spec.ratioKind)
				ui.Box(c).FillWidth().Height(4).Draw(func(p *ui.Painter, r ui.Rect) {
					if r.W <= 0 || r.H <= 0 {
						return
					}
					rad := r.H / 2
					p.Fill(r, t.Color.BorderDefault, rad)
					fillW := r.W * float32(max(0, min(1, spec.ratio)))
					if fillW > 0 {
						fillW = min(r.W, max(r.H, fillW))
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: fillW, H: r.H}, barColor, rad)
					}
				})
			}
			if spec.detail != "" {
				ui.Text(c, spec.detail).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			}
			if spec.note != "" {
				badge(c, spec.note, spec.noteKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
			}
		})
}

func reportMetrics(c *ui.Context, run core.ReportRun) {
	d := run.Dashboard
	qualityValue, qualityDetail := reportPassRate(c, d.Quality)
	qualityNote := ""
	if !d.Quality.Complete {
		qualityNote = tr(c, "reports.metrics.partial_scoring")
	}
	hasQualityRatio := d.Quality.Complete && d.Quality.Numerator != nil && d.Quality.Denominator != nil && *d.Quality.Denominator > 0
	var qualityRatio float64
	qualityRatioKind := stateSuccess
	if hasQualityRatio {
		qualityRatio = float64(*d.Quality.Numerator) / float64(*d.Quality.Denominator)
		if qualityRatio < 0.5 {
			qualityRatioKind = stateWarning
		}
	}
	available := reportScaled(d.SampledMinimumAvailableBytes, 1<<30, "GiB")
	metrics := []reportMetricSpec{
		{
			icon:      lucide.CircleCheck,
			label:     tr(c, "reports.metrics.answer_pass"),
			value:     qualityValue,
			detail:    qualityDetail,
			note:      qualityNote,
			noteKind:  stateWarning,
			hasRatio:  hasQualityRatio,
			ratio:     qualityRatio,
			ratioKind: qualityRatioKind,
		},
		{
			icon:     lucide.Activity,
			label:    tr(c, "reports.metrics.request_completion"),
			value:    reportRate(run.Summary.CompletionRate),
			detail:   tr(c, "reports.metrics.request_completion_detail"),
			note:     reportExcludedNoteFor(c, run.Summary.CompletionRate.Excluded),
			noteKind: stateNeutral,
		},
		{
			icon:  lucide.Clock,
			label: tr(c, "reports.metrics.median_latency"),
			value: reportScaled(d.Latency.P50, 1000, "s"),
			detail: func() string {
				if d.Latency.SampleCount != nil {
					return trf(c, "reports.metrics.valid_samples", "count", *d.Latency.SampleCount)
				}
				return trf(c, "reports.metrics.valid_samples", "count", reportNumber(d.Latency.SampleCount))
			}(),
			note:     reportExcludedNoteFor(c, d.Latency.Excluded),
			noteKind: stateNeutral,
		},
		{
			icon:     lucide.HardDrive,
			label:    tr(c, "reports.metrics.peak_rss"),
			value:    reportScaled(d.SampledPeakRSSBytes, 1<<30, "GiB"),
			detail:   trf(c, "reports.metrics.min_system_available_detail", "value", strings.ReplaceAll(available, " ", "\u00a0")),
			note:     reportMemoryReasonFor(c, d.SampledPeakRSSReason),
			noteKind: stateNeutral,
		},
	}
	columns := 4
	if compactLayout(c) {
		columns = 1
	} else if width, _ := c.Size(); width < 1200 {
		columns = 2
	}
	ui.Grid(c).Columns(columns).Gap(tokens(c).Density.Stack).Children(func() {
		for _, metric := range metrics {
			reportMetricCard(c, metric)
		}
	})
	if reason := reportMemoryReasonFor(c, d.MinimumAvailableReason); reason != "" {
		infoBanner(c, trf(c, "reports.metrics.system_available_memory_note", "reason", reason))
	}
	reportFactSummary(c, run)
}

func reportPassRate(c *ui.Context, quality core.ReportQuality) (value, detail string) {
	counts := reportRate(quality.StudioCounts)
	detail = tr(c, "reports.metrics.answer_pass_detail")
	if !quality.Complete || quality.Numerator == nil || quality.Denominator == nil || *quality.Denominator <= 0 {
		return counts, detail
	}
	return fmt.Sprintf("%.1f%%", float64(*quality.Numerator)*100/float64(*quality.Denominator)), counts + " · " + detail
}

func reportFactSummary(c *ui.Context, run core.ReportRun) {
	d := run.Dashboard
	if d.Quality.Numerator == nil && d.Quality.Denominator == nil &&
		run.Summary.CompletionRate.Numerator == nil && run.Summary.CompletionRate.Denominator == nil &&
		d.Latency.P50 == nil && d.SampledPeakRSSBytes == nil {
		ui.Text(c, tr(c, "reports.metrics.no_summary")).TextColor(c.Theme().TextMuted)
	}
}

func reportStatRow(c *ui.Context, label, value string, muted bool) {
	t := tokens(c)
	ariaLabel := trf(c, "device.row.aria_label", "label", label, "value", value)
	valColor := t.Color.FgPrimary
	if muted || value == "" || value == "—" {
		valColor = t.Color.FgTertiary
	}
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Tight+1, 0).Children(func() {
		ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).MinWidth(100)
		el := ui.Text(c, value).FontFeatures("tnum").TextColor(valColor).Selectable().Label(ariaLabel)
		if !muted && value != "" && value != "—" {
			el.Bold()
		}
	})
}

func reportThroughput(c *ui.Context, run core.ReportRun) {
	t := tokens(c)
	throughput := run.Dashboard.Throughput
	reportSectionCard(c, lucide.Gauge, tr(c, "reports.throughput.title"), nil, func() {
		if throughput == nil {
			ui.Text(c, tr(c, "reports.metrics.not_measured")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
			return
		}
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			reportStatRow(c, tr(c, "reports.throughput.requests_per_sec"), reportPerSecondFor(c, throughput.RequestsPerSecond, throughput.RequestsPerSecondReason), throughput.RequestsPerSecond == nil)
			reportKVDivider(c)
			reportStatRow(c, tr(c, "reports.throughput.output_tokens_per_sec"), reportPerSecondFor(c, throughput.OutputTokensPerSecond, throughput.OutputTokensPerSecondReason), throughput.OutputTokensPerSecond == nil)
			reportKVDivider(c)
			reportStatRow(c, tr(c, "reports.throughput.goodput_per_sec"), reportPerSecondFor(c, throughput.GoodputPerSecond, throughput.GoodputPerSecondReason), throughput.GoodputPerSecond == nil)
		})
	})
}

func reportPerSecond(value *float64, reason *string) string {
	return reportPerSecondFor(nil, value, reason)
}

func reportPerSecondFor(c *ui.Context, value *float64, reason *string) string {
	if value == nil {
		return reportThroughputReasonFor(c, reason)
	}
	if *value == 0 {
		return "0"
	}
	return reportDecimal(*value)
}

func reportDecimal(value float64) string {
	if value >= 0.01 || value <= -0.01 {
		return fmt.Sprintf("%.2f", value)
	}
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func reportThroughputReason(reason *string) string {
	return reportThroughputReasonFor(nil, reason)
}

func reportThroughputReasonFor(c *ui.Context, reason *string) string {
	switch emptyText(reason) {
	case "":
		return "—"
	case "no_formal_requests":
		return tr(c, "reports.throughput.reason.no_formal_requests")
	case "terminal_window_unavailable":
		return tr(c, "reports.throughput.reason.terminal_window_unavailable")
	case "output_token_counts_incomplete":
		return tr(c, "reports.throughput.reason.output_token_counts_incomplete")
	case "no_completed_requests":
		return tr(c, "reports.throughput.reason.no_completed_requests")
	case "quality_not_applicable":
		return tr(c, "reports.throughput.reason.quality_not_applicable")
	case "thresholds_not_preregistered":
		return tr(c, "reports.throughput.reason.thresholds_not_preregistered")
	case "goodput_indeterminable":
		return tr(c, "reports.throughput.reason.goodput_indeterminable")
	case "observation_missing":
		return tr(c, "reports.throughput.reason.observation_missing")
	default:
		return *reason
	}
}

func reportOutputIntervals(c *ui.Context, run core.ReportRun) {
	t := tokens(c)
	intervals := run.Dashboard.OutputBlockIntervals
	reportSectionCard(c, lucide.Clock, tr(c, "reports.intervals.title"), nil, func() {
		if intervals == nil {
			ui.Text(c, tr(c, "reports.metrics.not_measured")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
			return
		}
		ui.Column(c).FillWidth().Gap(0).Children(func() {
			reportStatRow(c, tr(c, "reports.intervals.median"), reportMilliseconds(intervals.P50), intervals.P50 == nil)
			reportKVDivider(c)
			reportStatRow(c, "P95", reportIntervalP95For(c, intervals), intervals.P95 == nil)
		})
		if reason := reportIntervalReasonFor(c, intervals.Reason); reason != "" {
			ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
	})
}

func reportIntervalP95(intervals *core.ReportOutputBlockIntervals) string {
	return reportIntervalP95For(nil, intervals)
}

func reportIntervalP95For(c *ui.Context, intervals *core.ReportOutputBlockIntervals) string {
	if intervals.P95 == nil {
		if emptyText(intervals.P95Reason) == "insufficient_samples" {
			return trf(c, "reports.intervals.p95_insufficient_value", "value", "—")
		}
		return "—"
	}
	value := reportMilliseconds(intervals.P95)
	if intervals.IntervalCount >= 20 && intervals.IntervalCount < 100 {
		return trf(c, "reports.intervals.p95_with_n", "value", value, "count", intervals.IntervalCount)
	}
	return value
}

func reportMilliseconds(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f ms", *value)
}

func reportIntervalReason(reason *string) string {
	return reportIntervalReasonFor(nil, reason)
}

func reportIntervalReasonFor(c *ui.Context, reason *string) string {
	switch emptyText(reason) {
	case "":
		return ""
	case "no_completed_requests":
		return tr(c, "reports.intervals.reason.no_completed_requests")
	case "arrival_capture_missing":
		return tr(c, "reports.intervals.reason.arrival_capture_missing")
	case "not_streaming":
		return tr(c, "reports.intervals.reason.not_streaming")
	case "insufficient_blocks":
		return tr(c, "reports.intervals.reason.insufficient_blocks")
	case "observation_missing":
		return tr(c, "reports.intervals.reason.observation_missing")
	default:
		return *reason
	}
}
