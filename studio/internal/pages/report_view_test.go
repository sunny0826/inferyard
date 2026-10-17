package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestReportViewIndexAndUnreadable(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	fixture := string(reportFixture(t))
	for _, id := range []string{"good", "bad", "missing"} {
		dir := filepath.Join(root, "reports", id)
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, "report.html"), []byte("original HTML"), 0600); e != nil {
			t.Fatal(e)
		}
		if id == "missing" {
			continue
		}
		raw := fixture
		if id == "bad" {
			raw = "{broken"
		}
		if e := os.WriteFile(filepath.Join(dir, "index.json"), []byte(raw), 0600); e != nil {
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
	if s.reportsError != nil || len(s.reports) != 3 {
		t.Fatal("bad index failed list", s.reportsError, s.reports)
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
	if !tt.HasText("good") || !tt.HasText("无法读取") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "good")
	click(t, tt, "打开报告")
	tt.Frame()
	for _, want := range []string{"Fixture Model Q4", "7 / 13", "各类别不构成综合评分", "数学计算", "3 / 8", "知识问答", "4 / 5", "1.24 s", "9 个有效样本", "排除 4", "null-case", "—", "timed-case", "通过", "0.84 s"} {
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
	// Retry re-reads the index; no report bytes are modified by opening it.
	if e := os.WriteFile(filepath.Join(root, "reports", "bad", "index.json"), []byte(fixture), 0600); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "重新读取报告")
	if !tt.HasText("7 / 13") {
		t.Fatal(tt.Texts())
	}
	for _, id := range []string{"good", "bad", "missing"} {
		raw, e := os.ReadFile(filepath.Join(root, "reports", id, "report.html"))
		if e != nil || string(raw) != "original HTML" {
			t.Fatal("HTML changed")
		}
	}
	if _, e := service.ReportIndex("../outside"); e == nil {
		t.Fatal("accepted traversal")
	}
}

func TestReportViewHasNoWebViewPath(t *testing.T) {
	entries, e := os.ReadDir(".")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, e := os.ReadFile(entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		for _, forbidden := range []string{"WebView", "NewReports", "reportURL", "reportWindows", "report.html"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%s still reaches %s", entry.Name(), forbidden)
			}
		}
	}
}
