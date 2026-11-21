package pages

import "github.com/egoist/mygo/ui"

type designTokens struct {
	Color     colorTokens
	Density   spacingTokens
	Type      typeTokens
	Radius    radiusTokens
	Elevation elevationTokens
	Card      cardTokens
	Metric    metricTokens
	State     stateColors
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
	None     float32
	XS       float32
	SM       float32
	MD       float32
	LG       float32
	XL       float32
	XXL      float32
	Full     float32
	Surface  float32
	Inset    float32
	Control  float32
	Floating float32
	Modal    float32
}

type shadowSpec struct {
	X, Y, Blur, Spread float32
	Color              ui.Color
}

type elevationTokens struct {
	None shadowSpec
	XS   shadowSpec
	MD   shadowSpec
	LG   shadowSpec
}

type materialKind int

const (
	materialCanvas materialKind = iota
	materialSunken
	materialCard
	materialInset
	materialControl
	materialFloating
	materialModal
)

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
	stateInfo
	stateSuccess
	stateWarning
	stateDanger
)

type stateColors struct {
	Neutral ui.Color
	Active  ui.Color
	Info    ui.Color
	Success ui.Color
	Warning ui.Color
	Danger  ui.Color
}

func (s stateColors) forKind(kind stateKind) ui.Color {
	switch kind {
	case stateActive:
		return s.Active
	case stateInfo:
		return s.Info
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

func tokens(c *ui.Context) designTokens {
	t := c.Theme()
	highContrast := c.Preferences().HighContrast
	colors := colorTokensFor(t, highContrast)
	radius, card, metric := surfaceTokensFor(t)
	return designTokens{
		Color:     colors,
		Density:   spacingTokensFor(t),
		Type:      typeTokensFor(t),
		Radius:    radius,
		Elevation: elevationTokensFor(t.Dark),
		Card:      card,
		Metric:    metric,
		State:     stateColorsFor(colors),
	}
}

func spacingTokensFor(t *ui.Theme) spacingTokens {
	return spacingTokens{
		Page:     t.Space(6),
		Section:  t.Space(4),
		CardGap:  t.Space(3),
		Stack:    t.Space(3),
		Row:      t.Space(2),
		Inset:    t.Space(2),
		Tight:    t.Space(1),
		Hairline: t.Space(.5),
	}
}

func typeTokensFor(t *ui.Theme) typeTokens {
	return typeTokens{
		PageTitle:   t.Rem(1.57),
		CardTitle:   t.Rem(1.14),
		Section:     t.Rem(1.0),
		Metric:      t.Rem(1.29),
		MetricLarge: t.Rem(1.71),
		Body:        t.Rem(1.0),
		Caption:     max(12, t.Rem(.86)),
		ShellLabel:  t.Rem(.75),
		ShellDetail: t.Rem(.68),
	}
}

func concentricRadius(outer, padding float32) float32 {
	return max(4, outer-padding)
}

func surfaceTokensFor(t *ui.Theme) (radiusTokens, cardTokens, metricTokens) {
	cardPad := t.Space(3)
	cardRadius := t.Space(5)
	modalRadius := t.Space(6)
	floatingRadius := t.Space(3)
	radius := radiusTokens{
		None:     0,
		XS:       t.Space(1),
		SM:       t.Space(1.5),
		MD:       t.Space(2),
		LG:       floatingRadius,
		XL:       cardRadius,
		XXL:      modalRadius,
		Full:     9999,
		Surface:  cardRadius,
		Inset:    concentricRadius(cardRadius, cardPad),
		Control:  concentricRadius(floatingRadius, t.Space(1)),
		Floating: floatingRadius,
		Modal:    modalRadius,
	}
	card := cardTokens{
		Padding:      cardPad,
		Border:       1,
		Edge:         1,
		EmptyPadding: t.Space(8),
	}
	metric := metricTokens{
		PaddingX: t.Space(4),
		PaddingY: t.Space(3),
		Gap:      t.Space(1),
	}
	return radius, card, metric
}

func elevationTokensFor(dark bool) elevationTokens {
	if dark {
		return elevationTokens{
			XS: shadowSpec{X: 0, Y: 1, Blur: 2, Spread: 0, Color: ui.RGBA(0, 0, 0, .14)},
			MD: shadowSpec{X: 0, Y: 5, Blur: 14, Spread: -2, Color: ui.RGBA(0, 0, 0, .24)},
			LG: shadowSpec{X: 0, Y: 12, Blur: 28, Spread: -4, Color: ui.RGBA(0, 0, 0, .34)},
		}
	}
	return elevationTokens{
		XS: shadowSpec{X: 0, Y: 1, Blur: 2, Spread: 0, Color: ui.RGBA(0, 0, 0, .04)},
		MD: shadowSpec{X: 0, Y: 4, Blur: 12, Spread: -2, Color: ui.RGBA(0, 0, 0, .08)},
		LG: shadowSpec{X: 0, Y: 10, Blur: 24, Spread: -4, Color: ui.RGBA(0, 0, 0, .12)},
	}
}

func applyShadow(el ui.Element, s shadowSpec) ui.Element {
	if s.Color.A == 0 || (s.Blur == 0 && s.Spread == 0 && s.X == 0 && s.Y == 0) {
		return el.NoShadow()
	}
	return el.Shadow(s.X, s.Y, s.Blur, s.Spread, s.Color)
}

func applyMaterial(c *ui.Context, el ui.Element, kind materialKind) ui.Element {
	t := tokens(c)
	switch kind {
	case materialCanvas:
		return el.Background(t.Color.Canvas).Radius(t.Radius.None).NoShadow()
	case materialSunken:
		return el.Background(t.Color.Sunken).Radius(t.Radius.Inset).Border(1, t.Color.BorderSubtle).NoShadow()
	case materialCard:
		return applyShadow(el.Background(t.Color.Surface).Radius(t.Radius.Surface).Border(t.Card.Edge, t.Color.BorderDefault), t.Elevation.XS)
	case materialInset:
		return el.Background(t.Color.Surface).Radius(t.Radius.Inset).Border(t.Card.Border, t.Color.BorderSubtle).NoShadow()
	case materialControl:
		return el.Background(t.Color.Surface).Radius(t.Radius.Control).Border(1, t.Color.BorderDefault).NoShadow()
	case materialFloating:
		return applyShadow(el.Background(t.Color.Elevated).Radius(t.Radius.Floating).Border(1, t.Color.BorderDefault), t.Elevation.MD)
	case materialModal:
		return applyShadow(el.Background(t.Color.Elevated).Radius(t.Radius.Modal).Border(1, t.Color.BorderDefault), t.Elevation.LG)
	default:
		return el
	}
}

func stateColorsFor(colors colorTokens) stateColors {
	return stateColors{
		Neutral: colors.Neutral.Solid,
		Active:  colors.Active.Solid,
		Info:    colors.Info.Solid,
		Success: colors.Success.Solid,
		Warning: colors.Warning.Solid,
		Danger:  colors.Danger.Solid,
	}
}

func stateColor(c *ui.Context, kind stateKind) ui.Color {
	return tokens(c).State.forKind(kind)
}

func stateScale(c *ui.Context, kind stateKind) stateColorScale {
	colors := tokens(c).Color
	switch kind {
	case stateActive:
		return colors.Active
	case stateInfo:
		return colors.Info
	case stateSuccess:
		return colors.Success
	case stateWarning:
		return colors.Warning
	case stateDanger:
		return colors.Danger
	default:
		return colors.Neutral
	}
}
