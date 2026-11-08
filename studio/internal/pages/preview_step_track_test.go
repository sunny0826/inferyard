//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestPreviewStepTrackFitsBothPages covers the two production call sites of
// stepTrack (regular benchmark and context speed) at the minimum window size:
// the number stays inside its badge at 1.5 and 2.0 text scale in both
// appearances. Frames advance past the color transitions before measuring.
func TestPreviewStepTrackFitsBothPages(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1.5, 2} {
			t.Run(scaleName(scale, dark), func(t *testing.T) {
				p := newPreviewSession(context.Background())
				p.loadScene(previewSceneIndex(t, "任务 · 运行中"))
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(p.theme(c))
					p.state.dashboardView(c)
				}, 800, 600)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: scale})
				tt.SetDark(dark)
				tt.Advance(2 * time.Second)
				assertStepNumbersContained(t, tt, "dashboard")

				s := newPreviewSession(context.Background())
				s.loadScene(previewSceneIndex(t, "任务 · 待运行"))
				s.state.speed.current = &speedTask{stage: speedMeasuring, model: "Preview-Model-Q4.gguf", engine: "preview-engine · llama-server", context: 65536, completed: 3, id: "preview-speed"}
				st := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(s.theme(c))
					s.state.dashboardView(c)
				}, 800, 600)
				st.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: scale})
				st.SetDark(dark)
				st.Advance(2 * time.Second)
				assertStepNumbersContained(t, st, "context-speed")
			})
		}
	}
}

func scaleName(scale float32, dark bool) string {
	name := "light"
	if dark {
		name = "dark"
	}
	if scale == 1.5 {
		return "1.5-" + name
	}
	return "2.0-" + name
}

func assertStepNumbersContained(t *testing.T, tt *ui.Tester, page string) {
	t.Helper()
	contained := stepNumberContainment(t, tt)
	if len(contained) == 0 {
		t.Fatalf("%s: no step number rendered", page)
	}
	for number, ok := range contained {
		if !ok {
			top, _ := tt.Find("阶段编号 " + number)
			text, _ := tt.Find(number)
			t.Fatalf("%s: number %q outside its badge: text %+v badge %+v", page, number, text, top)
		}
	}
}
