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
	results := filepath.Join(root, "results")
	dir := filepath.Join(root, "reports", "summary-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "studio.json"), reportFixture(t), 0600); err != nil {
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
	for _, want := range []string{"缺少资源采样", "采样期间系统最少可用 —", "系统可用内存：多个采样来源，未合并"} {
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
