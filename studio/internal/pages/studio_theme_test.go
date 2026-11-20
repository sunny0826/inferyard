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
		for label, color := range map[string]ui.Color{"success": p.success, "warning": p.warning, "danger": p.danger} {
			for surface, background := range surfaces {
				if got := contrastRatio(color, background); got < 4.5 {
					t.Errorf("%s %s on %s: contrast %.2f < 4.5", name, label, surface, got)
				}
			}
		}
	}
}

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

func TestStudioBannerTextContrast(t *testing.T) {
	for name, p := range map[string]paletteColors{"light": studioLightPalette, "dark": studioDarkPalette} {
		for label, color := range map[string]ui.Color{"danger": p.danger, "warning": p.warning} {
			background := p.surface.Mix(color, .07)
			foreground := color.Mix(p.text, .2)
			if got := contrastRatio(foreground, background); got < 4.5 {
				t.Errorf("%s %s banner: contrast %.2f < 4.5", name, label, got)
			}
		}
	}
}
