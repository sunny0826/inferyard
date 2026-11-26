package pages

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
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
	}
	if needsRefresh(events, false) {
		t.Fatal("high-frequency in-memory progress/resource events must not trigger heavy refresh")
	}
	if !needsRefresh(append(events, core.Event{ID: 4, Type: "run.state"}), false) {
		t.Fatal("run.state transition must still trigger refresh")
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
