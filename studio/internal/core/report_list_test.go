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

// The list projection comes from the same studio.json parse: model, bundle and
// quality per run, with the file modification time rather than a run or
// generation time.
func TestReportSummaryProjectsRunsAndFileTime(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	dir := filepath.Join(root, "reports", "projected")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../inferyard/testdata/studio-report.json")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "studio.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	service := reportListService(t, root, results)
	_, reports, apiErr := service.Reports()
	if apiErr != nil || len(reports) != 1 {
		t.Fatalf("%v %v", reports, apiErr)
	}
	report := reports[0]
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
	good := filepath.Join(root, "reports", "good")
	bad := filepath.Join(root, "reports", "bad")
	for _, dir := range []string{good, bad} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../inferyard/testdata/studio-report.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "studio.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "studio.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	service := reportListService(t, root, results)
	_, reports, apiErr := service.Reports()
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	var broken *ReportSummary
	for i := range reports {
		if reports[i].ReportId == "bad" {
			broken = &reports[i]
		}
	}
	if len(reports) != 2 || broken == nil {
		t.Fatalf("unreadable report was dropped: %+v", reports)
	}
	if !broken.Unreadable || len(broken.RunSummaries) != 0 {
		t.Fatalf("unreadable projection %+v", broken)
	}
	if broken.ModifiedAt == nil {
		t.Fatal("unreadable report lost its file time")
	}
}

// The summary sorts by the file time it reports, newest first.
func TestReportSummarySortsByFileTime(t *testing.T) {
	root := t.TempDir()
	results := filepath.Join(root, "results")
	raw, err := os.ReadFile("../inferyard/testdata/studio-report.json")
	if err != nil {
		t.Fatal(err)
	}
	times := map[string]time.Time{
		"older": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		"newer": time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	for id, stamp := range times {
		dir := filepath.Join(root, "reports", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "studio.json")
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, stamp, stamp); err != nil {
			t.Fatal(err)
		}
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
