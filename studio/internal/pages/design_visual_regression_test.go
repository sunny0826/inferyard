package pages

import (
	"fmt"
	"image"
	"image/draw"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestReportSourcesDoNotPaintOverFooter(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			s := designFixture("reports")
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportSourcesView(c) }) }, 800, 600)
			click(t, tt, "从历史运行另做报告")
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{TextScale: 2, HighContrast: true, ReduceMotion: true})
			tt.Frame()
			list, ok := tt.Find("报告来源运行列表")
			if !ok {
				t.Fatal("missing sources list")
			}
			footer, ok := tt.Find("请选择 1–100 个来源 run。")
			if !ok {
				t.Fatal("missing footer")
			}
			t.Logf("list=%+v footer=%+v", list, footer)
			if footer.Y < list.Y+list.H {
				t.Fatal("footer overlaps source list", list, footer)
			}
			before := image.NewRGBA(tt.Image().Bounds())
			draw.Draw(before, before.Bounds(), tt.Image(), image.Point{}, draw.Src)
			for i := range s.history.StandaloneRuns {
				s.history.StandaloneRuns[i].RunId = fmt.Sprintf("run-%03d", 999-i)
			}
			tt.Frame()
			after := tt.Image()
			for y := int(list.Y+list.H) + 1; y < min(600, int(footer.Y+footer.H)); y++ {
				for x := int(list.X); x < int(list.X+list.W); x++ {
					if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
						t.Fatalf("source row paints outside list over footer at %d,%d", x, y)
					}
				}
			}
		})
	}
}
