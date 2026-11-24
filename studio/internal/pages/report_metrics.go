package pages

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
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

func reportMetrics(c *ui.Context, run core.ReportRun) {
	d := run.Dashboard
	qualityNote := ""
	if !d.Quality.Complete {
		qualityNote = tr(c, "reports.metrics.partial_scoring")
	}
	metrics := []struct{ label, value, detail, note string }{
		{
			tr(c, "reports.metrics.answer_pass"),
			reportPassRate(d.Quality),
			tr(c, "reports.metrics.answer_pass_detail"),
			qualityNote,
		},
		{
			tr(c, "reports.metrics.request_completion"),
			reportRate(run.Summary.CompletionRate),
			tr(c, "reports.metrics.request_completion_detail"),
			reportExcludedNoteFor(c, run.Summary.CompletionRate.Excluded),
		},
		{
			tr(c, "reports.metrics.median_latency"),
			reportScaled(d.Latency.P50, 1000, "s"),
			trf(c, "reports.metrics.valid_samples", "count", reportNumber(d.Latency.SampleCount)),
			reportExcludedNoteFor(c, d.Latency.Excluded),
		},
		{
			tr(c, "reports.metrics.peak_rss"),
			reportScaled(d.SampledPeakRSSBytes, 1<<30, "GiB"),
			trf(c, "reports.metrics.min_system_available_detail", "value", reportScaled(d.SampledMinimumAvailableBytes, 1<<30, "GiB")),
			reportMemoryReasonFor(c, d.SampledPeakRSSReason),
		},
	}
	columns := 4
	if compactLayout(c) {
		columns = 1
	}
	ui.Grid(c).Columns(columns).Gap(tokens(c).Density.Stack).Children(func() {
		for _, metric := range metrics {
			metricCard(c, metric.label, metric.value, metric.detail, metric.note)
		}
	})
	if reason := reportMemoryReasonFor(c, d.MinimumAvailableReason); reason != "" {
		ui.Text(c, trf(c, "reports.metrics.system_available_memory_note", "reason", reason)).TextColor(c.Theme().TextMuted)
	}
	reportFactSummary(c, run)
}

func reportPassRate(quality core.ReportQuality) string {
	rate := reportRate(quality.StudioCounts)
	if !quality.Complete || quality.Numerator == nil || quality.Denominator == nil || *quality.Denominator <= 0 {
		return rate
	}
	return fmt.Sprintf("%s · %.1f%%", rate, float64(*quality.Numerator)*100/float64(*quality.Denominator))
}

func reportFactSummary(c *ui.Context, run core.ReportRun) {
	d := run.Dashboard
	if d.Quality.Numerator == nil && d.Quality.Denominator == nil &&
		run.Summary.CompletionRate.Numerator == nil && run.Summary.CompletionRate.Denominator == nil &&
		d.Latency.P50 == nil && d.SampledPeakRSSBytes == nil {
		ui.Text(c, tr(c, "reports.metrics.no_summary")).TextColor(c.Theme().TextMuted)
	}
}

func reportThroughput(c *ui.Context, run core.ReportRun) {
	throughput := run.Dashboard.Throughput
	card(c, tr(c, "reports.throughput.title"), func() {
		if throughput == nil {
			ui.Text(c, tr(c, "reports.metrics.not_measured")).TextColor(c.Theme().TextMuted)
		} else {
			deviceRow(c, tr(c, "reports.throughput.requests_per_sec"), reportPerSecondFor(c, throughput.RequestsPerSecond, throughput.RequestsPerSecondReason))
			deviceRow(c, tr(c, "reports.throughput.output_tokens_per_sec"), reportPerSecondFor(c, throughput.OutputTokensPerSecond, throughput.OutputTokensPerSecondReason))
			deviceRow(c, tr(c, "reports.throughput.goodput_per_sec"), reportPerSecondFor(c, throughput.GoodputPerSecond, throughput.GoodputPerSecondReason))
		}
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
	intervals := run.Dashboard.OutputBlockIntervals
	card(c, tr(c, "reports.intervals.title"), func() {
		if intervals == nil {
			ui.Text(c, tr(c, "reports.metrics.not_measured")).TextColor(c.Theme().TextMuted)
			return
		}
		deviceRow(c, tr(c, "reports.intervals.median"), reportMilliseconds(intervals.P50))
		deviceRow(c, "P95", reportIntervalP95For(c, intervals))
		if reason := reportIntervalReasonFor(c, intervals.Reason); reason != "" {
			ui.Text(c, reason).TextColor(c.Theme().TextMuted)
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
