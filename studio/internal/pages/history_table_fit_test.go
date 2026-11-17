package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// naturalWidth is how wide s draws on one line with nothing clipping it.
func naturalWidth(t *testing.T, s string) float32 {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) { ui.Row(c).Children(func() { ui.Text(c, s).SingleLine() }) }, 2000, 100)
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("%q not drawn", s)
	}
	return r.W
}

// At the window size the native suite uses, every history column shows in full
// inside the table viewport: no header or cell is cut at the right edge, so the
// start time reads without scrolling sideways.
func TestHistoryTableFitsDefaultWindow(t *testing.T) {
	s := testState()
	s.selected = "reports"
	run := core.RunSummary{RunId: "r", Path: "/r", ModelName: ptr("model · SmolLM2-135M-Instruct-Q4_K_M.gguf"), Bundle: ptr("zh-svg-pelican"), Status: "partial", StartedAt: ptr("2026-10-17T00:00:13Z"), Score: &core.RunScore{Passed: 120, Valid: 120, Rate: ptr(1.0)}}
	s.history.StandaloneRuns = []core.RunSummary{run}
	tt := ui.NewTester(s.view, 1024, 681)
	viewport, ok := tt.Find("运行历史表")
	if !ok {
		t.Fatal("history table missing", tt.Texts())
	}
	for _, text := range []string{"模型", "测评题集", "状态", "评分", "开始时间", "zh-svg-pelican", "不完整", runScore(run), *run.StartedAt} {
		got, ok := tt.Find(text)
		if !ok {
			t.Fatalf("%q missing from the history table", text)
		}
		if want := naturalWidth(t, text); got.W < want-0.5 || got.X+got.W > viewport.X+viewport.W-0.5 {
			t.Errorf("%q is cut: shows %.1f of %.1f DIP, ends at %.1f, viewport ends at %.1f", text, got.W, want, got.X+got.W, viewport.X+viewport.W)
		}
	}
}
