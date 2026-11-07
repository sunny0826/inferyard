package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// productionTheme resolves the installed Studio theme for one appearance.
func productionTheme(t *testing.T, dark, highContrast bool) (*ui.Theme, designTokens) {
	t.Helper()
	var theme *ui.Theme
	var set designTokens
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioTheme(c))
		theme, set = c.Theme(), tokens(c)
	}, 400, 400)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, HighContrast: highContrast})
	tt.SetDark(dark)
	tt.Frame()
	return theme, set
}

// TestABSideColorsAreDistinguishable covers the comparison requirement that
// the two sides of an A/B result must not share a color, which would make the
// paired bars indistinguishable. It does not pin the chosen colors.
func TestABSideColorsAreDistinguishable(t *testing.T) {
	var first, second ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioTheme(c))
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

// TestProductionThemeDensity covers the selected scheme: tighter spacing,
// smaller radii and an edged page card. These are the user's visual decision,
// not a snapshot of incidental values.
func TestProductionThemeDensity(t *testing.T) {
	for _, dark := range []bool{false, true} {
		theme, set := productionTheme(t, dark, false)
		if theme.Spacing != 3 || theme.Radius != 4 {
			t.Fatalf("dark=%v: spacing %v radius %v", dark, theme.Spacing, theme.Radius)
		}
		if set.Radius.Surface != 6 || set.Card.Edge != 1 {
			t.Fatalf("dark=%v: card radius %v edge %v", dark, set.Radius.Surface, set.Card.Edge)
		}
	}
}

// TestBodyTextContrast covers the review finding that body and secondary text
// looked washed out: both keep their contrast floor on the page and on cards,
// including high contrast.
func TestBodyTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, highContrast := range []bool{false, true} {
			assertTextContrast(t, dark, highContrast)
		}
	}
}

func assertTextContrast(t *testing.T, dark, highContrast bool) {
	t.Helper()
	theme, _ := productionTheme(t, dark, highContrast)
	if ratio := contrastRatio(theme.Text, theme.Background); ratio < 7 {
		t.Fatalf("dark=%v hc=%v text %.2f", dark, highContrast, ratio)
	}
	for _, surface := range []ui.Color{theme.Background, theme.Surface} {
		if ratio := contrastRatio(theme.TextMuted, surface); ratio < 4.5 {
			t.Fatalf("dark=%v hc=%v muted %.2f", dark, highContrast, ratio)
		}
	}
}

// TestStatusBadgeTextContrast covers the dark-badge readability fix: every
// state label keeps body-text contrast on an opaque badge background, in both
// appearances and in high contrast.
func TestStatusBadgeTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, highContrast := range []bool{false, true} {
			assertBadgeContrast(t, dark, highContrast)
		}
	}
}

func assertBadgeContrast(t *testing.T, dark, highContrast bool) {
	t.Helper()
	theme, set := productionTheme(t, dark, highContrast)
	for name, tint := range map[string]ui.Color{
		"neutral": set.State.Neutral, "active": set.State.Active, "success": set.State.Success,
		"warning": set.State.Warning, "danger": set.State.Danger,
	} {
		background, foreground := badgeSurfaceColors(theme, tint)
		if highContrast {
			background = theme.Surface
		}
		if ratio := contrastRatio(foreground, background); ratio < 4.5 {
			t.Fatalf("dark=%v hc=%v %s badge %.2f", dark, highContrast, name, ratio)
		}
	}
}

// TestQueuedRunUsesWaitingColor covers the design contract that a task reading
// "排队中" uses the neutral waiting color, not the active one. Tasks that
// actually started keep the active color.
func TestQueuedRunUsesWaitingColor(t *testing.T) {
	var neutral, active, queued, starting ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioTheme(c))
		neutral, active = stateColor(c, stateNeutral), stateColor(c, stateActive)
		queued = runColor(c, core.RunTask{State: "starting", Queued: true})
		starting = runColor(c, core.RunTask{State: "starting"})
	}, 40, 40)
	tt.Frame()
	if neutral == active {
		t.Fatal("neutral and active colors are identical")
	}
	if queued != neutral {
		t.Fatal("queued task does not use the waiting color")
	}
	if starting != active {
		t.Fatal("started task lost the active color")
	}
}

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + .05) / (lb + .05)
}

func relativeLuminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= .03928 {
			return s / 12.92
		}
		return math.Pow((s+.055)/1.055, 2.4)
	}
	return .2126*channel(c.R) + .7152*channel(c.G) + .0722*channel(c.B)
}
