//go:build ui_debug && !release

package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func prepareSpeed(t *testing.T, tier string) (*previewSession, *ui.Tester) {
	t.Helper()
	p, _ := preparePreview(t)
	tt := speedTester(p, 1120, 2200)
	click(t, tt, "长上下文速度")
	if tier != "64k" {
		click(t, tt, "上下文档位")
		if err := tt.ChooseMenuItem(tier); err != nil {
			t.Fatal(err)
		}
	}
	return p, tt
}

func speedTester(p *previewSession, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.shellOptions().Theme(c))
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(205).Shrink(0).Children(func() { p.left(c) })
			p.content(c)
		})
	}, width, height)
}
