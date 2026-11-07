package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestABSideColorsAreDistinguishable covers the comparison requirement that
// the two sides of an A/B result must not share a color, which would make the
// paired bars indistinguishable. It does not pin the chosen colors.
func TestABSideColorsAreDistinguishable(t *testing.T) {
	var first, second ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		first, second = sideColor(c, sideA), sideColor(c, sideB)
	}, 40, 40)
	tt.Frame()
	if first == second {
		t.Fatal("A/B sides share a color; the paired bars are indistinguishable")
	}
}

// TestPairedBarRendersBothSides covers the comparison requirement that a paired
// bar shows the metric name, both side names and both displayed values.
func TestPairedBarRendersBothSides(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		pairedBar(c, "通过率", pairedBarValue{Label: "A", Value: 0.5, Text: "50%"}, pairedBarValue{Label: "B", Value: 1.5, Text: "150%"})
	}, 400, 120)
	tt.Frame()
	for _, want := range []string{"通过率", "A", "B", "50%", "150%"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
}
