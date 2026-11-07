//go:build ui_debug && !release

package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

// candidatePalette is one candidate's color set for a single appearance.
type candidatePalette struct {
	background, surface, surfaceHover, surfacePressed ui.Color
	border, text, muted                               ui.Color
	accent, accentText                                ui.Color
	success, warning, danger                          ui.Color
}

// previewCandidate is one selectable visual scheme in the debug window. A
// candidate pairs a MyGo palette with token overrides, so switching one
// changes density, radii, type hierarchy, card edges and A/B side colors — not
// just hues. The palette is installed for the preview window only.
type previewCandidate struct {
	id, label, summary string
	base               bool
	spacing, radius    float32
	fontScale          float32
	light, dark        candidatePalette
	tweak              func(t *ui.Theme, base designTokens) designTokens
}

var previewCandidates = []previewCandidate{
	{
		id: "current", label: "当前方案（对照）", base: true,
		summary: "现有 token 与 onevra 调色板；圆角 12，卡片无描边，间距 4。",
	},
	{
		id: "a", label: "A · 薄边紧凑",
		spacing: 3, radius: 4, fontScale: 0.96,
		summary: "密度 -25%，圆角 6，卡片 1 DIP 描边，字号层级更紧；适合信息密集的多卡页面。",
		light: candidatePalette{
			background: ui.Hex("#f8f7f3"), surface: ui.Hex("#ffffff"), surfaceHover: ui.Hex("#f0eee8"), surfacePressed: ui.Hex("#e3e0d8"),
			border: ui.Hex("#d6d3c9"), text: ui.Hex("#2a2a27"), muted: ui.Hex("#67675f"),
			accent: ui.Hex("#b3401a"), accentText: ui.Hex("#ffffff"),
			success: ui.Hex("#047857"), warning: ui.Hex("#a34a06"), danger: ui.Hex("#a63a42"),
		},
		dark: candidatePalette{
			background: ui.Hex("#1b1a17"), surface: ui.Hex("#24221e"), surfaceHover: ui.Hex("#2e2b26"), surfacePressed: ui.Hex("#38342e"),
			border: ui.Hex("#47433c"), text: ui.Hex("#e9e5dd"), muted: ui.Hex("#b1aa9e"),
			accent: ui.Hex("#e88d64"), accentText: ui.Hex("#291b14"),
			success: ui.Hex("#6fbfa2"), warning: ui.Hex("#dfa95f"), danger: ui.Hex("#dd878d"),
		},
		tweak: func(t *ui.Theme, set designTokens) designTokens {
			set.Radius = radiusTokens{Surface: t.Space(2), Inset: t.Space(1)}
			set.Card = cardTokens{Padding: t.Space(3.5), Border: 1, Edge: 1, EmptyPadding: 32}
			set.Metric = metricTokens{PaddingX: t.Space(3), PaddingY: t.Space(2.5), Gap: t.Space(1)}
			set.Type.PageTitle, set.Type.CardTitle = t.Rem(1.6), t.Rem(1.2)
			set.Type.Section, set.Type.Metric, set.Type.MetricLarge = t.Rem(1.05), t.Rem(1.25), t.Rem(1.55)
			set.Type.Caption = max(12, t.Rem(.82))
			set.Sides = sideColors{A: ui.Hex("#2563eb"), B: ui.Hex("#b45309")}
			if t.Dark {
				set.Sides = sideColors{A: ui.Hex("#7cb2ff"), B: ui.Hex("#e6b567")}
			}
			return set
		},
	},
	{
		id: "b", label: "B · 大圆角宽松",
		spacing: 5, radius: 14, fontScale: 1.03,
		summary: "密度 +25%，圆角 18，卡片更宽的内边距与更大标题；适合少卡片、阅读优先的页面。",
		light: candidatePalette{
			background: ui.Hex("#faf8f3"), surface: ui.Hex("#fffefb"), surfaceHover: ui.Hex("#f4efe7"), surfacePressed: ui.Hex("#e9e2d6"),
			border: ui.Hex("#e1d9cb"), text: ui.Hex("#302f2a"), muted: ui.Hex("#75736a"),
			accent: ui.Hex("#c04d1f"), accentText: ui.Hex("#ffffff"),
			success: ui.Hex("#0b8a6b"), warning: ui.Hex("#b65a0b"), danger: ui.Hex("#bb4a52"),
		},
		dark: candidatePalette{
			background: ui.Hex("#201e1a"), surface: ui.Hex("#2c2924"), surfaceHover: ui.Hex("#38342d"), surfacePressed: ui.Hex("#423d35"),
			border: ui.Hex("#544e44"), text: ui.Hex("#f2eee6"), muted: ui.Hex("#c2baad"),
			accent: ui.Hex("#f0a077"), accentText: ui.Hex("#291b14"),
			success: ui.Hex("#83c8ab"), warning: ui.Hex("#e9bd72"), danger: ui.Hex("#ea979d"),
		},
		tweak: func(t *ui.Theme, set designTokens) designTokens {
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
		},
	},
	{
		id: "c", label: "C · 灰卡白底",
		spacing: 4, radius: 6, fontScale: 1,
		summary: "白色页面配灰色卡片与 1 DIP 描边，圆角 10，文字对比更高；适合图表和表格为主的页面。",
		light: candidatePalette{
			background: ui.Hex("#ffffff"), surface: ui.Hex("#eeeeea"), surfaceHover: ui.Hex("#e3e3dd"), surfacePressed: ui.Hex("#d7d7cf"),
			border: ui.Hex("#c9c8be"), text: ui.Hex("#1b1b18"), muted: ui.Hex("#56564e"),
			accent: ui.Hex("#a63a12"), accentText: ui.Hex("#ffffff"),
			success: ui.Hex("#04724f"), warning: ui.Hex("#8a4300"), danger: ui.Hex("#a12b34"),
		},
		dark: candidatePalette{
			background: ui.Hex("#0f0f0e"), surface: ui.Hex("#201f1d"), surfaceHover: ui.Hex("#2a2926"), surfacePressed: ui.Hex("#343330"),
			border: ui.Hex("#45443f"), text: ui.Hex("#f7f5ee"), muted: ui.Hex("#c3bcae"),
			accent: ui.Hex("#ffa070"), accentText: ui.Hex("#291b14"),
			success: ui.Hex("#7fd1b2"), warning: ui.Hex("#f0bd63"), danger: ui.Hex("#f09aa0"),
		},
		tweak: func(t *ui.Theme, set designTokens) designTokens {
			set.Radius = radiusTokens{Surface: t.Space(2.5), Inset: t.Space(1.5)}
			set.Card = cardTokens{Padding: t.Space(4.5), Border: 1, Edge: 1, EmptyPadding: 40}
			set.Sides = sideColors{A: ui.Hex("#c2410c"), B: ui.Hex("#1d4ed8")}
			if t.Dark {
				set.Sides = sideColors{A: ui.Hex("#fdba74"), B: ui.Hex("#93c5fd")}
			}
			return set
		},
	},
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
// tweak for the current frame. The zero candidate is the production theme.
func candidateTheme(index int, c *ui.Context) *ui.Theme {
	cand := previewCandidates[candidateIndex(index)]
	if cand.base {
		candidateStyle.theme, candidateStyle.apply = nil, nil
		return theme.Onevra(c)
	}
	t := theme.Palette(c.Theme().Dark)
	t.Dark = c.Theme().Dark
	p := cand.light
	if t.Dark {
		p = cand.dark
	}
	applyCandidatePalette(t, p)
	t.Radius, t.Spacing = cand.radius, cand.spacing
	t.FontSize *= cand.fontScale
	applyCandidatePreferences(t, c)
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

func applyCandidatePalette(t *ui.Theme, p candidatePalette) {
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

// applyCandidatePreferences mirrors theme.Onevra's high-contrast and text-scale
// adjustments so the candidates follow the same desktop preferences.
func applyCandidatePreferences(t *ui.Theme, c *ui.Context) {
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
