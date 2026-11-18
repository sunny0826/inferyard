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
// inside the table viewport: a CLI microsecond timestamp renders as compact UTC
// in the cell without clipping, while the selected row's raw detail keeps the
// original bytes.
func TestHistoryTableFitsDefaultWindow(t *testing.T) {
	s := testState()
	s.selected = "reports"
	rawStarted := "2026-11-18T00:00:13.123456+00:00"
	run := core.RunSummary{RunId: "run-1", Path: "/r", ModelName: ptr("model · SmolLM2-135M-Instruct-Q4_K_M.gguf"), Bundle: ptr("zh-core"), Status: "partial", StartedAt: &rawStarted, Score: &core.RunScore{Passed: 120, Valid: 120, Rate: ptr(1.0)}}
	s.history.StandaloneRuns = []core.RunSummary{run}
	tt := ui.NewTester(s.view, 1024, 681)
	viewport, ok := tt.Find("运行历史表")
	if !ok {
		t.Fatal("history table missing", tt.Texts())
	}
	startedCell := historyStarted(run)
	if startedCell != "2026-11-18T00:00:13Z" {
		t.Errorf("historyStarted(%q) = %q, want %q", rawStarted, startedCell, "2026-11-18T00:00:13Z")
	}
	for _, text := range []string{"模型", "测评题集", "状态", "评分", "开始时间", "zh-core", "不完整", runScore(run), startedCell} {
		got, ok := tt.Find(text)
		if !ok {
			t.Fatalf("%q missing from the history table: %v", text, tt.Texts())
		}
		if want := naturalWidth(t, text); got.W < want-0.5 || got.X+got.W > viewport.X+viewport.W-0.5 {
			t.Errorf("%q is cut: shows %.1f of %.1f DIP, ends at %.1f, viewport ends at %.1f", text, got.W, want, got.X+got.W, viewport.X+viewport.W)
		}
	}
	assertRawHistoryTimestamp(t, tt, &s.history.StandaloneRuns[0], rawStarted)
}

func assertRawHistoryTimestamp(t *testing.T, tt *ui.Tester, run *core.RunSummary, want string) {
	t.Helper()
	click(t, tt, run.RunId)
	click(t, tt, "运行原始详情")
	if emptyText(run.StartedAt) != want || !tt.HasText("开始："+want) {
		t.Fatalf("selected raw detail lost original StartedAt %q: %v", want, tt.Texts())
	}
}
