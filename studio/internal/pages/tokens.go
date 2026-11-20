package pages

import "github.com/egoist/mygo/ui"

type designTokens struct {
	Density spacingTokens
	Type    typeTokens
	Radius  radiusTokens
	Card    cardTokens
	Metric  metricTokens
	State   stateColors
	Sides   sideColors
}

type spacingTokens struct {
	Page     float32
	Section  float32
	CardGap  float32
	Stack    float32
	Row      float32
	Inset    float32
	Tight    float32
	Hairline float32
}

type typeTokens struct {
	PageTitle   float32
	CardTitle   float32
	Section     float32
	Metric      float32
	MetricLarge float32
	Body        float32
	Caption     float32
	ShellLabel  float32
	ShellDetail float32
}

type radiusTokens struct {
	Surface float32
	Inset   float32
}

type cardTokens struct {
	Padding      float32
	Border       float32
	Edge         float32
	EmptyPadding float32
}

type metricTokens struct {
	PaddingX float32
	PaddingY float32
	Gap      float32
}

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

type sideKind int

const (
	sideA sideKind = iota
	sideB
)

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
	return styledTokens(c, designTokens{
		Density: spacingTokensFor(t),
		Type:    typeTokensFor(t),
		Radius:  radius,
		Card:    card,
		Metric:  metric,
		State:   stateColorsFor(t),
		Sides:   sideColorsFor(t),
	})
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
		PageTitle:   t.Rem(1.6),
		CardTitle:   t.Rem(1.2),
		Section:     t.Rem(1.05),
		Metric:      t.Rem(1.25),
		MetricLarge: t.Rem(1.55),
		Body:        t.Rem(1),
		Caption:     max(12, t.Rem(.82)),
		ShellLabel:  t.Rem(.75),
		ShellDetail: t.Rem(.625),
	}
}

func concentricRadius(outer, padding float32) float32 {
	return max(2, outer-padding)
}

func surfaceTokensFor(t *ui.Theme) (radiusTokens, cardTokens, metricTokens) {
	padding := t.Space(3)
	surface := t.Space(4)
	radius := radiusTokens{Surface: surface, Inset: concentricRadius(surface, padding)}
	card := cardTokens{Padding: padding, Border: 1, Edge: 1, EmptyPadding: 32}
	metric := metricTokens{PaddingX: t.Space(3), PaddingY: t.Space(2.5), Gap: t.Space(1)}
	return radius, card, metric
}

func stateColorsFor(t *ui.Theme) stateColors {
	return stateColors{Neutral: t.TextMuted, Active: t.Accent, Success: t.Success, Warning: t.Warning, Danger: t.Danger}
}

func sideColorsFor(t *ui.Theme) sideColors {
	if t.Dark {
		return sideColors{A: ui.Hex("#7cb2ff"), B: ui.Hex("#e6b567")}
	}
	return sideColors{A: ui.Hex("#2563eb"), B: ui.Hex("#b45309")}
}

func stateColor(c *ui.Context, kind stateKind) ui.Color {
	return tokens(c).State.forKind(kind)
}

func sideColor(c *ui.Context, side sideKind) ui.Color {
	return tokens(c).Sides.forSide(side)
}
