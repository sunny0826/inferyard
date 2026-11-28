package pages

import (
	"strings"
	"testing"

	"inferyard/studio/internal/core"
)

func TestCompareFormattingKeepsMissingDistinctFromZero(t *testing.T) {
	var metrics *core.ReportCompareMetrics
	if got := formatCompareNumber(metricValue(metrics, "temperature"), "°C"); got != compareMissing {
		t.Fatalf("missing temperature = %q, want em dash", got)
	}
	zero := 0.0
	metrics = &core.ReportCompareMetrics{PeakTemperatureCelsius: &zero}
	if got := formatCompareNumber(metricValue(metrics, "temperature"), "°C"); got != "0.0 °C" {
		t.Fatalf("real zero temperature = %q", got)
	}
	if got := formatCompareBytes(nil); got != compareMissing || strings.Contains(got, "0") {
		t.Fatalf("missing bytes = %q", got)
	}
}

func TestCompareQualityAndRequestMissing(t *testing.T) {
	if got := formatCompareQuality(core.ReportCompareMetrics{}); got != compareMissing {
		t.Fatalf("missing quality = %q", got)
	}
	if got := compareRequestSummary(nil); got != compareMissing {
		t.Fatalf("missing request = %q", got)
	}
}

func TestCompareMetricsKeepLatencyTTFTAndOutputDistinct(t *testing.T) {
	latency, ttft, decode, output := 900.0, 80.0, 25.0, 30.0
	metrics := &core.ReportCompareMetrics{
		LatencyP50Ms: &latency, TTFTP50MS: &ttft,
		DecodeTokensPerSecond: &decode, OutputTokensPerSecond: &output,
	}
	if got := metricValue(metrics, "latency_p50"); got != &latency {
		t.Fatalf("request latency = %v", got)
	}
	if got := metricValue(metrics, "ttft_p50"); got != &ttft {
		t.Fatalf("TTFT = %v", got)
	}
	if got := metricValue(metrics, "output"); got != &output {
		t.Fatalf("output throughput = %v", got)
	}
	metrics.OutputTokensPerSecond = nil
	if got := metricValue(metrics, "output"); got != &decode {
		t.Fatalf("output fallback = %v", got)
	}
	metrics.TTFTP50MS = nil
	if got := formatCompareNumber(metricValue(metrics, "ttft_p50"), "ms"); got != compareMissing {
		t.Fatalf("missing TTFT = %q", got)
	}
	if got := compareModelName("RightModel", "Fallback"); got != "RightModel" {
		t.Fatalf("model name = %q", got)
	}
}
