package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func tokenFixture(t *testing.T) (designTokens, *ui.Theme) {
	t.Helper()
	var got designTokens
	var theme *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		theme = c.Theme()
		got = tokens(c)
	}, 40, 40)
	tt.Frame()
	return got, theme
}

func TestTokenSpacingAndRadiusDefaults(t *testing.T) {
	got, theme := tokenFixture(t)
	if got.Density.Page != theme.Space(6) || got.Density.CardGap != theme.Space(2.5) || got.Density.Tight != theme.Space(1) {
		t.Fatalf("spacing tokens changed: %+v", got.Density)
	}
	if got.Radius.Surface != theme.Space(3) || got.Radius.Inset != theme.Space(2) {
		t.Fatalf("radius tokens changed: %+v", got.Radius)
	}
}

func TestTokenTypeDefaults(t *testing.T) {
	got, theme := tokenFixture(t)
	if got.Type.PageTitle != theme.Rem(1.7) || got.Type.CardTitle != theme.Rem(1.3) || got.Type.Caption != max(12, theme.Rem(.85)) {
		t.Fatalf("type tokens changed: %+v", got.Type)
	}
}

func TestTokenStateColorsAndSides(t *testing.T) {
	got, theme := tokenFixture(t)
	if got.State.Success != theme.Success || got.State.Warning != theme.Warning || got.State.Danger != theme.Danger || got.State.Active != theme.Accent {
		t.Fatal("state tokens do not resolve to the theme colors")
	}
	if got.Sides.A == got.Sides.B {
		t.Fatal("A/B sides must be distinguishable")
	}
}

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
