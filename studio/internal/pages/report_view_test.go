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
	fixture := `{"runs":[{"run_id":"fixture-run","dashboard":{"quality":{"numerator":7,"denominator":13,"excluded":2},"categories":[{"id":"math","label":"数学计算","rate":{"numerator":3,"denominator":8,"excluded":1}},{"id":"qa","label":"知识问答","rate":{"numerator":4,"denominator":5,"excluded":1}}],"latency":{"min":12,"p50":24,"p95":null,"max":41,"sample_count":9,"excluded":4}},"requests":[{"case_id":"null-case","category_label":"数学计算","state":"failed","quality":null,"latency_ms":null},{"case_id":"timed-case","category_label":"知识问答","state":"completed","quality":"pass","latency_ms":17.25}]}]}`
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
	for _, want := range []string{"报告 good", "质量 7 / 13", "各类别不构成综合评分", "数学计算 · 3 / 8 · 排除 1", "知识问答 · 4 / 5 · 排除 1", "延迟 (ms) · 最小 12 · P50 24 · P95 — · 最大 41", "延迟样本 9 · 排除 4", "null-case · 数学计算 · failed · 未知 · 延迟 (ms) —", "timed-case · 知识问答 · completed · pass · 延迟 (ms) 17.25"} {
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
	if !tt.HasText("质量 7 / 13") {
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
