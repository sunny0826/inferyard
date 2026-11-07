package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// productionThemeInstall is the entry the main window uses for its theme.
func productionThemeInstall() func(*ui.Context) *ui.Theme {
	return pageShellOptions("InferYard Studio", "", nil, nil).Theme
}

// productionTheme resolves the installed Studio theme for one appearance.
func productionTheme(t *testing.T, dark, highContrast bool) *ui.Theme {
	t.Helper()
	var theme *ui.Theme
	install := productionThemeInstall()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(install(c))
		theme = c.Theme()
	}, 200, 200)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, HighContrast: highContrast})
	tt.SetDark(dark)
	tt.Frame()
	return theme
}

// TestABSideColorsAreDistinguishable covers the comparison requirement that
// both sides of an A/B result stay visible on the card surface, in both
// appearances: the sides must not share a color and each must reach the 3:1
// non-text contrast floor against the actual Surface. It does not pin the
// chosen colors.
func TestABSideColorsAreDistinguishable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		assertABSideColors(t, dark)
	}
}

func assertABSideColors(t *testing.T, dark bool) {
	t.Helper()
	var a, b, surface ui.Color
	install := productionThemeInstall()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(install(c))
		a, b = sideColor(c, sideA), sideColor(c, sideB)
		surface = c.Theme().Surface
	}, 40, 40)
	tt.SetDark(dark)
	tt.Frame()
	if a == b {
		t.Fatalf("dark=%v A/B sides share a color; the paired bars are indistinguishable", dark)
	}
	for name, color := range map[string]ui.Color{"A": a, "B": b} {
		if ratio := contrastRatio(color, surface); ratio < 3 {
			t.Fatalf("dark=%v side %s contrast against surface %.2f < 3", dark, name, ratio)
		}
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
	theme := productionTheme(t, dark, highContrast)
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
	theme := productionTheme(t, dark, highContrast)
	colors := map[string]ui.Color{
		"neutral": stateColorContext(t, theme, stateNeutral), "active": stateColorContext(t, theme, stateActive),
		"success": stateColorContext(t, theme, stateSuccess), "warning": stateColorContext(t, theme, stateWarning),
		"danger": stateColorContext(t, theme, stateDanger),
	}
	for name, tint := range colors {
		background, foreground := badgeSurfaceColors(theme, tint)
		if highContrast {
			background = theme.Surface
		}
		if ratio := contrastRatio(foreground, background); ratio < 4.5 {
			t.Fatalf("dark=%v hc=%v %s badge %.2f", dark, highContrast, name, ratio)
		}
	}
}

// stateColorContext resolves one semantic state color through the page entry
// so the test reads the same mapping the pages use.
func stateColorContext(t *testing.T, theme *ui.Theme, kind stateKind) ui.Color {
	t.Helper()
	var color ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		color = stateColor(c, kind)
	}, 40, 40)
	tt.Frame()
	return color
}

// TestQueuedRunUsesWaitingColor covers the design contract that a task reading
// "排队中" uses the neutral waiting color, not the active one. Tasks that
// actually started keep the active color.
func TestQueuedRunUsesWaitingColor(t *testing.T) {
	var neutral, active, queued, starting ui.Color
	install := productionThemeInstall()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(install(c))
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
