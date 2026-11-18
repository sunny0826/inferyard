package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// Report cards keep their identity across a reorder: the open button always
// targets that report, never the row that happens to share its index.
func TestReportCardsFollowKeyAcrossReorder(t *testing.T) {
	s := testState()
	a := core.ReportSummary{ReportId: "report-a", Path: "/a"}
	b := core.ReportSummary{ReportId: "report-b", Path: "/b"}
	s.reports = []core.ReportSummary{a, b}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 800, 600)
	s.reports = []core.ReportSummary{b, a}
	tt.Frame()
	click(t, tt, "打开报告 · report-b")
	if s.openedReport == nil || s.openedReport.reference != "report-b" {
		t.Fatalf("reorder changed the opened report: %+v", s.openedReport)
	}
	s.openedReport = nil
	s.reports = []core.ReportSummary{a}
	tt.Frame()
	if tt.HasText("打开报告 · report-b") {
		t.Fatal("removed report kept its card")
	}
	click(t, tt, "打开报告 · report-a")
	if s.openedReport == nil || s.openedReport.reference != "report-a" {
		t.Fatalf("remaining report did not open: %+v", s.openedReport)
	}
}
