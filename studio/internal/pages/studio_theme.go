package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
	"inferyard/studio/internal/core"
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
		background:     ui.Hex("#fafafa"),
		sunken:         ui.Hex("#f4f4f4"),
		surface:        ui.Hex("#ffffff"),
		surfaceHover:   ui.Hex("#f4f4f4"),
		surfacePressed: ui.Hex("#ebebeb"),
		elevated:       ui.Hex("#ffffff"),
		borderSubtle:   ui.Hex("#efefef"),
		border:         ui.Hex("#e5e5e5"),
		borderStrong:   ui.Hex("#cfcfcf"),
		text:           ui.Hex("#171717"),
		muted:          ui.Hex("#666666"),
		tertiary:       ui.Hex("#999999"),
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
		background:     ui.Hex("#212121"),
		sunken:         ui.Hex("#1a1a1a"),
		surface:        ui.Hex("#2a2a2a"),
		surfaceHover:   ui.Hex("#333333"),
		surfacePressed: ui.Hex("#3d3d3d"),
		elevated:       ui.Hex("#303030"),
		borderSubtle:   ui.Hex("#353535"),
		border:         ui.Hex("#424242"),
		borderStrong:   ui.Hex("#5a5a5a"),
		text:           ui.Hex("#f5f5f5"),
		muted:          ui.Hex("#b4b4b4"),
		tertiary:       ui.Hex("#808080"),
		accent:         ui.Hex("#ffffff"),
		accentHover:    ui.Hex("#e8e8e8"),
		accentPressed:  ui.Hex("#d6d6d6"),
		accentText:     ui.Hex("#171717"),
		info:           ui.Hex("#7cb3f0"),
		success:        ui.Hex("#5cd198"),
		warning:        ui.Hex("#edb84d"),
		danger:         ui.Hex("#f78c94"),
		chart: [6]ui.Color{
			ui.Hex("#7cb3f0"),
			ui.Hex("#5cd198"),
			ui.Hex("#edb84d"),
			ui.Hex("#b59be8"),
			ui.Hex("#f78c94"),
			ui.Hex("#a8a8a8"),
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

func normalizeThemeMode(mode string) string {
	return core.NormalizeTheme(mode)
}

func resolveStudioTheme(c *ui.Context, mode string) *ui.Theme {
	switch normalizeThemeMode(mode) {
	case core.ThemeLight:
		return studioThemeFor(c, false)
	case core.ThemeDark:
		return studioThemeFor(c, true)
	default:
		return studioTheme(c)
	}
}

func nativeThemeSource(mode string) mygo.ThemeSource {
	switch normalizeThemeMode(mode) {
	case core.ThemeLight:
		return mygo.ThemeLight
	case core.ThemeDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

func syncNativeTheme(mode string) {
	mygo.Theme.SetSource(nativeThemeSource(mode))
}

func applyPalette(t *ui.Theme, p paletteColors) {
	t.Background, t.Surface = p.background, p.surface
	t.SurfaceHover, t.SurfacePressed = p.surfaceHover, p.surfacePressed
	t.Border, t.Text, t.TextMuted = p.border, p.text, p.muted
	t.Accent, t.AccentText = p.accent, p.accentText
	t.AccentHover, t.AccentPressed = p.accentHover, p.accentPressed
	if t.Dark {
		t.Selection, t.Focus = p.accent.Alpha(.14), p.accent.Alpha(.75)
		t.Scrollbar = p.text.Alpha(.28)
	} else {
		t.Selection, t.Focus = p.accent.Alpha(.08), p.accent.Alpha(.65)
		t.Scrollbar = p.text.Alpha(.24)
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
	bgMix, fgMix, borderAlpha := float32(.07), float32(.18), float32(.30)
	if dark {
		bgMix, fgMix, borderAlpha = .12, .10, .42
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
	subtleBgAlpha := float32(.05)
	backdrop := ui.RGBA(0, 0, 0, .24)
	if t.Dark {
		subtleBgAlpha = .09
		backdrop = ui.RGBA(0, 0, 0, .45)
	}
	accentSubtleBg := p.accent.Alpha(subtleBgAlpha)
	if highContrast {
		borderSubtle = borderDefault
		borderStrong = t.Text
		fgTertiary = fgTertiary.Mix(t.Text, .25)
		backdrop = ui.RGBA(0, 0, 0, .68)
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
