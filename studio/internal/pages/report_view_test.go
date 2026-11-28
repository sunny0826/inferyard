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

func TestReportViewReadsStudioJSONOnly(t *testing.T) {
	root := t.TempDir()
	fixture := string(reportFixture(t))
	for _, id := range []string{"good", "bad"} {
		dir := filepath.Join(root, "reports", id)
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		raw := fixture
		if id == "bad" {
			raw = "{broken"
		}
		if e := os.WriteFile(filepath.Join(dir, "studio.json"), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	legacy := filepath.Join(root, "reports", "legacy")
	if e := os.MkdirAll(legacy, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"report.html", "index.json"} {
		if e := os.WriteFile(filepath.Join(legacy, name), []byte("retired "+name), 0600); e != nil {
			t.Fatal(e)
		}
	}
	settings := filepath.Join(root, "settings.json")
	noCLI := filepath.Join(root, "no-cli")
	raw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: &root, InferyardPath: &noCLI})
	if e := os.WriteFile(settings, raw, 0600); e != nil {
		t.Fatal(e)
	}
	service, e := core.New(context.Background(), core.Options{SettingsPath: settings, Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
		panic("report view must not call CLI")
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Shutdown(context.Background())
	s := testState()
	s.selected = "reports"
	s.service = service
	_, s.reports, s.reportsError = service.Reports()
	if s.reportsError != nil || len(s.reports) != 2 {
		t.Fatal("old report directory was listed", s.reportsError, s.reports)
	}
	if _, e := service.ReportIndex("legacy"); e == nil || e.Code != "unsupported_report_format" {
		t.Fatal("legacy report did not explain its unsupported format", e)
	}
	tt := ui.NewTester(s.view, 1120, 1200)
	if !tt.HasText("Fixture Model Q4") || !tt.HasText("无法读取") || tt.HasText("legacy") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "打开报告 · good")
	tt.Frame()
	for _, want := range []string{"数学计算", "知识问答", "2 个有效样本", "排除 1", "null-case", "timed-case"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %s: %v", want, tt.Texts())
		}
	}
	click(t, tt, "返回报告列表")
	click(t, tt, "打开报告 · bad")
	tt.Frame()
	if !tt.HasText("报告 bad") || !tt.HasText("无法读取") {
		t.Fatal(tt.Texts())
	}
	if e := os.WriteFile(filepath.Join(root, "reports", "bad", "studio.json"), []byte(fixture), 0600); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "重新读取报告")
	if _, ok := tt.Find("53.8%"); !ok || !tt.HasText("7 / 13 · 通过 / 评分题") || !tt.HasText("9.40\u00a0GiB") {
		t.Fatal(tt.Texts())
	}
	for _, name := range []string{"report.html", "index.json"} {
		raw, e := os.ReadFile(filepath.Join(legacy, name))
		if e != nil || string(raw) != "retired "+name {
			t.Fatal("legacy report bytes changed")
		}
	}
	if _, e := service.ReportIndex("../outside"); e == nil {
		t.Fatal("accepted traversal")
	}
}

func TestReportHeaderBackReturnsToList(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		report      reportViewState
	}{
		{"loading", "报告 sample", reportViewState{loading: true}},
		{"failure", "报告 sample", reportViewState{failure: core.Err("not_found")}},
		{"empty", "报告 sample", reportViewState{index: &core.ReportIndex{}}},
		{"ready", "Model", reportViewState{index: &core.ReportIndex{Runs: []core.ReportRun{{Model: "Model"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testState()
			s.selected = "reports"
			tc.report.reference = "sample"
			s.openedReport, s.reportsTab = &tc.report, reportsTabSpeed
			tt := ui.NewTester(s.view, 1120, 1200)
			title, hasTitle := tt.AccessByRole(ui.RoleHeading, tc.title)
			backLabel := tr(nil, "reports.detail.back")
			back, hasBack := tt.AccessByRole(ui.RoleButton, backLabel)
			if !hasTitle || !hasBack || back.Bounds.X+back.Bounds.W > title.Bounds.X || back.Bounds.X+back.Bounds.W > 205 {
				t.Fatalf("unexpected left-sidebar back placement: title=%+v back=%+v", title.Bounds, back.Bounds)
			}
			click(t, tt, backLabel)
			if s.openedReport != nil || s.reportsTab != reportsTabList {
				t.Fatal("back did not close the report and restore the list")
			}
		})
	}
}
