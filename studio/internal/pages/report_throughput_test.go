package pages

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func renderReport(t *testing.T, s viewState) *ui.Tester {
	t.Helper()
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.reportView(c) })
	}, 1120, 3200)
}

func TestReportThroughputAndIntervalCards(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	tt := renderReport(t, s)
	for _, want := range []string{
		"吞吐", "请求/秒", "输出 token/秒", "goodput/秒",
		"1.5", "12.25", "0（已测）",
		"串行批次口径，不代表并发服务能力",
		"输出间隔", "内容块到达间隔，非逐 token ITL",
		"25 个", "2 个请求",
		"min 0 ms", "p50 120.5 ms", "p95 400 ms", "max 900.25 ms",
		"逐 token ITL 未测：无逐 token 事件源",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q: %v", want, tt.Texts())
		}
	}
}

func TestReportThroughputMissingReasonsAndZero(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	run := &s.openedReport.index.Runs[0]
	run.Dashboard.Throughput = &core.ReportThroughput{
		RequestsPerSecondReason:     ptr("terminal_window_unavailable"),
		OutputTokensPerSecondReason: ptr("output_token_counts_incomplete"),
		GoodputPerSecondReason:      ptr("goodput_indeterminable"),
	}
	run.Dashboard.OutputBlockIntervals = &core.ReportOutputBlockIntervals{
		IntervalCount: 0,
		RequestCount:  1,
		P95Reason:     ptr("insufficient_samples"),
		Reason:        ptr("not_streaming"),
	}
	tt := renderReport(t, s)
	for _, want := range []string{
		"无可用终态窗口", "输出 token 计数不完整", "无法判定（不可判请求存在）",
		"0 个", "1 个请求", "缺测原因：非流式请求", "p95 —（样本少于 20）", "min —", "max —",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"0（已测）", "0 ms"} {
		if tt.HasText(unwanted) {
			t.Errorf("missing measurement rendered as a value: %q", unwanted)
		}
	}
}

func TestReportWithoutOptionalSectionsShowsUnmeasured(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	run := &s.openedReport.index.Runs[0]
	run.Dashboard.Throughput = nil
	run.Dashboard.OutputBlockIntervals = nil
	tt := renderReport(t, s)
	count := 0
	for _, text := range tt.Texts() {
		if text == "未提供测量" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("both optional cards must report missing measurement, got %d: %v", count, tt.Texts())
	}
	for _, want := range []string{"串行批次口径，不代表并发服务能力", "内容块到达间隔，非逐 token ITL", "逐 token ITL 未测：无逐 token 事件源"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q", want)
		}
	}
	if tt.HasText("1.5") || tt.HasText("25 个") {
		t.Fatal("absent sections rendered measurements")
	}
}

func TestReportOutputIntervalP95Exploration(t *testing.T) {
	cases := []struct {
		count int64
		p95   *float64
		want  bool
	}{
		{19, nil, false},
		{20, ptr(400.0), true},
		{99, ptr(400.0), true},
		{100, ptr(400.0), false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d samples", tc.count), func(t *testing.T) {
			s := testState()
			s.service, _ = reportFixtureService(t)
			s.openReport("summary-1")
			intervals := s.openedReport.index.Runs[0].Dashboard.OutputBlockIntervals
			intervals.IntervalCount, intervals.P95, intervals.P95Reason = tc.count, tc.p95, nil
			tt := renderReport(t, s)
			if got := tt.HasText("p95 为探索性估计（样本少于 100）"); got != tc.want {
				t.Fatalf("exploratory note = %v, want %v: %v", got, tc.want, tt.Texts())
			}
		})
	}
}

func TestReportRunSelectionKeepsThroughputSeparate(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	second := s.openedReport.index.Runs[0]
	second.Source.RunID, second.Profile.Title, second.Facets.BundleLabel = "second-run", ptr("Second Model"), "第二题包"
	second.Dashboard.Throughput = &core.ReportThroughput{
		RequestsPerSecond:     ptr(3.25),
		OutputTokensPerSecond: ptr(64.5),
		GoodputPerSecond:      ptr(2.5),
	}
	second.Dashboard.OutputBlockIntervals = &core.ReportOutputBlockIntervals{
		IntervalCount: 25,
		RequestCount:  4,
		Min:           ptr(10.0),
		P50:           ptr(20.0),
		P95:           ptr(30.0),
		Max:           ptr(40.0),
	}
	s.openedReport.index.Runs = append(s.openedReport.index.Runs, second)
	tt := renderReport(t, s)
	if !tt.HasText("1.5") {
		t.Fatalf("first run throughput missing: %v", tt.Texts())
	}
	click(t, tt, "选择运行")
	click(t, tt, "第二题包 · second-run")
	tt.Frame()
	for _, want := range []string{"Second Model", "3.25", "64.5", "2.5", "4 个请求", "p95 30 ms"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q after run switch: %v", want, tt.Texts())
		}
	}
	if tt.HasText("1.5") || tt.HasText("2 个请求") {
		t.Fatal("previous run throughput leaked into the selected run")
	}
}
