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

// reportListService lists from the report root without running the CLI.
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
	t.Cleanup(func() { service.Shutdown(context.Background()) })
	return service
}

// writeReportFixture writes the bundled report under one directory, optionally
// backdating its file time so ordering is deterministic.
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

// The list projection comes from the same studio.json parse: model, bundle and
// quality per run, with the file modification time rather than a run or
// generation time.
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

// An unreadable report stays listed with its file time and flag; it does not
// hide its neighbours.
func TestReportSummaryKeepsUnreadableEntry(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	writeReportFixture(t, filepath.Join(root, "reports", "good"), time.Time{})
	writeBrokenReport(t, filepath.Join(root, "reports", "bad"))
	service := reportListService(t, root, results)
	_, reports, apiErr := service.Reports()
	if apiErr != nil || len(reports) != 2 {
		t.Fatalf("%v %v", reports, apiErr)
	}
	broken := reports[reportIndex(reports, "bad")]
	if !broken.Unreadable || len(broken.RunSummaries) != 0 || broken.ModifiedAt == nil {
		t.Fatalf("unreadable projection %+v", broken)
	}
}

func writeBrokenReport(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "studio.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
}

func reportIndex(reports []ReportSummary, id string) int {
	for i := range reports {
		if reports[i].ReportId == id {
			return i
		}
	}
	return -1
}

// The summary sorts by the file time it reports, newest first.
func TestReportSummarySortsByFileTime(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	times := map[string]time.Time{
		"older": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		"newer": time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	for id, stamp := range times {
		writeReportFixture(t, filepath.Join(root, "reports", id), stamp)
	}
	service := reportListService(t, root, results)
	_, reports, apiErr := service.Reports()
	if apiErr != nil || len(reports) != 2 {
		t.Fatalf("%v %v", reports, apiErr)
	}
	if reports[0].ReportId != "newer" || reports[1].ReportId != "older" {
		t.Fatalf("order %s, %s", reports[0].ReportId, reports[1].ReportId)
	}
}
