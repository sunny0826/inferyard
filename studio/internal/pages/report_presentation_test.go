package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func reportFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/report-index.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reportFixtureService(t *testing.T) (*core.Service, string) {
	t.Helper()
	root := t.TempDir()
	results := filepath.Join(root, "results")
	dir := filepath.Join(root, "reports", "summary-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), reportFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{ResultsRoot: &results})
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

func TestCompletedTaskOpensItsReport(t *testing.T) {
	for _, restored := range []bool{false, true} {
		s := compactRunFixture()
		service, path := reportFixtureService(t)
		s.service, s.tasks = service, nil
		s.task.stage, s.task.report = "done", path
		if restored {
			s.task = benchmarkTask{}
			s.progress.NextAction = "done"
			s.progress.Report = inferyard.ProgressReport{Status: "verified", Path: &path}
		}
		tt := ui.NewTester(s.view, 1120, 800)
		if tt.HasText(path) || tt.HasText("任务报告已生成") {
			t.Error("completion should only offer the report button", tt.Texts())
		}
		buttonBounds, ok := tt.Find("查看报告")
		if !ok || buttonBounds.W > 240 {
			t.Error("missing or stretched report button", buttonBounds)
		}
		click(t, tt, "查看报告")
		if s.selected != "reports" || s.openedReport == nil || !tt.HasText("Fixture Model Q4") || tt.HasText("历史任务") {
			t.Fatal("completion opened the report menu instead of the generated report", tt.Texts())
		}
		click(t, tt, "返回报告列表")
		if !tt.HasText("历史任务") {
			t.Fatal("cannot return from report to list")
		}
	}
}

func TestReportPrototypeSectionsAndValues(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Gap(16).Children(func() { s.reportView(c) })
	}, 1120, 2200)
	for _, want := range []string{"Fixture Model Q4", "答案通过数", "请求完成率", "完成请求中位耗时", "服务采样峰值 RSS", "7 / 13", "11 / 13", "1.24 s", "1.80 GiB", "系统最低可用 9.40 GiB", "各类任务表现", "3 / 8", "4 / 5", "模型与机器", "机器：Fixture CPU · 14 核 · 24.00 GiB", "引擎：llama-server · Metal · 8 线程", "逐题答案", "数据来源", "运行：fixture-run", "0.84 s", "1.20 s", "通过", "未通过", "—"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, label := range tt.Texts() {
		if strings.Contains(label, "/private/") {
			t.Errorf("report reveals a local path: %s", label)
		}
	}
	previous := float32(-1)
	for _, label := range []string{"答案通过数", "各类任务表现", "模型与机器", "逐题答案", "数据来源"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y <= previous {
			t.Errorf("section %s is missing or out of order", label)
		}
		previous = bounds.Y
	}
}

func TestReportAbsolutePathCannotOpenAnotherReport(t *testing.T) {
	service, path := reportFixtureService(t)
	if _, err := service.ReportIndex(path); err != nil {
		t.Error("generated report path did not open", err)
	}
	outside := filepath.Join(t.TempDir(), filepath.Base(path))
	if _, err := service.ReportIndex(outside); err == nil {
		t.Fatal("outside report path silently opened the same name inside reports root")
	}
	escape := filepath.Join(filepath.Dir(path), "escape")
	if err := os.Symlink(filepath.Dir(outside), escape); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if _, err := service.ReportIndex(escape); err == nil {
		t.Fatal("report path accepted a symlink outside reports root")
	}
}

func TestReportRunSelectionKeepsMetricsSeparate(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	var second core.ReportIndex
	if err := json.Unmarshal(reportFixture(t), &second); err != nil {
		t.Fatal(err)
	}
	run := second.Runs[0]
	run.Source.RunID, run.Profile.Title, run.Facets.BundleLabel = "second-run", "Second Model", "第二题包"
	run.Dashboard.Quality.Numerator = ptr(2.0)
	run.Diagnostic = true
	run.Requests = nil
	s.openedReport.index.Runs = append(s.openedReport.index.Runs, run)
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportView(c) }) }, 1120, 2200)
	click(t, tt, "选择运行")
	click(t, tt, "第二题包 · second-run")
	tt.Frame()
	for _, want := range []string{"Second Model", "2 / 13", "运行：second-run", "诊断运行", "暂无逐题数据。"} {
		if !tt.HasText(want) {
			t.Errorf("missing %s after run switch: %v", want, tt.Texts())
		}
	}
	if tt.HasText("7 / 13") || tt.HasText("timed-case") {
		t.Fatal("previous run leaked into selected run")
	}
}

func TestReportTablesFillCardsAndScroll(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.openReport("summary-1")
	run := &s.openedReport.index.Runs[0]
	for i := range 30 {
		run.Requests = append(run.Requests, core.ReportRequest{CaseID: fmt.Sprintf("extra-%02d", i)})
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Children(func() { s.reportView(c) }) }, 1120, 2400)
	tt.Frame()
	table, ok := tt.Find("质量分类")
	if !ok || table.W < 1000 {
		t.Fatalf("table does not fill card: %+v", table)
	}
	requests, ok := tt.Find("逐题答案表")
	if !ok {
		t.Fatal("request table missing")
	}
	first, _ := tt.Find("null-case")
	tt.Scroll(requests.X+30, requests.Y+80, 0, 800)
	after, _ := tt.Find("null-case")
	if after.Y >= first.Y {
		t.Fatal("long request table did not scroll")
	}
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
	for _, want := range []string{"缺少资源采样", "系统最低可用 —", "系统可用内存：多个采样来源，未合并"} {
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
