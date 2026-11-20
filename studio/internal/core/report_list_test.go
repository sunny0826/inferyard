package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func reportListService(t *testing.T, root, results string) *Service {
	t.Helper()
	settings := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(UserSettings{ResultsRoot: &results})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), Options{
		SettingsPath: settings,
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			panic("listing reports must not run CLI commands")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return service
}

func writeReportFixture(t *testing.T, dir string, stamp time.Time) {
	t.Helper()
	raw, err := os.ReadFile("../inferyard/testdata/studio-report.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "studio.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if !stamp.IsZero() {
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReportSummaryProjectsRunsAndFileTime(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	stamp := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	writeReportFixture(t, filepath.Join(root, "reports", "projected"), stamp)
	report := onlyReport(t, root, results)
	assertProjectedRun(t, report, stamp)
}

func onlyReport(t *testing.T, root, results string) ReportSummary {
	t.Helper()
	service := reportListService(t, root, results)
	_, reports, apiErr := service.Reports()
	if apiErr != nil || len(reports) != 1 {
		t.Fatalf("%v %v", reports, apiErr)
	}
	return reports[0]
}

func assertProjectedRun(t *testing.T, report ReportSummary, stamp time.Time) {
	t.Helper()
	if report.Unreadable {
		t.Fatal("readable report marked unreadable")
	}
	if report.ModifiedAt == nil || *report.ModifiedAt != stamp.Format(time.RFC3339Nano) {
		t.Fatalf("modified at %v, want %v", report.ModifiedAt, stamp)
	}
	if len(report.RunSummaries) != 1 {
		t.Fatalf("run summaries %+v", report.RunSummaries)
	}
	run := report.RunSummaries[0]
	if run.Model == "" || run.RunID != "fixture-run" {
		t.Fatalf("projection %+v", run)
	}
	if run.Quality.Denominator == nil || *run.Quality.Denominator == 0 {
		t.Fatalf("quality not projected: %+v", run.Quality)
	}
}
