package pages

import (
	"fmt"

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

func reportRate(rate core.ReportRate) string {
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
	if d.Quality.Complete != nil && !*d.Quality.Complete {
		qualityNote += " · 仅已记录通过数"
	}
	metrics := []struct{ label, value, detail, note string }{
		{"答案通过数", reportRate(d.Quality), qualityNote, "质量排除数 " + reportNumber(d.Quality.Excluded)},
		{"请求完成率", reportRate(run.Summary.CompletionRate), "完成 / 有效执行", "排除 " + reportNumber(run.Summary.CompletionRate.Excluded)},
		{"完成请求中位耗时", reportScaled(d.Latency.P50, 1000, "s"), reportNumber(d.Latency.SampleCount) + " 个有效样本", "排除 " + reportNumber(d.Latency.Excluded)},
		{"服务采样峰值 RSS", reportScaled(d.SampledPeakRSSBytes, 1<<30, "GiB"), "系统最低可用 " + reportScaled(d.SampledMinimumAvailableBytes, 1<<30, "GiB"), reportMemoryReason(d.SampledPeakRSSReason)},
	}
	columns := 4
	if compactLayout(c) {
		columns = 1
	}
	ui.Grid(c).Columns(columns).Gap(c.Theme().Space(3)).Children(func() {
		for _, metric := range metrics {
			ui.Column(c).MinWidth(0).Gap(c.Theme().Space(1)).Padding(c.Theme().Space(3.5), c.Theme().Space(4)).Background(c.Theme().Surface).Radius(c.Theme().Space(3)).Children(func() {
				ui.Text(c, metric.label)
				ui.Text(c, metric.value).Bold().FontSize(c.Theme().Rem(1.7)).Selectable()
				ui.Text(c, metric.detail).TextColor(c.Theme().TextMuted)
				if metric.note != "" {
					ui.Text(c, metric.note).TextColor(c.Theme().TextMuted)
				}
			})
		}
	})
	if reason := reportMemoryReason(d.MinimumAvailableReason); reason != "" {
		ui.Text(c, "系统可用内存："+reason).TextColor(c.Theme().TextMuted)
	}
}
