package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

type paletteColors struct {
	background, sunken, surface, surfaceHover, surfacePressed, elevated ui.Color
	borderSubtle, border, borderStrong                                  ui.Color
	text, muted, tertiary                                               ui.Color
	accent, accentHover, accentPressed, accentText                      ui.Color
	info, success, warning, danger                                      ui.Color
	chart                                                               [6]ui.Color
}

type stateColorScale struct {
	Solid    ui.Color
	Fg       ui.Color
	SubtleBg ui.Color
	Border   ui.Color
}

type colorTokens struct {
	Canvas, Sunken, Surface, SurfaceHover, SurfacePressed, Elevated, Backdrop ui.Color
	BorderSubtle, BorderDefault, BorderStrong, BorderFocus                    ui.Color
	FgPrimary, FgSecondary, FgTertiary, FgDisabled, FgOnAccent                ui.Color
	Accent, AccentHover, AccentPressed, AccentSubtleBg, AccentSelection       ui.Color
	Neutral, Active, Info, Success, Warning, Danger                           stateColorScale
	Chart                                                                     [6]ui.Color
}

var (
	studioLightPalette = paletteColors{
		background:     ui.Hex("#f7f7f7"),
		sunken:         ui.Hex("#efefef"),
		surface:        ui.Hex("#ffffff"),
		surfaceHover:   ui.Hex("#f2f2f2"),
		surfacePressed: ui.Hex("#e6e6e6"),
		elevated:       ui.Hex("#ffffff"),
		borderSubtle:   ui.Hex("#ebebeb"),
		border:         ui.Hex("#dcdcdc"),
		borderStrong:   ui.Hex("#b8b8b8"),
		text:           ui.Hex("#141414"),
		muted:          ui.Hex("#595959"),
		tertiary:       ui.Hex("#8c8c8c"),
		accent:         ui.Hex("#171717"),
		accentHover:    ui.Hex("#2e2e2e"),
		accentPressed:  ui.Hex("#454545"),
		accentText:     ui.Hex("#ffffff"),
		info:           ui.Hex("#255b98"),
		success:        ui.Hex("#16794b"),
		warning:        ui.Hex("#8f5a0b"),
		danger:         ui.Hex("#be2436"),
		chart: [6]ui.Color{
			ui.Hex("#255b98"),
			ui.Hex("#16794b"),
			ui.Hex("#8f5a0b"),
			ui.Hex("#6d4aa6"),
			ui.Hex("#be2436"),
			ui.Hex("#737373"),
		},
	}
	studioDarkPalette = paletteColors{
		background:     ui.Hex("#161616"),
		sunken:         ui.Hex("#111111"),
		surface:        ui.Hex("#1e1e1e"),
		surfaceHover:   ui.Hex("#272727"),
		surfacePressed: ui.Hex("#303030"),
		elevated:       ui.Hex("#252525"),
		borderSubtle:   ui.Hex("#282828"),
		border:         ui.Hex("#343434"),
		borderStrong:   ui.Hex("#4e4e4e"),
		text:           ui.Hex("#f2f2f2"),
		muted:          ui.Hex("#a3a3a3"),
		tertiary:       ui.Hex("#6e6e6e"),
		accent:         ui.Hex("#f2f2f2"),
		accentHover:    ui.Hex("#dedede"),
		accentPressed:  ui.Hex("#c7c7c7"),
		accentText:     ui.Hex("#111111"),
		info:           ui.Hex("#6ba4e8"),
		success:        ui.Hex("#4cc38a"),
		warning:        ui.Hex("#e5a93c"),
		danger:         ui.Hex("#f27d86"),
		chart: [6]ui.Color{
			ui.Hex("#6ba4e8"),
			ui.Hex("#4cc38a"),
			ui.Hex("#e5a93c"),
			ui.Hex("#a88ce2"),
			ui.Hex("#f27d86"),
			ui.Hex("#9e9e9e"),
		},
	}
)

func paletteFor(dark bool) paletteColors {
	if dark {
		return studioDarkPalette
	}
	return studioLightPalette
}

func studioThemeFor(c *ui.Context, dark bool) *ui.Theme {
	t := theme.Palette(dark)
	t.Dark = dark
	applyPalette(t, paletteFor(dark))
	t.Radius, t.Spacing = 8, 4
	t.FontSize *= 0.96
	applyThemePreferences(t, c)
	return t
}

