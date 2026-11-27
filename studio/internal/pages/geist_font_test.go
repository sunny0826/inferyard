package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestGeistFontRegisteredAndApplied(t *testing.T) {
	ensureGeistFont()

	found := false
	for _, fam := range ui.FontFamilies() {
		if strings.EqualFold(fam.Name, geistFontFamily) && fam.Registered {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registered font family %q in ui.FontFamilies()", geistFontFamily)
	}

	var themeFont string
	tt := ui.NewTester(func(c *ui.Context) {
		themeFont = studioThemeFor(c, false).Font
	}, 800, 600)
	_ = tt

	if themeFont != geistFontFamily {
		t.Fatalf("studioThemeFor().Font = %q, want %q", themeFont, geistFontFamily)
	}
}
