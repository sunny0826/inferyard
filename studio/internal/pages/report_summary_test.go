package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func renderFactReport(t *testing.T, mutate func(*core.ReportRun)) *ui.Tester {
	t.Helper()
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	if mutate != nil {
		mutate(&s.openedReport.index.Runs[0])
	}
	return renderReport(t, s)
}

// The summary states only the facts this run recorded, in plain language.
func TestReportFactSummaryStatesRecordedFacts(t *testing.T) {
	tt := renderFactReport(t, nil)
	for _, want := range []string{
		"13 道评分题中通过 7 道。",
		"11 / 13 个请求完成。",
		"完成请求的中位耗时 0.84 秒。",
		"采样期间服务进程内存峰值 1.80 GiB。",
		"以上只描述本次记录；机器总内存、质量与耗时分别展示，不代表模型适合其他上下文或并发。",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q: %v", want, tt.Texts())
		}
	}
}

// A missing value is omitted from the summary instead of becoming zero.
func TestReportFactSummaryOmitsMissingValues(t *testing.T) {
	tt := renderFactReport(t, func(run *core.ReportRun) {
		run.Dashboard.Latency.P50 = nil
		run.Dashboard.Quality.Numerator = nil
	})
	if !tt.HasText("11 / 13 个请求完成。") {
		t.Fatalf("existing fact dropped: %v", tt.Texts())
	}
	for _, unwanted := range []string{"中位耗时 0.00 秒", "通过 0 道", "道评分题中通过 0 道。"} {
		if tt.HasText(unwanted) {
			t.Errorf("missing measurement became %q", unwanted)
		}
	}
	if tt.HasText("完成请求的中位耗时") {
		t.Fatal("summary kept a missing latency")
	}
}

// An incomplete quality section says so; it never presents partial counts as a
// complete result.
func TestReportFactSummaryMarksIncompleteQuality(t *testing.T) {
	tt := renderFactReport(t, func(run *core.ReportRun) { run.Dashboard.Quality.Complete = false })
	if !tt.HasText("已记录的评分题中通过 7 道（质量结论不完整）。") {
		t.Fatalf("missing incomplete-quality wording: %v", tt.Texts())
	}
	if tt.HasText("13 道评分题中通过 7 道。") {
		t.Fatal("incomplete quality read as complete")
	}
}

// Nothing recorded at all is stated as such, not as an empty summary.
func TestReportFactSummaryWithoutMeasurements(t *testing.T) {
	tt := renderFactReport(t, func(run *core.ReportRun) {
		run.Dashboard.Quality.Numerator, run.Dashboard.Quality.Denominator = nil, nil
		run.Summary.CompletionRate.Numerator, run.Summary.CompletionRate.Denominator = nil, nil
		run.Dashboard.Latency.P50 = nil
		run.Dashboard.SampledPeakRSSBytes = nil
	})
	if !tt.HasText("本次运行没有可汇总的指标；下方各区说明各自缺测原因。") {
		t.Fatalf("missing empty-summary wording: %v", tt.Texts())
	}
}

// The report never asserts fitness, a composite score, or an estimate the
// evidence cannot support.
func TestReportAvoidsFitnessAndCompositeClaims(t *testing.T) {
	tt := renderFactReport(t, nil)
	for _, banned := range []string{"综合评分：", "适合你的机器", "推荐使用", "预计耗时", "总体得分"} {
		if tt.HasText(banned) {
			t.Errorf("report claims %q", banned)
		}
	}
	for _, want := range []string{"达标请求使用本次预先固定的门槛；未预注册门槛时无法判定。", "约 95% 的已观测间隔不超过 P95。"} {
		if !tt.HasText(want) {
			t.Errorf("missing scope note %q", want)
		}
	}
}
