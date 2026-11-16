package pages

import (
	"fmt"
	"strconv"
	"strings"

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
	switch emptyText(reason) {
	case "multiple_sources":
		return "多个采样来源，未合并"
	case "resource_samples_missing":
		return "缺少资源采样"
	case "":
		return ""
	default:
		return "缺测原因：" + *reason
	}
}

func reportMetrics(c *ui.Context, run core.ReportRun) {
	d := run.Dashboard
	qualityNote := "各类别不构成综合评分"
	if !d.Quality.Complete {
		qualityNote += " · 仅已记录通过数"
	}
	metrics := []struct{ label, value, detail, note string }{
		{"答案通过数", reportRate(d.Quality.StudioCounts), qualityNote, "质量排除数 " + reportNumber(d.Quality.Excluded)},
		{"请求完成率", reportRate(run.Summary.CompletionRate), "完成 / 有效执行", "排除 " + reportNumber(run.Summary.CompletionRate.Excluded)},
		{"完成请求中位耗时", reportScaled(d.Latency.P50, 1000, "s"), reportNumber(d.Latency.SampleCount) + " 个有效样本", "排除 " + reportNumber(d.Latency.Excluded)},
		{"服务采样峰值 RSS", reportScaled(d.SampledPeakRSSBytes, 1<<30, "GiB"), "系统最低可用 " + reportScaled(d.SampledMinimumAvailableBytes, 1<<30, "GiB"), reportMemoryReason(d.SampledPeakRSSReason)},
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
	if reason := reportMemoryReason(d.MinimumAvailableReason); reason != "" {
		ui.Text(c, "系统可用内存："+reason).TextColor(c.Theme().TextMuted)
	}
}

// reportThroughput renders the batch-window rates. Null values keep their
// recorded reason; a measured zero is shown as measured, not as missing.
func reportThroughput(c *ui.Context, run core.ReportRun) {
	throughput := run.Dashboard.Throughput
	card(c, "吞吐", func() {
		if throughput == nil {
			ui.Text(c, "未提供测量").TextColor(c.Theme().TextMuted)
		} else {
			deviceRow(c, "请求/秒", reportPerSecond(throughput.RequestsPerSecond, throughput.RequestsPerSecondReason))
			deviceRow(c, "输出 token/秒", reportPerSecond(throughput.OutputTokensPerSecond, throughput.OutputTokensPerSecondReason))
			deviceRow(c, "goodput/秒", reportPerSecond(throughput.GoodputPerSecond, throughput.GoodputPerSecondReason))
		}
		ui.Text(c, "串行批次口径，不代表并发服务能力").TextColor(c.Theme().TextMuted)
	})
}

func reportPerSecond(value *float64, reason *string) string {
	if value == nil {
		return reportThroughputReason(reason)
	}
	if *value == 0 {
		return "0（已测）"
	}
	return reportDecimal(*value)
}

// reportDecimal keeps small non-zero measurements from rendering as zero.
func reportDecimal(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func reportThroughputReason(reason *string) string {
	switch emptyText(reason) {
	case "":
		return "—"
	case "no_formal_requests":
		return "无正式请求"
	case "terminal_window_unavailable":
		return "无可用终态窗口"
	case "output_token_counts_incomplete":
		return "输出 token 计数不完整"
	case "no_completed_requests":
		return "无完成请求"
	case "quality_not_applicable":
		return "质量不适用"
	case "thresholds_not_preregistered":
		return "未预注册达标阈值"
	case "goodput_indeterminable":
		return "无法判定（不可判请求存在）"
	case "observation_missing":
		return "缺少观测"
	default:
		return *reason
	}
}

// reportOutputIntervals renders pooled block-arrival intervals in milliseconds.
// They are not per-token ITLs, and the card says so.
func reportOutputIntervals(c *ui.Context, run core.ReportRun) {
	intervals := run.Dashboard.OutputBlockIntervals
	card(c, "输出间隔", func() {
		ui.Text(c, "内容块到达间隔，非逐 token ITL").TextColor(c.Theme().TextMuted)
		if intervals == nil {
			ui.Text(c, "未提供测量").TextColor(c.Theme().TextMuted)
		} else {
			deviceRow(c, "间隔数", fmt.Sprintf("%d 个", intervals.IntervalCount))
			deviceRow(c, "贡献请求数", fmt.Sprintf("%d 个请求", intervals.RequestCount))
			deviceRow(c, "间隔分位", reportIntervalQuantiles(intervals))
			if reportP95Exploratory(intervals) {
				ui.Text(c, "p95 为探索性估计（样本少于 100）").TextColor(c.Theme().TextMuted)
			}
			if reason := reportIntervalReason(intervals.Reason); reason != "" {
				ui.Text(c, "缺测原因："+reason).TextColor(c.Theme().TextMuted)
			}
		}
		ui.Text(c, "逐 token ITL 未测：无逐 token 事件源").TextColor(c.Theme().TextMuted)
	})
}

func reportIntervalQuantiles(intervals *core.ReportOutputBlockIntervals) string {
	return strings.Join([]string{
		reportMilliseconds("min", intervals.Min, ""),
		reportMilliseconds("p50", intervals.P50, ""),
		reportMilliseconds("p95", intervals.P95, reportP95Reason(intervals.P95Reason)),
		reportMilliseconds("max", intervals.Max, ""),
	}, " · ")
}

// reportP95Exploratory follows the presentation rule: twenty samples allow a
// pooled p95, but it stays exploratory below one hundred samples.
func reportP95Exploratory(intervals *core.ReportOutputBlockIntervals) bool {
	return intervals.P95 != nil && intervals.IntervalCount >= 20 && intervals.IntervalCount < 100
}

func reportMilliseconds(label string, value *float64, missing string) string {
	if value == nil {
		return label + " —" + missing
	}
	return fmt.Sprintf("%s %s ms", label, reportDecimal(*value))
}

func reportP95Reason(reason *string) string {
	if emptyText(reason) == "insufficient_samples" {
		return "（样本少于 20）"
	}
	return ""
}

func reportIntervalReason(reason *string) string {
	switch emptyText(reason) {
	case "":
		return ""
	case "no_completed_requests":
		return "无完成请求"
	case "arrival_capture_missing":
		return "缺少到达时间采集"
	case "not_streaming":
		return "非流式请求"
	case "insufficient_blocks":
		return "内容块不足"
	case "observation_missing":
		return "缺少观测"
	default:
		return *reason
	}
}
