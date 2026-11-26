//go:build ui_debug && !release

package pages

import (
	"context"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreviewReportListOpensHistorySources(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, previewSceneZhLabel("preview.scene.task_completed")))
	s := p.state
	s.selectPage("reports")
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 900)
	click(t, tt, tr(nil, "reports.action.create_from_history"))
	tt.Frame()
	runs := s.historyRuns()
	if !s.reportDialogOpen || len(runs) == 0 || !tt.HasText(tr(nil, "reports.dialog.select_runs")) {
		t.Fatalf("missing report sources: %v", tt.Texts())
	}
	click(t, tt, runs[0].RunId)
	if !s.reportChoices.Has(runs[0].Path) {
		t.Fatal("history run was not selected")
	}
}

func TestPreviewMac48StarterNotes(t *testing.T) {
	s := testState()
	previewRecMac48FirstUse(&s)
	rec := s.starterRecommendation()
	if !rec.FromSnapshot || rec.Backend != "metal" || slices.Contains(rec.Notes, "cpu_missing_avx2_experimental_build") {
		t.Fatalf("unexpected Mac48 recommendation: backend=%q notes=%v", rec.Backend, rec.Notes)
	}
}
