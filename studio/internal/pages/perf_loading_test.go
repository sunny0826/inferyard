package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestSelectTaskReadsInMemoryDetailWithoutRefreshLock(t *testing.T) {
	svc, _ := reportFixtureService(t)
	s := testState()
	s.service = svc

	// Hold refreshMu to simulate a slow background refresh in progress.
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.selectTask("missing-task")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("selectTask blocked on refreshMu")
	}
	if s.detailID != "missing-task" || s.detailError == nil || s.detailError.Code != "not_found" {
		t.Fatalf("expected immediate not_found from in-memory RunDetail, got id=%q err=%+v", s.detailID, s.detailError)
	}
}

func TestNeedsRefreshSkipsHighFrequencyInMemoryRunEvents(t *testing.T) {
	events := []core.Event{
		{ID: 1, Type: "run.progress"},
		{ID: 2, Type: "run.resources"},
		{ID: 3, Type: "services.memory"},
		{ID: 4, Type: "download.progress"},
		{ID: 5, Type: "engine.check"},
		{ID: 6, Type: "download.state", Data: json.RawMessage(`{"task":{"state":"running"}}`)},
	}
	if needsRefresh(events, false) {
		t.Fatal("high-frequency in-memory progress/resource/download/check events must not trigger refresh")
	}
	if !needsRefresh(append(events, core.Event{ID: 7, Type: "run.state"}), false) {
		t.Fatal("run.state transition must still trigger refresh")
	}
	if m := refreshMaskFor([]core.Event{{ID: 8, Type: "services.state"}}, false); m != refreshCore {
		t.Fatalf("services.state mask = %v, want refreshCore (%v) without history/reports/progress", m, refreshCore)
	}
	if m := refreshMaskFor([]core.Event{{ID: 9, Type: "assets.updated"}}, false); m&(refreshHistory|refreshReports|refreshProgress) != 0 {
		t.Fatalf("assets.updated must not re-scan history/reports/progress, mask = %v", m)
	}
	if m := refreshMaskFor([]core.Event{{ID: 10, Type: "download.state", Data: json.RawMessage(`{"task":{"state":"done"}}`)}}, false); m&(refreshRegistry) == 0 || m&(refreshHistory|refreshReports) != 0 {
		t.Fatalf("completed download should refresh registry without history/reports, mask = %v", m)
	}
	if m := refreshMaskFor([]core.Event{{ID: 11, Type: "report.created"}}, false); m&refreshReports == 0 || m&(refreshHistory|refreshRegistry) != 0 {
		t.Fatalf("report.created should refresh reports without history/registry, mask = %v", m)
	}
}

