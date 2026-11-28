package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReportCompareMetricsSeparateLatencyFromTTFT(t *testing.T) {
	p50, p95, output := 500.0, 900.0, 42.0
	run := ReportRun{}
	run.Dashboard.Latency.P50, run.Dashboard.Latency.P95 = &p50, &p95
	run.Dashboard.Throughput = &ReportThroughput{OutputTokensPerSecond: &output}
	metrics := metricsForReportRun(run)
	if metrics.TTFTP50MS != nil || metrics.TTFTP95MS != nil {
		t.Fatalf("L03 latency was reported as TTFT: %+v", metrics)
	}
	if metrics.LatencyP50Ms == nil || *metrics.LatencyP50Ms != p50 || metrics.LatencyP95Ms == nil || *metrics.LatencyP95Ms != p95 {
		t.Fatalf("L03 latency missing: %+v", metrics)
	}
	if metrics.OutputTokensPerSecond == nil || *metrics.OutputTokensPerSecond != output || metrics.DecodeTokensPerSecond == nil || *metrics.DecodeTokensPerSecond != output {
		t.Fatalf("output throughput missing: %+v", metrics)
	}
}

func TestCompareReportsMatchesRunsByModelAndBundle(t *testing.T) {
	svc, workspace := newNodeTestService(t, newMockRemoteHub())
	tpsA, tpsB, tpsARight, tpsBRight := 10.0, 20.0, 40.0, 80.0
	leftA := makeReportRunFixture("run-a", "/models/A.gguf", "/opt/a", "llama.cpp", &tpsA)
	leftB := makeReportRunFixture("run-b", "/models/B.gguf", "/opt/b", "llama.cpp", &tpsB)
	rightB := makeReportRunFixture("run-b-right", `C:\models\b.gguf`, `C:\b.exe`, "llama.cpp", &tpsBRight)
	rightA := makeReportRunFixture("run-a-right", "a.GGUF", "/opt/a2", "llama.cpp", &tpsARight)
	leftA["facets"] = map[string]any{"bundle_label": "ZH-Core"}
	rightA["facets"] = map[string]any{"bundle_label": "zh-core"}
	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "left", time.Now(), []map[string]any{leftA, leftB})
	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "right", time.Now(), []map[string]any{rightB, rightA})

	comparison, apiErr := svc.CompareReports("left", "right")
	if apiErr != nil || len(comparison.Runs) != 2 {
		t.Fatalf("comparison failed: %+v err=%v", comparison, apiErr)
	}
	first, second := comparison.Runs[0], comparison.Runs[1]
	if first.LeftRunID != "run-a" || first.RightRunID != "run-a-right" || first.LeftModel != "/models/A.gguf" || first.RightModel != "a.GGUF" {
		t.Fatalf("model A runs misaligned: %+v", first)
	}
	if second.LeftRunID != "run-b" || second.RightRunID != "run-b-right" || second.LeftModel != "/models/B.gguf" || second.RightModel != `C:\models\b.gguf` {
		t.Fatalf("model B runs misaligned: %+v", second)
	}

	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "single-left", time.Now(), []map[string]any{leftA})
	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "single-right", time.Now(), []map[string]any{rightB})
	single, apiErr := svc.CompareReports("single-left", "single-right")
	if apiErr != nil || len(single.Runs) != 1 || single.Runs[0].Left == nil || single.Runs[0].Right == nil {
		t.Fatalf("single-run cross-model comparison failed: %+v err=%v", single, apiErr)
	}
	if single.Runs[0].LeftModel != "/models/A.gguf" || single.Runs[0].RightModel != `C:\models\b.gguf` {
		t.Fatalf("single-run model identities lost: %+v", single.Runs[0])
	}
}

func TestCompareReportCasesAlignsDuplicateOccurrences(t *testing.T) {
	latencies := []float64{1, 2, 3, 10, 20, 30, 4}
	request := func(caseID string, latency *float64) ReportRequest {
		return ReportRequest{CaseID: caseID, LatencyMS: latency}
	}
	left := &ReportRun{Requests: []ReportRequest{
		request("c1", &latencies[0]), request("c1", &latencies[1]), request("c2", &latencies[2]),
	}}
	left.Source.RunID = "left"
	right := &ReportRun{Requests: []ReportRequest{
		request("c1", &latencies[3]), request("c1", &latencies[4]), request("c1", &latencies[5]), request("c2", &latencies[6]),
	}}
	right.Source.RunID = "right"
	cases := compareReportCases(left, right)
	if len(cases) != 4 {
		t.Fatalf("duplicate requests dropped: %+v", cases)
	}
	for i, want := range [][2]float64{{1, 10}, {2, 20}, {3, 4}} {
		if cases[i].Left == nil || cases[i].Right == nil || *cases[i].Left.LatencyMS != want[0] || *cases[i].Right.LatencyMS != want[1] {
			t.Fatalf("occurrence %d misaligned: %+v", i, cases[i])
		}
	}
	if cases[3].Left != nil || cases[3].Right == nil || *cases[3].Right.LatencyMS != 30 {
		t.Fatalf("unmatched right occurrence lost: %+v", cases[3])
	}
}
