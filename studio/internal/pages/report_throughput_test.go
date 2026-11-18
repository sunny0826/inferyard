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
		"吞吐", "完成请求/秒", "输出 token/秒", "达标请求/秒",
		"1.5", "12.25", "0（已测）",
		"依次执行本批请求的观测值，不代表并发服务能力",
		"输出内容块到达间隔", "内容块不等于真实 token；没有逐 token 事件源，因此未测逐 token 间隔。",
		"中位", "120.5 ms", "P95", "400 ms", "约 95% 的已观测间隔不超过 P95。",
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
		"缺测原因：非流式请求", "P95", "—（样本少于 20）",
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
	for _, want := range []string{"依次执行本批请求的观测值，不代表并发服务能力", "内容块不等于真实 token；没有逐 token 事件源，因此未测逐 token 间隔。"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q", want)
		}
	}
	if tt.HasText("1.5") || tt.HasText("120.5 ms") {
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
			if got := tt.HasText("样本较少，仅供观察。"); got != tc.want {
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
	for _, want := range []string{"Second Model", "3.25", "64.5", "2.5", "20 ms", "30 ms"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q after run switch: %v", want, tt.Texts())
		}
	}
	if tt.HasText("1.5") || tt.HasText("120.5 ms") {
		t.Fatal("previous run throughput leaked into the selected run")
	}
}
