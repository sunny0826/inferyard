//go:build ui_debug && !release

package pages

import (
	"testing"

	"inferyard/studio/internal/core"
)

func TestPreviewReportMetricsFollowRunRequests(t *testing.T) {
	run := previewReportFixture("").Runs[0]
	run.Requests = []core.ReportRequest{
		{CaseID: "case-a", State: "completed", LatencyMS: previewValue(100.0)},
		{CaseID: "case-b", State: "completed", LatencyMS: previewValue(300.0)},
		{CaseID: "case-c", State: "completed", LatencyMS: previewValue(200.0)},
		{CaseID: "case-d", State: "failed"},
	}
	detail := &core.RunTaskDetail{Resources: []core.ResourceSample{
		{Source: core.ResourceSource{Metric: "service_rss", Unit: "bytes"}, Value: previewValue(float64(3 << 30))},
		{Source: core.ResourceSource{Metric: "system_mem_available", Unit: "bytes"}, Value: previewValue(float64(5 << 30))},
	}}
	previewReportMetrics(&run, detail)
	d := run.Dashboard
	if d.Latency.SampleCount == nil || *d.Latency.SampleCount != 3 {
		t.Fatalf("latency sample count must follow requests, got %v", reportNumber(d.Latency.SampleCount))
	}
	if d.Latency.Excluded == nil || *d.Latency.Excluded != 1 {
		t.Fatalf("latency excluded must count requests without latency, got %v", reportNumber(d.Latency.Excluded))
	}
	if d.Latency.P50 == nil || *d.Latency.P50 != 200 {
		t.Fatalf("median must follow request latencies, got %v", reportNumber(d.Latency.P50))
	}
	if d.Latency.P95 == nil || *d.Latency.P95 != 300 {
		t.Fatalf("p95 must follow request latencies, got %v", reportNumber(d.Latency.P95))
	}
	if d.SampledPeakRSSBytes == nil || *d.SampledPeakRSSBytes != 3<<30 {
		t.Fatalf("peak RSS must follow resource samples, got %v", reportNumber(d.SampledPeakRSSBytes))
	}
	if d.SampledMinimumAvailableBytes == nil || *d.SampledMinimumAvailableBytes != 5<<30 {
		t.Fatalf("minimum available memory must follow resource samples, got %v", reportNumber(d.SampledMinimumAvailableBytes))
	}
}
