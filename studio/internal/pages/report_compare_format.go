package pages

import (
	"fmt"
	"strings"

	"inferyard/studio/internal/core"
)

const compareMissing = "—"

func ptrMetrics(metrics *core.ReportCompareMetrics) core.ReportCompareMetrics {
	if metrics == nil {
		return core.ReportCompareMetrics{}
	}
	return *metrics
}

func metricValue(metrics *core.ReportCompareMetrics, name string) *float64 {
	if metrics == nil {
		return nil
	}
	switch name {
	case "ttft_p50":
		return metrics.TTFTP50MS
	case "ttft_p95":
		return metrics.TTFTP95MS
	case "decode":
		return metrics.DecodeTokensPerSecond
	case "memory":
		return metrics.PeakMemoryBytes
	case "working_set":
		return metrics.PeakWorkingSetBytes
	case "temperature":
		return metrics.PeakTemperatureCelsius
	default:
		return nil
	}
}

func formatCompareQuality(metrics core.ReportCompareMetrics) string {
	if metrics.Rate == nil {
		return compareMissing
	}
	value := fmt.Sprintf("%.1f%%", *metrics.Rate*100)
	if metrics.Passed != nil && metrics.Valid != nil {
		value += fmt.Sprintf(" (%d/%d)", *metrics.Passed, *metrics.Valid)
	}
	return value
}

func formatCompareNumber(value *float64, unit string) string {
	if value == nil {
		return compareMissing
	}
	if unit != "" {
		return fmt.Sprintf("%.1f %s", *value, unit)
	}
	return fmt.Sprintf("%.1f", *value)
}

func formatCompareBytes(value *float64) string {
	if value == nil {
		return compareMissing
	}
	return fmt.Sprintf("%.1f GiB", *value/(1<<30))
}

func compareRequestSummary(request *core.ReportRequest) string {
	if request == nil {
		return compareMissing
	}
	parts := []string{}
	if request.Quality != nil && strings.TrimSpace(*request.Quality) != "" {
		parts = append(parts, *request.Quality)
	} else if request.State != "" {
		parts = append(parts, request.State)
	}
	if request.LatencyMS != nil {
		parts = append(parts, fmt.Sprintf("%.0f ms", *request.LatencyMS))
	}
	if len(parts) == 0 {
		return compareMissing
	}
	return strings.Join(parts, " · ")
}
