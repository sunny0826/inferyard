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
	qualityNote := ""
	if !d.Quality.Complete {
		qualityNote = "仅已记录通过数"
	}
	metrics := []struct{ label, value, detail, note string }{
		{"答案通过", reportPassRate(d.Quality), "通过 / 评分题", qualityNote},
		{"请求完成", reportRate(run.Summary.CompletionRate), "完成 / 发出", "排除 " + reportNumber(run.Summary.CompletionRate.Excluded)},
		{"完成请求耗时（中位）", reportScaled(d.Latency.P50, 1000, "s"), reportNumber(d.Latency.SampleCount) + " 个有效样本", "排除 " + reportNumber(d.Latency.Excluded)},
		{"服务进程内存（采样峰值）", reportScaled(d.SampledPeakRSSBytes, 1<<30, "GiB"), "采样期间系统最少可用 " + reportScaled(d.SampledMinimumAvailableBytes, 1<<30, "GiB"), reportMemoryReason(d.SampledPeakRSSReason)},
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
		ui.Text(c, "本次运行暂无可汇总的指标。").TextColor(c.Theme().TextMuted)
	}
}

func reportThroughput(c *ui.Context, run core.ReportRun) {
	throughput := run.Dashboard.Throughput
	card(c, "吞吐", func() {
		if throughput == nil {
			ui.Text(c, "未提供测量").TextColor(c.Theme().TextMuted)
		} else {
			deviceRow(c, "完成请求/秒", reportPerSecond(throughput.RequestsPerSecond, throughput.RequestsPerSecondReason))
			deviceRow(c, "输出 token/秒", reportPerSecond(throughput.OutputTokensPerSecond, throughput.OutputTokensPerSecondReason))
			deviceRow(c, "达标请求/秒", reportPerSecond(throughput.GoodputPerSecond, throughput.GoodputPerSecondReason))
		}
	})
}

func reportPerSecond(value *float64, reason *string) string {
	if value == nil {
		return reportThroughputReason(reason)
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

func reportOutputIntervals(c *ui.Context, run core.ReportRun) {
	intervals := run.Dashboard.OutputBlockIntervals
	card(c, "输出内容块到达间隔", func() {
		if intervals == nil {
			ui.Text(c, "未提供测量").TextColor(c.Theme().TextMuted)
			return
		}
		deviceRow(c, "中位", reportMilliseconds("", intervals.P50, ""))
		deviceRow(c, "P95", reportIntervalP95(intervals))
		if reason := reportIntervalReason(intervals.Reason); reason != "" {
			ui.Text(c, "缺测原因："+reason).TextColor(c.Theme().TextMuted)
		}
		componentFoldKeyed(c, "interval-details", "间隔详情", func() {
			deviceRow(c, "间隔数", fmt.Sprintf("%d 个", intervals.IntervalCount))
			deviceRow(c, "贡献请求数", fmt.Sprintf("%d 个请求", intervals.RequestCount))
			deviceRow(c, "最小 / 最大", reportMilliseconds("min", intervals.Min, "")+" · "+reportMilliseconds("max", intervals.Max, ""))
		})
	})
}

func reportIntervalP95(intervals *core.ReportOutputBlockIntervals) string {
	value := reportMilliseconds("", intervals.P95, reportP95Reason(intervals.P95Reason))
	if intervals.P95 != nil && intervals.IntervalCount >= 20 && intervals.IntervalCount < 100 {
		return fmt.Sprintf("%s（n=%d）", value, intervals.IntervalCount)
	}
	return value
}

func reportMilliseconds(label string, value *float64, missing string) string {
	if label != "" {
		label += " "
	}
	if value == nil {
		return label + "—" + missing
	}
	return fmt.Sprintf("%s%.2f ms", label, *value)
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
