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
	results := filepath.Join(root, "results")
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
	raw, _ := json.Marshal(core.UserSettings{ResultsRoot: &results})
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
	s.service = service
	_, s.reports, s.reportsError = service.Reports()
	if s.reportsError != nil || len(s.reports) != 2 {
		t.Fatal("old report directory was listed", s.reportsError, s.reports)
	}
	if _, e := service.ReportIndex("legacy"); e == nil || e.Code != "unsupported_report_format" {
		t.Fatal("legacy report did not explain its unsupported format", e)
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
	if !tt.HasText("good") || !tt.HasText("无法读取") || tt.HasText("legacy") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "good")
	click(t, tt, "打开报告")
	tt.Frame()
	for _, want := range []string{"各类别不构成综合评分", "数学计算", "知识问答", "2 个有效样本", "排除 1", "null-case", "timed-case", "索引", "studio.json"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %s: %v", want, tt.Texts())
		}
	}
	click(t, tt, "返回报告列表")
	click(t, tt, "bad")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("报告 bad") || !tt.HasText("无法读取") {
		t.Fatal(tt.Texts())
	}
	// Retry re-reads studio.json; no report bytes are modified by opening it.
	if e := os.WriteFile(filepath.Join(root, "reports", "bad", "studio.json"), []byte(fixture), 0600); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "重新读取报告")
	if !tt.HasText("7 / 13") {
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
