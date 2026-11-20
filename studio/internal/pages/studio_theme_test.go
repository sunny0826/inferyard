package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func relativeLuminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := max(la, lb), min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

func hue(c ui.Color) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	if hi == lo {
		return 0
	}
	d := hi - lo
	var h float64
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

func hueDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	return min(d, 360-d)
}

func TestStudioPaletteStateColorsMeetContrastFloor(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		surfaces := map[string]ui.Color{"background": p.background, "surface": p.surface, "surfaceHover": p.surfaceHover}
		for label, color := range map[string]ui.Color{"info": p.info, "success": p.success, "warning": p.warning, "danger": p.danger} {
			for surface, background := range surfaces {
				if got := contrastRatio(color, background); got < 4.5 {
					t.Errorf("%s %s on %s: contrast %.2f < 4.5", name, label, surface, got)
				}
			}
		}
	}
}

func TestStudioPaletteMonochromeAccentAndNeutralGray(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		neutrals := map[string]ui.Color{
			"background": p.background, "sunken": p.sunken, "surface": p.surface,
			"surfaceHover": p.surfaceHover, "surfacePressed": p.surfacePressed,
			"elevated": p.elevated, "borderSubtle": p.borderSubtle, "border": p.border,
			"borderStrong": p.borderStrong, "text": p.text, "muted": p.muted, "tertiary": p.tertiary,
			"accent": p.accent, "accentHover": p.accentHover, "accentPressed": p.accentPressed, "accentText": p.accentText,
		}
		for label, color := range neutrals {
			if color.R != color.G || color.G != color.B {
				t.Errorf("%s %s (%d,%d,%d) is not pure neutral", name, label, color.R, color.G, color.B)
			}
		}
		if got := contrastRatio(p.accent, p.accentText); got < 12.0 {
			t.Errorf("%s accent/accentText contrast %.2f < 12.0", name, got)
		}
	}
}

func TestStudioPaletteSemanticHuesStayDistinct(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		colors := map[string]ui.Color{"info": p.info, "success": p.success, "warning": p.warning, "danger": p.danger}
		pairs := [][2]string{
			{"info", "success"}, {"info", "warning"}, {"info", "danger"},
			{"success", "warning"}, {"success", "danger"}, {"warning", "danger"},
		}
		for _, pair := range pairs {
			a, b := hue(colors[pair[0]]), hue(colors[pair[1]])
			if got := hueDistance(a, b); got < 15 {
				t.Errorf("%s %s/%s hues %.0f°/%.0f° are only %.0f° apart", name, pair[0], pair[1], a, b, got)
			}
		}
	}
}

func TestStudioBannerTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		p := paletteFor(dark)
		for label, color := range map[string]ui.Color{"info": p.info, "success": p.success, "warning": p.warning, "danger": p.danger} {
			scale := stateScaleFor(p.surface, p.text, color, dark, false)
			if got := contrastRatio(scale.Fg, scale.SubtleBg); got < 4.5 {
				t.Errorf("%s %s banner: contrast %.2f < 4.5", name, label, got)
			}
		}
	}
}

func TestStudioSpacingAndConcentricRadius(t *testing.T) {
	theme := &ui.Theme{Spacing: 4, Radius: 8, FontSize: 14}
	space := spacingTokensFor(theme)
	if space.Page != 24 || space.Section != 16 || space.CardGap != 12 || space.Row != 8 || space.Tight != 4 || space.Hairline != 2 {
		t.Fatalf("unexpected 4px spacing scale: %+v", space)
	}
	radius, card, _ := surfaceTokensFor(theme)
	if radius.Control != 8 || radius.Floating != 12 || radius.Surface != 16 || radius.Modal != 20 || card.Padding != 16 {
		t.Fatalf("unexpected soft rounded radius/card scale: radius=%+v card=%+v", radius, card)
	}
	if got := concentricRadius(radius.LG, space.Tight); got != 8 {
		t.Fatalf("concentricRadius(12, 4) = %v, want 8", got)
	}
	if got := concentricRadius(radius.MD, space.Stack); got != 4 {
		t.Fatalf("concentricRadius(8, 12) = %v, want floor 4", got)
	}
}

func TestStudioShadowsStaySubtle(t *testing.T) {
	light := elevationTokensFor(false)
	if light.XS.Color.A == 0 || light.XS.Color.A > 16 {
		t.Fatalf("light XS shadow alpha = %d, want subtle (1..16)", light.XS.Color.A)
	}
	if light.LG.Color.A > 40 {
		t.Fatalf("light LG shadow alpha = %d, want <= 40", light.LG.Color.A)
	}
	dark := elevationTokensFor(true)
	if dark.XS.Color.A == 0 || dark.XS.Color.A > 42 {
		t.Fatalf("dark XS shadow alpha = %d, want subtle (1..42)", dark.XS.Color.A)
	}
	if dark.LG.Color.A > 96 {
		t.Fatalf("dark LG shadow alpha = %d, want <= 96", dark.LG.Color.A)
	}
}
