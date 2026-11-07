//go:build ui_debug && !release

package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

// previewCandidate is one selectable visual scheme in the debug window. A
// candidate pairs a MyGo theme with token overrides, so switching one changes
// density, radii, type hierarchy, card edges and A/B side colors — not just
// hues. Only the debug window installs candidates.
type previewCandidate struct {
	id, label, summary string
	theme              func(*ui.Context) *ui.Theme
	tweak              func(t *ui.Theme, base designTokens) designTokens
}

var previewCandidates = []previewCandidate{
	{
		id: "default", label: "生产默认 · A · 薄边紧凑", theme: studioTheme,
		summary: "已选方案：密度 3，圆角 6，卡片与任务行 1 DIP 描边，标题 1.6；生产页面当前使用这一套。",
	},
	{
		id: "b", label: "B · 大圆角宽松",
		summary: "密度 5，圆角 18，卡片更宽的内边距与更大标题；适合少卡片、阅读优先的页面。",
		theme: func(c *ui.Context) *ui.Theme {
			return variantTheme(c, candidateVariant{light: studioLightPalette, dark: studioDarkPalette, spacing: 5, radius: 14, fontScale: 1.03})
		},
		tweak: candidateBRoomy,
	},
	{
		id: "c", label: "C · 灰卡白底",
		summary: "白色页面配灰色卡片与 1 DIP 描边，圆角 10，文字对比更高；适合图表和表格为主的页面。",
		theme: func(c *ui.Context) *ui.Theme {
			return variantTheme(c, candidateVariant{light: greyLightPalette, dark: greyDarkPalette, spacing: 4, radius: 6, fontScale: 1})
		},
		tweak: candidateCGrey,
	},
	{
		id: "legacy", label: "旧方案（对照）", theme: theme.Onevra,
		summary: "阶段一之前的 onevra 调色板与 token：密度 4、圆角 12、卡片无描边、标题 1.7。",
		tweak:   legacyTokens,
	},
}

var (
	greyLightPalette = paletteColors{
		background: ui.Hex("#ffffff"), surface: ui.Hex("#eeeeea"), surfaceHover: ui.Hex("#e3e3dd"), surfacePressed: ui.Hex("#d7d7cf"),
		border: ui.Hex("#c9c8be"), text: ui.Hex("#1b1b18"), muted: ui.Hex("#4f4f47"),
		accent: ui.Hex("#a63a12"), accentText: ui.Hex("#ffffff"),
		success: ui.Hex("#04724f"), warning: ui.Hex("#7d3d00"), danger: ui.Hex("#a12b34"),
	}
	greyDarkPalette = paletteColors{
		background: ui.Hex("#0f0f0e"), surface: ui.Hex("#201f1d"), surfaceHover: ui.Hex("#2a2926"), surfacePressed: ui.Hex("#343330"),
		border: ui.Hex("#45443f"), text: ui.Hex("#f7f5ee"), muted: ui.Hex("#cbc4b6"),
		accent: ui.Hex("#ffa070"), accentText: ui.Hex("#291b14"),
		success: ui.Hex("#7fd1b2"), warning: ui.Hex("#f0bd63"), danger: ui.Hex("#f09aa0"),
	}
)

// candidateVariant describes a comparison scheme's palette and metrics.
type candidateVariant struct {
	light, dark     paletteColors
	spacing, radius float32
	fontScale       float32
}

// variantTheme builds a candidate palette on top of the production helpers.
func variantTheme(c *ui.Context, v candidateVariant) *ui.Theme {
	t := theme.Palette(c.Theme().Dark)
	t.Dark = c.Theme().Dark
	p := v.light
	if t.Dark {
		p = v.dark
	}
	applyPalette(t, p)
	t.Spacing, t.Radius = v.spacing, v.radius
	t.FontSize *= v.fontScale
	applyThemePreferences(t, c)
	return t
}

func candidateBRoomy(t *ui.Theme, set designTokens) designTokens {
	set.Radius = radiusTokens{Surface: t.Space(3.5), Inset: t.Space(2)}
	set.Card = cardTokens{Padding: t.Space(4), Border: 1, Edge: 0, EmptyPadding: 56}
	set.Metric = metricTokens{PaddingX: t.Space(3.5), PaddingY: t.Space(3), Gap: t.Space(1)}
	set.Type.PageTitle, set.Type.CardTitle = t.Rem(1.9), t.Rem(1.42)
	set.Type.Section, set.Type.Metric, set.Type.MetricLarge = t.Rem(1.15), t.Rem(1.45), t.Rem(1.9)
	set.Sides = sideColors{A: ui.Hex("#0f766e"), B: ui.Hex("#6d28d9")}
	if t.Dark {
		set.Sides = sideColors{A: ui.Hex("#5eead4"), B: ui.Hex("#c4b5fd")}
	}
	return set
}

func candidateCGrey(t *ui.Theme, set designTokens) designTokens {
	set.Radius = radiusTokens{Surface: t.Space(2.5), Inset: t.Space(1.5)}
	set.Sides = sideColors{A: ui.Hex("#c2410c"), B: ui.Hex("#1d4ed8")}
	if t.Dark {
		set.Sides = sideColors{A: ui.Hex("#fdba74"), B: ui.Hex("#93c5fd")}
	}
	return set
}

func legacyTokens(t *ui.Theme, set designTokens) designTokens {
	set.Radius = radiusTokens{Surface: t.Space(3), Inset: t.Space(2)}
	set.Card = cardTokens{Padding: t.Space(4.5), Border: 1, Edge: 0, EmptyPadding: 40}
	set.Metric = metricTokens{PaddingX: t.Space(4), PaddingY: t.Space(3.5), Gap: t.Space(1)}
	set.Type.PageTitle, set.Type.CardTitle = t.Rem(1.7), t.Rem(1.3)
	set.Type.Section, set.Type.Metric, set.Type.MetricLarge = t.Rem(1.1), t.Rem(1.35), t.Rem(1.7)
	set.Type.Caption = max(12, t.Rem(.85))
	set.Sides = sideColors{A: t.Accent, B: t.Success}
	return set
}

// candidateStyle ties a token tweak to the exact theme pointer that installed
// it, so the production window and in-memory tests that use the stock theme
// never inherit a candidate.
var candidateStyle struct {
	theme *ui.Theme
	apply func(*ui.Theme, designTokens) designTokens
}

func init() { candidateTokenStyle = previewTokenStyle }

func previewTokenStyle(c *ui.Context, base designTokens) designTokens {
	t := c.Theme()
	if candidateStyle.theme != t || candidateStyle.apply == nil {
		return base
	}
	return candidateStyle.apply(t, base)
}

// candidateTheme returns the theme for one candidate and activates its token
// tweak for the current frame. The first candidate is the production theme.
func candidateTheme(index int, c *ui.Context) *ui.Theme {
	cand := previewCandidates[candidateIndex(index)]
	t := cand.theme(c)
	candidateStyle.theme, candidateStyle.apply = t, cand.tweak
	return t
}

// candidateIndex clamps an index to the candidate list.
func candidateIndex(index int) int {
	if index < 0 || index >= len(previewCandidates) {
		return 0
	}
	return index
}
