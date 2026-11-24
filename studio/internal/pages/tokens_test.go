package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func assertOn4pxGrid(t *testing.T, name string, value float32) {
	t.Helper()
	if value < 0 {
		t.Fatalf("%s = %v, want >= 0", name, value)
	}
	if rem := math.Mod(float64(value), 4); rem != 0 {
		t.Fatalf("%s = %v, want multiple of 4", name, value)
	}
}

func TestDesignTokensConcentricRadiiAnd4pxGrid(t *testing.T) {
	theme := &ui.Theme{Radius: 8, Spacing: 4}
	spacing := spacingTokensFor(theme)
	radius, card, metric := surfaceTokensFor(theme)

	for name, v := range map[string]float32{
		"Density.Page":      spacing.Page,
		"Density.Section":   spacing.Section,
		"Density.CardGap":   spacing.CardGap,
		"Density.Stack":     spacing.Stack,
		"Density.Row":       spacing.Row,
		"Density.Inset":     spacing.Inset,
		"Density.Tight":     spacing.Tight,
		"Density.Hairline":  spacing.Hairline,
		"Card.Padding":      card.Padding,
		"Card.EmptyPadding": card.EmptyPadding,
		"Metric.PaddingX":   metric.PaddingX,
		"Metric.PaddingY":   metric.PaddingY,
		"Metric.Gap":        metric.Gap,
	} {
		assertOn4pxGrid(t, name, v)
	}

	for name, v := range map[string]float32{
		"Radius.None":     radius.None,
		"Radius.XS":       radius.XS,
		"Radius.SM":       radius.SM,
		"Radius.MD":       radius.MD,
		"Radius.LG":       radius.LG,
		"Radius.XL":       radius.XL,
		"Radius.XXL":      radius.XXL,
		"Radius.Surface":  radius.Surface,
		"Radius.Inset":    radius.Inset,
		"Radius.Control":  radius.Control,
		"Radius.Floating": radius.Floating,
		"Radius.Modal":    radius.Modal,
	} {
		assertOn4pxGrid(t, name, v)
		if name != "Radius.None" && v < 4 {
			t.Fatalf("%s = %v, want >= 4", name, v)
		}
	}

	if radius.Full < 9999 {
		t.Fatalf("Radius.Full = %v, want pill/circle radius >= 9999", radius.Full)
	}
	if got := concentricRadius(radius.Surface, card.Padding); got != radius.Inset {
		t.Fatalf("card concentric radius = %v, want Radius.Inset (%v)", got, radius.Inset)
	}
	if got := concentricRadius(radius.Modal, spacing.Section); got != radius.Control {
		t.Fatalf("modal concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(radius.Modal, spacing.Row); got != radius.LG {
		t.Fatalf("inbox popover concentric radius = %v, want Radius.LG (%v)", got, radius.LG)
	}
	if got := concentricRadius(radius.LG, spacing.Row); got != radius.Control {
		t.Fatalf("toast/inbox card concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(radius.Floating, spacing.Tight); got != radius.Control {
		t.Fatalf("floating concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(radius.Inset, spacing.Tight); got != radius.XS {
		t.Fatalf("inset list-row concentric radius = %v, want Radius.XS (%v)", got, radius.XS)
	}
	if got := concentricRadius(4, 8); got != 4 {
		t.Fatalf("concentricRadius floor = %v, want 4", got)
	}
}
