package pages

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// relativeLuminance computes the WCAG 2.x relative luminance of an opaque sRGB
// color, so contrast claims about the palette are checked rather than stated.
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

// hue returns the HSL hue in degrees, the axis that tells a terracotta accent
// from amber warning and crimson danger.
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

// The semantic colors must stay readable on the page and card surfaces in both
// appearances. The tinted banner and badge surfaces are derived from these, so
// a palette edit that drops below the body-text floor fails here.
func TestStudioPaletteStateColorsMeetContrastFloor(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		surfaces := map[string]ui.Color{"background": p.background, "surface": p.surface, "surfaceHover": p.surfaceHover}
		for label, color := range map[string]ui.Color{"success": p.success, "warning": p.warning, "danger": p.danger} {
			for surface, background := range surfaces {
				if got := contrastRatio(color, background); got < 4.5 {
					t.Errorf("%s %s on %s: contrast %.2f < 4.5", name, label, surface, got)
				}
			}
		}
	}
}

// Accent, warning and danger carry different meanings; they must also read as
// different hues, not three warm browns. Each pairing stays at least 15° apart
// on the hue circle.
func TestStudioPaletteSemanticHuesStayDistinct(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		colors := map[string]ui.Color{"accent": p.accent, "warning": p.warning, "danger": p.danger}
		pairs := [][2]string{{"accent", "warning"}, {"accent", "danger"}, {"warning", "danger"}}
		for _, pair := range pairs {
			a, b := hue(colors[pair[0]]), hue(colors[pair[1]])
			if got := hueDistance(a, b); got < 15 {
				t.Errorf("%s %s/%s hues %.0f°/%.0f° are only %.0f° apart", name, pair[0], pair[1], a, b, got)
			}
		}
	}
}

// The danger banner text must stay readable on its own tinted surface, not just
// on the plain card background. The call site mixes the state color toward the
// text color, which is the foreground this checks.
func TestStudioErrorBannerTextContrast(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		background := p.surface.Mix(p.danger, .07)
		foreground := p.danger.Mix(p.text, .2)
		if got := contrastRatio(foreground, background); got < 4.5 {
			t.Errorf("%s error banner: contrast %.2f < 4.5", name, got)
		}
	}
}
