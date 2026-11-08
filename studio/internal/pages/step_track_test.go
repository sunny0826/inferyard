package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// stepNumberContainment returns the step numbers whose text stays inside the
// labeled number container at the current frame.
func stepNumberContainment(t *testing.T, tt *ui.Tester) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, label := range tt.Texts() {
		number, ok := strings.CutPrefix(label, "阶段编号 ")
		if !ok {
			continue
		}
		box, foundBox := tt.Find(label)
		text, foundText := tt.Find(number)
		if !foundBox || !foundText {
			out[number] = false
			continue
		}
		const tolerance = 0.5
		out[number] = text.X >= box.X-tolerance && text.Y >= box.Y-tolerance &&
			text.X+text.W <= box.X+box.W+tolerance && text.Y+text.H <= box.Y+box.H+tolerance
	}
	return out
}

// TestStepNumberFitsAtLargeText covers the readable step rail: the number must
// stay inside its badge when the desktop text scale grows the glyphs. The
// container follows the text line height instead of a fixed diameter.
func TestStepNumberFitsAtLargeText(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1.5, 2} {
			assertStepNumbersFit(t, dark, scale)
		}
	}
}

func assertStepNumbersFit(t *testing.T, dark bool, scale float32) {
	t.Helper()
	for _, number := range []string{"1", "5", "10", "✓"} {
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(studioTheme(c))
			ui.Column(c).Width(400).Children(func() {
				stepTrack(c, number, "长上下文速度", func() { ui.Text(c, "已完成 3 / 5 次测量") })
			})
		}, 800, 600)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: scale})
		tt.SetDark(dark)
		tt.Advance(2 * time.Second)
		contained, ok := stepNumberContainment(t, tt)[number]
		if !ok || !contained {
			top, _ := tt.Find("阶段编号 " + number)
			text, _ := tt.Find(number)
			t.Fatalf("dark=%v scale=%v number %q not contained: text %+v box %+v", dark, scale, number, text, top)
		}
	}
}
