package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func TestSharedIconButtonPrimitives(t *testing.T) {
	var outlineClicked, primaryClicked, dangerClicked, refreshClicked int
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Gap(8).Children(func() {
			if outlineIconButton(c, lucide.Copy, "Copy URL").Clicked() {
				outlineClicked++
			}
			if primaryIconButton(c, lucide.Play, "Start Service").Clicked() {
				primaryClicked++
			}
			if dangerIconButton(c, lucide.Trash, "Delete Asset").Clicked() {
				dangerClicked++
			}
			if refreshIconButton(c, "Refresh List", true).Clicked() {
				refreshClicked++
			}
		})
	}, 600, 120)
	tt.Frame()

	for _, label := range []string{"Copy URL", "Start Service", "Delete Asset", "Refresh List"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("expected icon button with accessible label %q", label)
		}
	}

	click(t, tt, "Copy URL")
	click(t, tt, "Start Service")
	click(t, tt, "Delete Asset")
	click(t, tt, "Refresh List")
	tt.Frame()

	if outlineClicked != 1 || primaryClicked != 1 || dangerClicked != 1 || refreshClicked != 1 {
		t.Fatalf("unexpected click counts: outline=%d primary=%d danger=%d refresh=%d",
			outlineClicked, primaryClicked, dangerClicked, refreshClicked)
	}
}
