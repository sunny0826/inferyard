//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestPreviewPalettesContrastAndRender(t *testing.T) {
	defer setActiveAccent(core.AccentCobalt)

	if got := activeAccent(); got != core.AccentCobalt {
		t.Fatalf("default activeAccent() = %q, want %q", got, core.AccentCobalt)
	}
	if len(studioAccentSpecs) != 5 {
		t.Fatalf("len(studioAccentSpecs) = %d, want 5", len(studioAccentSpecs))
	}

	white := ui.Hex("#ffffff")
	for _, spec := range studioAccentSpecs {
		for _, mode := range []struct {
			name string
			dark bool
			p    paletteColors
		}{
			{"light", false, spec.light},
			{"dark", true, spec.dark},
		} {
			check := func(label string, fg, bg ui.Color, min float64) {
				t.Helper()
				if r := contrastRatio(fg, bg); r < min {
					t.Errorf("accent %s (%s) %s contrast = %.2f, want >= %.1f", spec.id, mode.name, label, r, min)
				}
			}
			check("body/surface", mode.p.text, mode.p.surface, 7.0)
			check("muted/surface", mode.p.muted, mode.p.surface, 4.5)
			check("primary/rest", mode.p.accentText, mode.p.accent, 4.5)
			check("primary/hover", mode.p.accentText, mode.p.accentHover, 4.5)
			check("primary/pressed", mode.p.accentText, mode.p.accentPressed, 4.5)

			for _, st := range []struct {
				name  string
				solid ui.Color
			}{
				{"info", mode.p.info},
				{"success", mode.p.success},
				{"warning", mode.p.warning},
				{"danger", mode.p.danger},
			} {
				sc := stateScaleFor(mode.p.surface, mode.p.text, st.solid, mode.dark, false)
				check(st.name+"-badge", sc.Fg, sc.SubtleBg, 4.5)
			}

			bp := mode.p.buttons
			check("secondary/rest", mode.p.text, bp.secondaryBg, 7.0)
			check("secondary/hover", mode.p.text, bp.secondaryHover, 7.0)
			check("danger-soft/rest", bp.dangerFg, bp.dangerBg, 4.5)
			check("danger-soft/hover", bp.dangerFg, bp.dangerHover, 4.5)
			check("danger-solid/rest", white, bp.dangerSolid, 4.5)
			check("danger-solid/hover", white, bp.dangerSolidHover, 4.5)
			check("engine-running", white, bp.engineRunning, 4.5)
		}
	}

	p := newPreviewSession(context.Background())
	p.openDesignSystemScene()
	tt := previewTester(p, 1200, 1800)

	for _, spec := range studioAccentSpecs {
		p.selectAccent(spec.id)
		for _, themeMode := range []int{1, 2} {
			p.themeMode = themeMode
			tt.Frame()
			got := paletteFor(themeMode == 2)
			want := spec.light
			if themeMode == 2 {
				want = spec.dark
			}
			if got.accent != want.accent {
				t.Fatalf("accent %s theme=%d accent = %v, want %v", spec.id, themeMode, got.accent, want.accent)
			}
		}
	}

	p.state.selected = "settings"
	p.selectAccent(core.AccentCobalt)
	tt.Frame()
	click(t, tt, "深曜靛蓝")
	tt.Frame()
	if p.accentMode != core.AccentIndigo || p.state.accentMode != core.AccentIndigo || p.state.settings.Effective.Accent != core.AccentIndigo {
		t.Fatalf("settings accent click did not sync: preview=%q state=%q effective=%q",
			p.accentMode, p.state.accentMode, p.state.settings.Effective.Accent)
	}
}
