package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestReportCardsKeepRunsSeparateAndNeverZeroFill(t *testing.T) {
	s := testState()
	s.reports = []core.ReportSummary{{
		ReportId: "multi", Path: "/reports/multi", RunIds: []string{"a", "b"}, RunPaths: []*string{nil, nil},
		RunSummaries: []core.ReportListRun{
			{RunID: "a", Model: "Model-A", BundleLabel: "zh-core", Quality: core.ReportQuality{StudioCounts: core.ReportCounts{Numerator: ptr(int64(3)), Denominator: ptr(int64(5))}, Complete: true}},
			{RunID: "b", Model: "Model-B", BundleLabel: "zh-svg-pelican"},
		},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsTableView(c) }) }, 1120, 1200)
	for _, want := range []string{"2 次运行", "Model-A · zh-core · 答案通过 3 / 5", "Model-B · zh-svg-pelican · 答案通过未记录"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"3 / 5 · 0 / 0", "100%", "合计"} {
		if tt.HasText(unwanted) {
			t.Fatalf("runs merged or zero-filled as %q", unwanted)
		}
	}
}
