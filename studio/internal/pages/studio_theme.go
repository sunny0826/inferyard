package pages

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
	"inferyard/studio/internal/core"
)

type buttonPalette struct {
	secondaryBg, secondaryHover, secondaryPressed, secondaryBorder, secondaryBorderHover ui.Color
	dangerBg, dangerHover, dangerPressed, dangerBorder, dangerFg                         ui.Color
	dangerSolid, dangerSolidHover, dangerSolidPressed                                    ui.Color
	engineRunning, engineRunningPressed                                                  ui.Color
	selectionBg, focusRing                                                               ui.Color
}

type paletteColors struct {
	background, sunken, surface, surfaceHover, surfacePressed, elevated ui.Color
	borderSubtle, border, borderStrong                                  ui.Color
	text, muted, tertiary                                               ui.Color
	accent, accentHover, accentPressed, accentText                      ui.Color
	info, success, warning, danger                                      ui.Color
	chart                                                               [6]ui.Color
	buttons                                                             buttonPalette
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

func paletteFor(dark bool) paletteColors {
	return paletteForAccent(activeAccent(), dark)
}

func studioThemeFor(c *ui.Context, dark bool) *ui.Theme {
	ensureGeistFont()
	t := theme.Palette(dark)
	t.Dark = dark
	applyPalette(t, paletteFor(dark))
	t.Radius, t.Spacing = 8, 4
	t.Font = geistFontFamily
	t.FontSize = applyStudioFontSize(t.FontSize)
	t.TooltipDelay = labelOpenDelay
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
		t.Scrollbar = p.text.Alpha(.24)
	} else {
		t.Selection, t.Focus = p.accent.Alpha(.08), p.accent.Alpha(.65)
		t.Scrollbar = p.text.Alpha(.20)
	}
	if p.buttons.selectionBg.A > 0 {
		t.Selection = p.buttons.selectionBg
	}
	if p.buttons.focusRing.A > 0 {
		t.Focus = p.buttons.focusRing
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
