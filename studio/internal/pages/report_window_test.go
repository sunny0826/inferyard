package pages

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestReportShellLoadFailureAndRetryPreserveHTML(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	dir := filepath.Join(root, "reports", "offline-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("<!doctype html><title>Offline fixture</title><p>original report</p>")
	file := filepath.Join(dir, "report.html")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(core.UserSettings{ResultsRoot: &results})
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := core.New(context.Background(), core.Options{SettingsPath: config, Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
		panic("report fixture must not execute CLI")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())
	reports := NewReports(service)
	content := &reportContent{id: "offline-fixture"}
	content.failure = reports.loadReport(content.id, func(_ string) error { return errors.New("private native diagnostic") })
	if content.failure == nil || content.failure.Code != "report_open_failed" {
		t.Fatal("load failure not mapped")
	}
	var loaded string
	content.retry = func() {
		content.failure = reports.loadReport(content.id, func(value string) error { loaded = value; return nil })
	}
	closed := false
	content.close = func() { closed = true }
	tt := ui.NewTester(content.view, 800, 600)
	if tt.HasText("private native diagnostic") || !tt.HasText("重新加载报告") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "重新加载报告")
	u, err := url.Parse(loaded)
	resolved, resolveError := filepath.EvalSymlinks(file)
	if err != nil || resolveError != nil || u.Scheme != "file" || u.Path != resolved || content.failure != nil {
		t.Fatal("retry did not load validated local file", loaded, content.failure)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(original) {
		t.Fatal("report bytes changed")
	}
	content.failure = reportLoadError()
	tt.Frame()
	click(t, tt, "关闭报告窗口")
	if !closed {
		t.Fatal("close action not reached")
	}
	if e := reports.loadReport("../outside", func(string) error { t.Fatal("invalid report reached loader"); return nil }); e == nil {
		t.Fatal("traversal accepted")
	}
	if len(reports.windows) != 0 {
		t.Fatal("fixture opened native windows")
	}
}

func TestReportShellErrorLayout(t *testing.T) {
	content := &reportContent{id: "report-fixture", failure: reportLoadError(), retry: func() {}, close: func() {}}
	for _, size := range [][2]int{{1120, 800}, {800, 600}} {
		tt := ui.NewTester(content.view, size[0], size[1])
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			for _, scale := range []float32{1, 1.5, 2} {
				tt.SetPreferences(ui.Preferences{TextScale: scale, HighContrast: true, ReduceMotion: true})
				for _, label := range []string{"报告 report-fixture", "重新加载报告", "关闭报告窗口"} {
					rect, ok := tt.Find(label)
					if !ok || rect.X < 0 || rect.X+rect.W > float32(size[0]) || rect.Y+rect.H > float32(size[1]) {
						t.Fatal("report shell clipped", label, rect)
					}
				}
			}
		}
	}
}
