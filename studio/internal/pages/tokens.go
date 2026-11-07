package pages

import "github.com/egoist/mygo/ui"

// designTokens is the single source of shared page styling. Every default
// resolves to the value the page code used before this layer existed, so
// introducing tokens does not change the rendered pages.
type designTokens struct {
	Density spacingTokens
	Type    typeTokens
	Radius  radiusTokens
	Card    cardTokens
	Metric  metricTokens
	State   stateColors
	Sides   sideColors
}

// spacingTokens holds the DIP sizes pages share. Widget-specific widths and
// heights stay at their call sites.
type spacingTokens struct {
	Page     float32 // page content inset
	Section  float32 // gap between page sections
	CardGap  float32 // gap inside a card
	Stack    float32 // task card and metric grid gap
	Row      float32 // inline row gap
	Inset    float32 // list and popup inset
	Tight    float32 // label/value gap
	Hairline float32 // minimal separation inside a list row
}

// typeTokens holds the font hierarchy, expressed through the theme's Rem so
// the desktop text scale still applies.
type typeTokens struct {
	PageTitle   float32 // first-level page heading
	CardTitle   float32 // card heading
	Section     float32 // subsection heading and task name
	Metric      float32 // compact metric value
	MetricLarge float32 // report metric value
	Body        float32 // default text
	Caption     float32 // secondary text, at least 12 DIP
	ShellLabel  float32 // development environment label
	ShellDetail float32 // development environment detail
}

// radiusTokens holds the surface radii shared by cards and inset rows.
type radiusTokens struct {
	Surface float32 // cards, metric cards and empty panels
	Inset   float32 // nested rows inside a surface
}

// cardTokens describes the card surface and its empty-state variant.
type cardTokens struct {
	Padding      float32
	Border       float32
	EmptyPadding float32
}

// metricTokens describes the report metric card.
type metricTokens struct {
	PaddingX float32
	PaddingY float32
	Gap      float32
}

// stateKind names the semantic status a color expresses. Neutral is shared by
// secondary text and idle states.
type stateKind int

const (
	stateNeutral stateKind = iota
	stateActive
	stateSuccess
	stateWarning
	stateDanger
)

type stateColors struct {
	Neutral ui.Color
	Active  ui.Color
	Success ui.Color
	Warning ui.Color
	Danger  ui.Color
}

func (s stateColors) forKind(kind stateKind) ui.Color {
	switch kind {
	case stateActive:
		return s.Active
	case stateSuccess:
		return s.Success
	case stateWarning:
		return s.Warning
	case stateDanger:
		return s.Danger
	default:
		return s.Neutral
	}
}

// sideKind names one side of an A/B comparison.
type sideKind int

const (
	sideA sideKind = iota
	sideB
)

// sideColors distinguishes the two sides of an A/B comparison. No page uses
// them yet; T5 selects the final values.
type sideColors struct {
	A ui.Color
	B ui.Color
}

func (s sideColors) forSide(side sideKind) ui.Color {
	if side == sideB {
		return s.B
	}
	return s.A
}

func tokens(c *ui.Context) designTokens {
	t := c.Theme()
	radius, card, metric := surfaceTokensFor(t)
	return designTokens{
		Density: spacingTokensFor(t),
		Type:    typeTokensFor(t),
		Radius:  radius,
		Card:    card,
		Metric:  metric,
		State:   stateColorsFor(t),
		Sides:   sideColorsFor(t),
	}
}

func spacingTokensFor(t *ui.Theme) spacingTokens {
	return spacingTokens{
		Page:     t.Space(6),
		Section:  t.Space(4),
		CardGap:  t.Space(2.5),
		Stack:    t.Space(3),
		Row:      t.Space(2),
		Inset:    t.Space(1.5),
		Tight:    t.Space(1),
		Hairline: 2,
	}
}

func typeTokensFor(t *ui.Theme) typeTokens {
	return typeTokens{
		PageTitle:   t.Rem(1.7),
		CardTitle:   t.Rem(1.3),
		Section:     t.Rem(1.1),
		Metric:      t.Rem(1.35),
		MetricLarge: t.Rem(1.7),
		Body:        t.Rem(1),
		Caption:     max(12, t.Rem(.85)),
		ShellLabel:  t.Rem(.75),
		ShellDetail: t.Rem(.625),
	}
}

func surfaceTokensFor(t *ui.Theme) (radiusTokens, cardTokens, metricTokens) {
	radius := radiusTokens{Surface: t.Space(3), Inset: t.Space(2)}
	card := cardTokens{Padding: t.Space(4.5), Border: 1, EmptyPadding: 40}
	metric := metricTokens{PaddingX: t.Space(4), PaddingY: t.Space(3.5), Gap: t.Space(1)}
	return radius, card, metric
}

func stateColorsFor(t *ui.Theme) stateColors {
	return stateColors{Neutral: t.TextMuted, Active: t.Accent, Success: t.Success, Warning: t.Warning, Danger: t.Danger}
}

func sideColorsFor(t *ui.Theme) sideColors {
	return sideColors{A: t.Accent, B: t.Success}
}

func stateColor(c *ui.Context, kind stateKind) ui.Color {
	return tokens(c).State.forKind(kind)
}

func sideColor(c *ui.Context, side sideKind) ui.Color {
	return tokens(c).Sides.forSide(side)
}
