package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

// paletteColors is one appearance's Studio palette. The page shell installs
// it on top of the onevra base, which keeps the fields Studio does not set.
type paletteColors struct {
	background, surface, surfaceHover, surfacePressed ui.Color
	border, text, muted                               ui.Color
	accent, accentText                                ui.Color
	success, warning, danger                          ui.Color
}

// studioLightPalette and studioDarkPalette are the selected scheme A: warm
// near-neutral surfaces, a terracotta accent and muted text held above the
// body-text contrast floor in both appearances.
var (
	studioLightPalette = paletteColors{
		background: ui.Hex("#f8f7f3"), surface: ui.Hex("#ffffff"), surfaceHover: ui.Hex("#f0eee8"), surfacePressed: ui.Hex("#e3e0d8"),
		border: ui.Hex("#d6d3c9"), text: ui.Hex("#26261f"), muted: ui.Hex("#5c5c53"),
		accent: ui.Hex("#b3401a"), accentText: ui.Hex("#ffffff"),
		success: ui.Hex("#047857"), warning: ui.Hex("#a34a06"), danger: ui.Hex("#a63a42"),
	}
	studioDarkPalette = paletteColors{
		background: ui.Hex("#1b1a17"), surface: ui.Hex("#24221e"), surfaceHover: ui.Hex("#2e2b26"), surfacePressed: ui.Hex("#38342e"),
		border: ui.Hex("#47433c"), text: ui.Hex("#efeae2"), muted: ui.Hex("#c6bfb3"),
		accent: ui.Hex("#e88d64"), accentText: ui.Hex("#291b14"),
		success: ui.Hex("#6fbfa2"), warning: ui.Hex("#dfa95f"), danger: ui.Hex("#dd878d"),
	}
)

// studioTheme installs the production scheme. The debug window reuses the
// same helpers to build its comparison candidates.
func studioTheme(c *ui.Context) *ui.Theme {
	t := theme.Palette(c.Theme().Dark)
	t.Dark = c.Theme().Dark
	p := studioLightPalette
	if t.Dark {
		p = studioDarkPalette
	}
	applyPalette(t, p)
	t.Radius, t.Spacing = 4, 3
	t.FontSize *= 0.96
	applyThemePreferences(t, c)
	return t
}

// applyPalette overrides the Studio-owned colors and derives the interaction,
// selection and focus colors from the accent.
func applyPalette(t *ui.Theme, p paletteColors) {
	black, white := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)
	t.Background, t.Surface = p.background, p.surface
	t.SurfaceHover, t.SurfacePressed = p.surfaceHover, p.surfacePressed
	t.Border, t.Text, t.TextMuted = p.border, p.text, p.muted
	t.Accent, t.AccentText = p.accent, p.accentText
	if t.Dark {
		t.AccentHover, t.AccentPressed = p.accent.Mix(white, .12), p.accent.Mix(black, .12)
		t.Selection, t.Focus = p.accent.Alpha(.24), p.accent.Alpha(.8)
		t.Scrollbar = p.text.Alpha(.35)
	} else {
		t.AccentHover, t.AccentPressed = p.accent.Mix(black, .12), p.accent.Mix(black, .25)
		t.Selection, t.Focus = p.accent.Alpha(.22), p.accent.Alpha(.55)
		t.Scrollbar = p.text.Alpha(.30)
	}
	t.Success, t.Warning, t.Danger = p.success, p.warning, p.danger
}

// applyThemePreferences mirrors theme.Onevra's high-contrast and text-scale
// adjustments so Studio follows the same desktop preferences.
func applyThemePreferences(t *ui.Theme, c *ui.Context) {
	p := c.Preferences()
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, .45)
		t.TextMuted = t.TextMuted.Mix(t.Text, .4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}
	if s := p.TextScale; s > 0 && s != 1 {
		t.FontSize *= s
	}
}
