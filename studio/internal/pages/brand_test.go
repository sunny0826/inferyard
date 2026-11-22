package pages

import (
	"image/color"
	"testing"
)

func TestBrandLogosParseAndRasterize(t *testing.T) {
	for name, dark := range map[string]bool{"light": false, "dark": true} {
		svg := brandLogoFor(dark)
		if svg == nil {
			t.Fatalf("%s brand logo is nil", name)
		}
		w, h := svg.Size()
		if w != 256 || h != 256 {
			t.Fatalf("%s brand logo size = %gx%g, want 256x256", name, w, h)
		}
		if unsupported := svg.Unsupported(); len(unsupported) > 0 {
			t.Fatalf("%s brand logo has unsupported SVG features: %v", name, unsupported)
		}
		img := svg.Image(40, 40, color.Black)
		if img == nil {
			t.Fatalf("%s brand logo rendered nil image", name)
		}
		opaque := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				opaque++
			}
		}
		if opaque == 0 {
			t.Fatalf("%s brand logo rasterized completely transparent", name)
		}
	}
}
