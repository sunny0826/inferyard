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
	control := controlTokensFor(theme)
	radius, card, metric := surfaceTokensFor(theme)

	for name, v := range map[string]float32{
		"Control.SM":        control.SM,
		"Control.MD":        control.MD,
		"Control.LG":        control.LG,
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
		if name != "Radius.None" && (v < 8 || v > 40) {
			t.Fatalf("%s = %v, want in [8, 40]", name, v)
		}
	}

	if radius.Full < 9999 {
		t.Fatalf("Radius.Full = %v, want pill/circle radius >= 9999", radius.Full)
	}
	if got := concentricRadius(radius.Surface, card.Padding); got != radius.Control {
		t.Fatalf("card concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(radius.Modal, spacing.Row); got != radius.LG {
		t.Fatalf("popover concentric radius = %v, want Radius.LG (%v)", got, radius.LG)
	}
	if got := concentricRadius(radius.LG, spacing.Tight); got != radius.Control {
		t.Fatalf("toast/inbox card concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(radius.Floating, spacing.Tight); got != radius.Control {
		t.Fatalf("floating concentric radius = %v, want Radius.Control (%v)", got, radius.Control)
	}
	if got := concentricRadius(4, 8); got != 8 {
		t.Fatalf("concentricRadius floor = %v, want 8", got)
	}
}