func studioTheme(c *ui.Context) *ui.Theme {
	return studioThemeFor(c, c.Theme().Dark)
}

func applyPalette(t *ui.Theme, p paletteColors) {
	t.Background, t.Surface = p.background, p.surface
	t.SurfaceHover, t.SurfacePressed = p.surfaceHover, p.surfacePressed
	t.Border, t.Text, t.TextMuted = p.border, p.text, p.muted
	t.Accent, t.AccentText = p.accent, p.accentText
	t.AccentHover, t.AccentPressed = p.accentHover, p.accentPressed
	if t.Dark {
		t.Selection, t.Focus = p.accent.Alpha(.14), p.accent.Alpha(.80)
		t.Scrollbar = p.text.Alpha(.30)
	} else {
		t.Selection, t.Focus = p.accent.Alpha(.10), p.accent.Alpha(.75)
		t.Scrollbar = p.text.Alpha(.26)
	}
	t.Success, t.Warning, t.Danger = p.success, p.warning, p.danger
}

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

func stateScaleFor(surface, text, solid ui.Color, dark, highContrast bool) stateColorScale {
	if highContrast {
		return stateColorScale{
			Solid:    solid,
			Fg:       text,
			SubtleBg: surface,
			Border:   solid,
		}
	}
	bgMix, fgMix, borderAlpha := float32(.08), float32(.20), float32(.35)
	if dark {
		bgMix, fgMix, borderAlpha = .13, .12, .45
	}
	return stateColorScale{
		Solid:    solid,
		Fg:       solid.Mix(text, fgMix),
		SubtleBg: surface.Mix(solid, bgMix),
		Border:   solid.Alpha(borderAlpha),
	}
}

func colorTokensFor(t *ui.Theme, highContrast bool) colorTokens {
	p := paletteFor(t.Dark)
	borderSubtle := p.borderSubtle
	borderDefault := t.Border
	borderStrong := p.borderStrong
	fgTertiary := p.tertiary
	subtleBgAlpha := float32(.06)
	backdrop := ui.RGBA(0, 0, 0, 71)
	if t.Dark {
		subtleBgAlpha = .10
		backdrop = ui.RGBA(0, 0, 0, 133)
	}
	accentSubtleBg := p.accent.Alpha(subtleBgAlpha)
	if highContrast {
		borderSubtle = borderDefault
		borderStrong = t.Text
		fgTertiary = fgTertiary.Mix(t.Text, .25)
		backdrop = ui.RGBA(0, 0, 0, 184)
		accentSubtleBg = t.Surface
	}
	infoScale := stateScaleFor(t.Surface, t.Text, p.info, t.Dark, highContrast)
	return colorTokens{
		Canvas:          t.Background,
		Sunken:          p.sunken,
		Surface:         t.Surface,
		SurfaceHover:    t.SurfaceHover,
		SurfacePressed:  t.SurfacePressed,
		Elevated:        p.elevated,
		Backdrop:        backdrop,
		BorderSubtle:    borderSubtle,
		BorderDefault:   borderDefault,
		BorderStrong:    borderStrong,
		BorderFocus:     t.Focus,
		FgPrimary:       t.Text,
		FgSecondary:     t.TextMuted,
		FgTertiary:      fgTertiary,
		FgDisabled:      t.Text.Alpha(.38),
		FgOnAccent:      t.AccentText,
		Accent:          t.Accent,
		AccentHover:     t.AccentHover,
		AccentPressed:   t.AccentPressed,
		AccentSubtleBg:  accentSubtleBg,
		AccentSelection: t.Selection,
		Neutral:         stateScaleFor(t.Surface, t.Text, t.TextMuted, t.Dark, highContrast),
		Active:          infoScale,
		Info:            infoScale,
		Success:         stateScaleFor(t.Surface, t.Text, t.Success, t.Dark, highContrast),
		Warning:         stateScaleFor(t.Surface, t.Text, t.Warning, t.Dark, highContrast),
		Danger:          stateScaleFor(t.Surface, t.Text, t.Danger, t.Dark, highContrast),
		Chart:           p.chart,
	}
}
