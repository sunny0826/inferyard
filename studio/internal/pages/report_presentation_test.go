package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func reportFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/studio.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reportFixtureService(t *testing.T) (*core.Service, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "reports", "summary-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "studio.json"), reportFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(root, "settings.json")
	noCLI := filepath.Join(root, "no-cli")
	raw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: &root, InferyardPath: &noCLI})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := core.New(context.Background(), core.Options{
		SettingsPath: settings,
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			panic("reading a report must not run CLI commands")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Shutdown(context.Background()) })
	return service, dir
}

func TestReportMissingMeasurementsAndEmptyIndex(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	run := &s.openedReport.index.Runs[0]
	run.Dashboard.SampledPeakRSSBytes = nil
	run.Dashboard.SampledPeakRSSReason = ptr("resource_samples_missing")
	run.Dashboard.SampledMinimumAvailableBytes = nil
	run.Dashboard.MinimumAvailableReason = ptr("multiple_sources")
	run.Dashboard.Latency.P50 = nil
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportView(c) }) }, 1120, 2200)
	for _, want := range []string{"缺少资源采样", "最低可用 —", "系统可用内存：多个采样来源，未合并"} {
		if !tt.HasText(want) {
			t.Error("missing", want)
		}
	}
	if tt.HasText("0.00 s") || tt.HasText("0.00 GiB") {
		t.Fatal("missing measurement became zero")
	}
	s.openedReport.index.Runs = nil
	tt.Frame()
	if !tt.HasText("报告中暂无运行数据。") {
		t.Fatal(tt.Texts())
	}
}

func TestReportCardsKeepRunsSeparateAndNeverZeroFill(t *testing.T) {
	s := testState()
	s.reports = []core.ReportSummary{{
		ReportId: "multi", Path: "/reports/multi", RunIds: []string{"a", "b"}, RunPaths: []*string{nil, nil},
		RunSummaries: []core.ReportListRun{
			{RunID: "a", Model: "Model-A", BundleLabel: "zh-core", Quality: core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(3)), Denominator: ptr(int64(5))}, Complete: true}},
			{RunID: "b", Model: "Model-B", BundleLabel: "zh-svg-pelican"},
		},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 1200)
	for _, want := range []string{"2 次运行", "Model-A · zh-core · 答案通过 3 / 5", "Model-B · zh-svg-pelican · 答案通过未记录"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"3 / 5 · 0 / 0", "100%", "合计", "报告详情"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected report card text %q", unwanted)
		}
	}
}

func TestReportMetricsResponsiveGrid(t *testing.T) {
	var run core.ReportRun
	run.Dashboard.Quality = core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(120)), Denominator: ptr(int64(120))}, Complete: true}
	labels := []string{"answer_pass", "request_completion", "median_latency", "peak_rss"}
	for _, tc := range []struct{ width, columns int }{{1000, 1}, {1120, 2}, {1280, 4}} {
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { reportMetrics(c, run) }) }, tc.width, 1000)
		if _, ok := tt.Find("100.0%"); !ok || !tt.HasText("120 / 120 · 通过 / 评分题") {
			t.Fatalf("width %d: %v", tc.width, tt.Texts())
		}
		var previous ui.Rect
		for i, label := range labels {
			pos, ok := tt.Find(tr(nil, "reports.metrics."+label))
			if !ok {
				t.Fatalf("width %d: missing %s", tc.width, label)
			}
			if i%tc.columns == 0 {
				if i > 0 && pos.Y <= previous.Y {
					t.Fatalf("width %d: expected %s on the next row", tc.width, label)
				}
			} else if pos.Y != previous.Y || pos.X <= previous.X {
				t.Fatalf("width %d: expected %s in the next column", tc.width, label)
			}
			previous = pos
		}
	}
}

func TestReportPartialScoringKeepsCounts(t *testing.T) {
	quality := core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(4)), Denominator: ptr(int64(6))}}
	value, detail := reportPassRate(nil, quality)
	if value != "4 / 6" || detail != tr(nil, "reports.metrics.answer_pass_detail") {
		t.Fatalf("unexpected partial result %q, %q", value, detail)
	}
}
