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
