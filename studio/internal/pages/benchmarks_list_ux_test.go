package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestRecentContextSpeedRowsIdentifyModelContextAndEngine(t *testing.T) {
	s := readyState("benchmarks")
	started := "2026-01-01T12:30:00Z"
	s.speed = &speedWorkspace{history: []*speedTask{{id: "history", stage: speedDone, model: "Preview-Model-Q4.gguf", context: 65536, engine: "preview-engine", startedAt: started}}}
	s.tasks = []core.RunTask{{TaskId: "disk", Kind: "context_speed", State: "done", ContextSpeed: &core.ContextSpeedTask{Model: "registered-model", DisplayModel: "display-model", Context: 32768}}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.benchmarksTasksTabView(c) }) }, 1120, 1200)
	kind := l10n.TR("shell.header.context_speed")
	for _, want := range []string{"Preview-Model-Q4.gguf · 64k · " + kind, "display-model · 32k · " + kind, "preview-engine · " + formatTimestamp(started)} {
		if !tt.HasText(want) {
			t.Fatalf("recent tasks missing %q: %v", want, tt.Texts())
		}
	}
	s.speed.history = nil
	for _, tc := range []struct {
		speed core.ContextSpeedTask
		title string
	}{
		{core.ContextSpeedTask{Model: "legacy-model"}, "legacy-model · " + kind},
		{core.ContextSpeedTask{Context: 65536}, "64k · " + kind},
		{core.ContextSpeedTask{}, kind},
	} {
		s.tasks[0].ContextSpeed = &tc.speed
		tt.Frame()
		if !tt.HasText(tc.title) {
			t.Fatalf("row with missing metadata should show %q: %v", tc.title, tt.Texts())
		}
	}
}

func TestHistoryScoreGapLabels(t *testing.T) {
	tests := []struct {
		score  core.RunScore
		zh, en string
	}{
		{core.RunScore{Passed: 4, Valid: 6, Failed: 2}, "4/6 · 未完整，含 2 题执行失败", "4/6 · Incomplete, 2 failed"},
		{core.RunScore{Passed: 4, Valid: 6, Missing: 2}, "4/6 · 2 题缺评分", "4/6 · 2 missing scores"},
		{core.RunScore{Passed: 4, Valid: 6, Unscorable: 2}, "4/6 · 2 题不可评分", "4/6 · 2 unscorable"},
	}
	for _, locale := range []string{l10n.LanguageZhHans, l10n.LanguageEn} {
		t.Run(locale, func(t *testing.T) {
			t.Cleanup(l10n.SetTestLocale(locale))
			for _, tc := range tests {
				want := tc.zh
				if locale == l10n.LanguageEn {
					want = tc.en
				}
				if got := runScore(core.RunSummary{Score: &tc.score}); got != want {
					t.Errorf("score %v: got %q, want %q", tc.score, got, want)
				}
			}
		})
	}
}