func TestBootstrapPaintsServicesWithoutWaitingOnProgressOrHistory(t *testing.T) {
	workspace := t.TempDir()
	for _, sub := range []string{"bundles", "preflight", "candidates", "bound", "results", "reports", "models", "engines"} {
		if err := os.MkdirAll(filepath.Join(workspace, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cliPath := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	cfgRaw, _ := json.Marshal(core.UserSettings{WorkspaceRoot: &workspace, InferyardPath: &cliPath})
	if err := os.WriteFile(settingsPath, cfgRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	var progressCalls, schemaCalls atomic.Int32
	svc, err := core.New(context.Background(), core.Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) == 1 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 99.0.0 source=release\n")}, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				progressCalls.Add(1)
				body := `{"kind":"progress","progress_version":2,"workspace":` + fmt.Sprintf("%q", workspace) + `,"device_preflight":{"status":"ok","path":null},"bundles":[],"report":{"status":"present","path":"/r/summary-1","runs":[]},"next_action":"done","next_command":null,"default_scope_complete":true}`
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(body)}, nil
			}
			if len(args) == 2 && args[0] == "--schema" && args[1] == "serve_plan" {
				schemaCalls.Add(1)
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"properties":{"schema_version":{"const":3},"kind":{"const":"serve_plan"}}}`)}, nil
			}
			if len(args) >= 2 && args[0] == "service" && args[1] == "discover" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"service discover","status":"inspected","details":{"services":[]}}`)}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Shutdown(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := testState()
	s.ctx = ctx
	s.service = svc
	s.selected = "services"
	s.loaded = false
	s.progress = nil
	s.reports = nil

	// Phase 1: bootstrap marks Services loaded immediately with 0 progress/--schema CLI spawns.
	s.bootstrap()
	if !s.loaded || !s.hydrating {
		t.Fatalf("expected bootstrap to set loaded=true hydrating=true, got loaded=%v hydrating=%v", s.loaded, s.hydrating)
	}
	if progressCalls.Load() != 0 || schemaCalls.Load() != 0 {
		t.Fatalf("bootstrap must not spawn progress or --schema CLI calls, got progress=%d schema=%d", progressCalls.Load(), schemaCalls.Load())
	}
	if s.environmentMissing() {
		t.Fatalf("expected initialized workspace and valid CLI not to report environmentMissing after bootstrap")
	}

	// While hydrating, Reports shows loading state instead of false empty guide.
	s.selected = "reports"
	rt := ui.NewTester(func(c *ui.Context) { s.reportsTableView(c) }, 960, 600)
	rt.Frame()
	if rt.HasText("新建测评") {
		t.Fatalf("expected hydrating Reports view not to flash empty-state CTA before refreshAll completes, texts=%v", rt.Texts())
	}

	// Phase 2: refreshAll completes hydration and populates cached progress/serve support.
	s.refreshMask(refreshAll)
	if s.hydrating {
		t.Fatal("expected refreshAll to clear hydrating")
	}
	if progressCalls.Load() != 1 || schemaCalls.Load() != 1 {
		t.Fatalf("expected 1 progress and 1 --schema call on initial refreshAll, got progress=%d schema=%d", progressCalls.Load(), schemaCalls.Load())
	}

	// Subsequent service-state or progress refresh runs with 0 CLI subprocess spawns when workspace is unchanged.
	s.refreshMask(refreshCore)
	s.refreshMask(refreshCore | refreshProgress)
	if progressCalls.Load() != 1 || schemaCalls.Load() != 1 {
		t.Fatalf("expected 0 extra CLI spawns on cached refresh, got progress=%d schema=%d", progressCalls.Load(), schemaCalls.Load())
	}
}

func TestOpenReportUsesWarmCacheImmediatelyAndMatchesPathExclusivity(t *testing.T) {
	svc, reportDir := reportFixtureService(t)
	_, summaries, apiErr := svc.Reports()
	if apiErr != nil || len(summaries) != 1 {
		t.Fatalf("Reports failed: err=%v summaries=%+v", apiErr, summaries)
	}

	s := testState()
	s.service = svc
	s.reportExclusivities = map[string]string{"summary-1": core.ExclusivityExclusive}
	var updates []func()
	s.updateWindow = func(fn func()) { updates = append(updates, fn) }

	// Opening via full directory path (as completionView does) hits warm cache synchronously even with live window.
	s.openReport(reportDir)
	if s.openedReport == nil || s.openedReport.loading || s.openedReport.index == nil {
		t.Fatalf("expected warm cache openReport to populate index immediately without loading state, got %+v", s.openedReport)
	}
	if len(updates) != 0 {
		t.Fatalf("expected zero async hops when report and exclusivity are cached, got %d", len(updates))
	}
	if got := s.reportExclusivity(reportDir); got != core.ExclusivityExclusive {
		t.Fatalf("reportExclusivity(fullPath) = %q, want %q", got, core.ExclusivityExclusive)
	}

	// Cold report opens asynchronously when updateWindow is present without blocking caller.
	coldSvc, coldDir := reportFixtureService(t)
	s2 := testState()
	s2.service = coldSvc
	done := make(chan func(), 2)
	s2.updateWindow = func(fn func()) { done <- fn }
	s2.openReport(filepath.Base(coldDir))
	if s2.openedReport == nil || !s2.openedReport.loading {
		t.Fatalf("expected uncached report with live window to enter non-blocking loading state, got %+v", s2.openedReport)
	}
	select {
	case fn := <-done:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async openReport completion")
	}
	if s2.openedReport.loading || s2.openedReport.index == nil {
		t.Fatalf("expected async openReport to populate index, got %+v", s2.openedReport)
	}
}

func TestReportRequestsAndDetailCasesPaginateOverFiftyRows(t *testing.T) {
	s := testState()
	s.selected = "reports"
	reqs := make([]core.ReportRequest, 65)
	for i := range reqs {
		reqs[i] = core.ReportRequest{
			CaseID:        fmt.Sprintf("case-%03d", i+1),
			CategoryLabel: "QA",
			State:         "completed",
		}
	}
	s.openedReport = &reportViewState{
		reference: "paged-report",
		index: &core.ReportIndex{Runs: []core.ReportRun{{
			Source:   core.ReportSource{RunID: "r1", Path: "/r1"},
			Model:    "Paged Model",
			Facets:   core.ReportFacets{BundleLabel: "zh-core · 1.0"},
			Requests: reqs,
		}}},
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportView(c) }) }, 1120, 1600)
	tt.Frame()
	if !tt.HasText("case-001") || !tt.HasText("case-050") || tt.HasText("case-051") {
		t.Fatalf("expected page 1 to render only first 50 requests, texts=%v", tt.Texts())
	}
	click(t, tt, "下一页")
	tt.Frame()
	if tt.HasText("case-001") || !tt.HasText("case-051") || !tt.HasText("case-065") {
		t.Fatalf("expected page 2 to render remaining 15 requests, texts=%v", tt.Texts())
	}

	// Detail Cases pagination over 50 rows.
	cases := make([]core.CaseProgress, 55)
	for i := range cases {
		cases[i] = core.CaseProgress{CaseId: fmt.Sprintf("dcase-%03d", i+1), State: "completed"}
	}
	s.selected = "benchmarks"
	s.detailID = "task-1"
	s.detail = &core.RunTaskDetail{
		Task:     core.RunTask{TaskId: "task-1", State: "running", Bundle: ptr("zh-core")},
		Progress: &core.RunProgress{Done: 55, Total: ptr(int64(55)), Cases: cases},
	}
	dt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.detailView(c) }) }, 1120, 1400)
	dt.Frame()
	if !dt.HasText("dcase-001") || !dt.HasText("dcase-050") || dt.HasText("dcase-051") {
		t.Fatalf("expected detail page 1 to render first 50 cases, texts=%v", dt.Texts())
	}
	click(t, dt, "下一页")
	dt.Frame()
	if dt.HasText("dcase-001") || !dt.HasText("dcase-051") || !dt.HasText("dcase-055") {
		t.Fatalf("expected detail page 2 to render remaining 5 cases, texts=%v", dt.Texts())
	}
}
