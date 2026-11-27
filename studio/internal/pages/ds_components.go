package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

type controlSize int

const (
	sizeMD controlSize = iota
	sizeSM
	sizeLG
)

type badgeStyle int

const (
	badgeSoft badgeStyle = iota
	badgeOutline
	badgeDot
)

type badgeOptions struct {
	Size  controlSize
	Style badgeStyle
}

type bannerSize int

const (
	bannerStandard bannerSize = iota
	bannerCompact
)

type noticeBannerOptions struct {
	Kind    stateKind
	Size    bannerSize
	Icon    func() *ui.SVG
	Title   string
	Message string
	Actions func()
	Details func()
}

type validatedFieldOptions struct {
	Label       string
	Description string
	Error       string
	Input       func()
}

type emptyStateSurface int

const (
	emptySunken emptyStateSurface = iota
	emptyCard
	emptyInline
)

type emptyStateOptions struct {
	Icon        func() *ui.SVG
	Title       string
	Description string
	Surface     emptyStateSurface
	Action      func()
}

type listRowOptions struct {
	Leading                func()
	Title                  string
	Subtitle               string
	Trailing               func()
	Radius                 float32
	Selected               bool
	BoldTitle              bool
	RevealTrailingOnIntent bool
	OnClick                func()
}

func styleButtonSize(c *ui.Context, b ui.Element, size controlSize) ui.Element {
	t := tokens(c)
	b.Radius(t.Radius.Control).MaxWidthPercent(100).AlignSelf(ui.Start)
	switch size {
	case sizeSM:
		return b.MinHeight(t.Control.SM).Padding(t.Density.Hairline, t.Density.Row).FontSize(t.Type.Caption)
	case sizeLG:
		return b.MinHeight(t.Control.LG).Padding(t.Density.Row, t.Density.Section).FontSize(t.Type.Body)
	default:
		return b.MinHeight(t.Control.MD).Padding(t.Density.Tight, t.Density.Stack)
	}
}

func outlineButton(c *ui.Context, label string) ui.Element {
	return styleButtonSize(c, components.Button(c, label, components.ButtonOutline), sizeMD)
}

func ghostButton(c *ui.Context, label string) ui.Element {
	return styleButtonSize(c, components.Button(c, label, components.ButtonGhost), sizeMD)
}

func iconButton(c *ui.Context, icon func() *ui.SVG, label string, style components.IconButtonStyle) ui.Element {
	t := tokens(c)
	size := t.Control.MD
	if style == components.IconButtonCompact {
		size = studioIconBoxSize
	}
	b := ui.ButtonBase(c).
		Size(size, size).
		Radius(t.Radius.Control).
		AlignItems(ui.Center).
		Justify(ui.Center).
		TextColor(t.Color.FgSecondary)
	if !b.IsDisabled() && style != components.IconButtonPlain {
		switch {
		case b.Pressed():
			b.Background(t.Color.SurfacePressed).TextColor(t.Color.FgPrimary)
		case b.Hovered():
			b.Background(t.Color.SurfaceHover).TextColor(t.Color.FgPrimary)
		}
	}
	b.Children(func() {
		studioIcon(c, icon, ui.Color{})
	})
	if label != "" {
		b.Label(label)
		studioTooltip(c, b, label)
	}
	return b
}

func sizedButton(c *ui.Context, variant components.ButtonVariant, size controlSize, label string) ui.Element {
	b := styleButtonSize(c, components.Button(c, label, variant), size)
	if variant == components.ButtonSecondary {
		applySecondaryButtonPalette(c, b)
	}
	return b
}

func studioTextBox(c *ui.Context, value *string, opts ...components.TextBoxOptions) ui.Element {
	t := tokens(c)
	var o components.TextBoxOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	el := components.TextBox(c, value, o).Radius(t.Radius.Control)
	if !o.Multiline && o.LeadingIcon == nil && o.LeadingText == "" {
		el.Padding(t.Density.Tight, t.Density.Stack).MinHeight(t.Control.MD)
	} else if !o.Multiline {
		el.MinHeight(t.Control.MD)
	}
	return el
}

func studioSelect[T comparable](c *ui.Context, selected *T) *components.SelectParts[T] {
	t := tokens(c)
	sel := components.Select(c, selected)
	sel.Trigger.MinHeight(t.Control.MD).
		Padding(t.Density.Tight, c.Theme().Space(6), t.Density.Tight, t.Density.Stack).
		Radius(t.Radius.Control)
	return sel
}

func badgeColorsFor(c *ui.Context, kind stateKind) (bg, fg, border, dot ui.Color) {
	t := tokens(c)
	bg = t.Color.Sunken
	fg = t.Color.FgSecondary
	border = t.Color.BorderDefault
	dot = t.Color.FgSecondary
	switch kind {
	case stateActive:
		bg = t.Color.AccentSubtleBg
		fg = t.Color.Active.Fg
		border = t.Color.Active.Border
		dot = t.Color.Accent
	case stateWarning:
		bg = t.Color.Sunken
		fg = t.Color.Warning.Fg
		border = t.Color.Warning.Border
		dot = t.Color.Warning.Solid
	case stateDanger:
		bg = t.Color.Danger.SubtleBg
		fg = t.Color.Danger.Fg
		border = t.Color.Danger.Border
		dot = t.Color.Danger.Solid
	}
	return bg, fg, border, dot
}

func badge(c *ui.Context, label string, kind stateKind, opts badgeOptions) ui.Element {
	t := tokens(c)
	fontSize := t.Type.Caption
	padY, padX := t.Density.Hairline, t.Density.Tight
	bg, fg, border, dotColor := badgeColorsFor(c, kind)

	if opts.Style == badgeDot {
		row := ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Label(label)
		row.Children(func() {
			ui.Box(c).Size(8, 8).Radius(t.Radius.Full).Background(dotColor)
			ui.Text(c, label).FontSize(fontSize).TextColor(t.Color.FgPrimary).SingleLine()
		})
		return row
	}
	el := ui.Box(c).AlignSelf(ui.Start).AlignItems(ui.Center).Justify(ui.Center).
		Padding(padY, padX).Radius(t.Radius.Full).Label(label)
	switch {
	case opts.Style == badgeOutline || c.Preferences().HighContrast:
		el.Background(t.Color.Surface).Border(1, border)
	default:
		el.Background(bg)
	}
	el.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Hairline).Children(func() {
			if kind == stateWarning {
				ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Shrink(0).Background(dotColor)
			}
			ui.Text(c, label).FontSize(fontSize).TextColor(fg).SingleLine()
		})
	})
	return el
}

func defaultStateIcon(kind stateKind) func() *ui.SVG {
	switch kind {
	case stateSuccess:
		return reicon.CircleCheck
	case stateWarning:
		return reicon.TriangleAlert
	case stateDanger:
		return reicon.CircleAlert
	default:
		return reicon.Info
	}
}

func semanticAccentColor(c *ui.Context, scale stateColorScale) ui.Color {
	if c.Preferences().HighContrast {
		return tokens(c).Color.FgPrimary
	}
	if c.Theme().Dark {
		return scale.Fg
	}
	return scale.Solid
}

func applyNeutralNoticeStrip(c *ui.Context, el ui.Element, scale stateColorScale, compact bool) ui.Element {
	t := tokens(c)
	padY, padX := t.Density.Stack, t.Density.Section
	if compact {
		padY, padX = t.Density.Row, t.Density.Stack
	}
	el.Padding(padY, padX).Radius(t.Radius.Inset)
	if c.Preferences().HighContrast {
		return el.Background(t.Color.Surface).Border(1.5, scale.Solid)
	}
	return el.Background(t.Color.Sunken).NoShadow()
}

func isQuietInlineNotice(opts noticeBannerOptions) bool {
	if opts.Actions != nil || opts.Details != nil {
		return false
	}
	switch opts.Kind {
	case stateWarning, stateDanger:
		return false
	default:
		return true
	}
}

func renderNoticeBodyText(c *ui.Context, opts noticeBannerOptions, compact, quietInfo bool) {
	t := tokens(c)
	if opts.Title != "" {
		size := t.Type.Body
		if compact {
			size = t.Type.Caption
		}
		ui.Text(c, opts.Title).Bold().FontSize(size).TextColor(t.Color.FgPrimary)
	}
	if opts.Message != "" {
		size := t.Type.Body
		msgColor := t.Color.FgPrimary
		if opts.Title != "" {
			size = t.Type.Caption
			msgColor = t.Color.FgSecondary
		} else if quietInfo {
			size = t.Type.Caption
			msgColor = t.Color.FgSecondary
		} else if compact {
			size = t.Type.Caption
		}
		ui.Text(c, opts.Message).FontSize(size).Role(ui.RoleStatus).TextColor(msgColor)
	}
}

func noticeBanner(c *ui.Context, opts noticeBannerOptions) {
	if opts.Title == "" && opts.Message == "" && opts.Actions == nil && opts.Details == nil {
		return
	}
	t := tokens(c)
	scale := stateScale(c, opts.Kind)
	icon := opts.Icon
	if icon == nil {
		icon = defaultStateIcon(opts.Kind)
	}
	compact := opts.Size == bannerCompact

	// Tier 1: Quiet frameless inline status line for low-severity informational/success notices.
	if isQuietInlineNotice(opts) {
		align := ui.Center
		if opts.Title != "" {
			align = ui.Start
		}
		ui.Row(c).FillWidth().AlignItems(align).Gap(t.Density.Row).Padding(t.Density.Hairline, 0).Children(func() {
			if opts.Kind == stateSuccess || opts.Icon != nil {
				studioIcon(c, icon, semanticAccentColor(c, scale))
			} else {
				ui.Box(c).Size(studioIconBoxSize, studioIconBoxSize).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(t.Radius.Full).Background(scale.Solid)
				})
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
				renderNoticeBodyText(c, opts, compact, opts.Kind != stateSuccess)
			})
		})
		return
	}

	// Tier 3: Neutral sunken action strip for warnings, errors, or actionable notices.
	strip := applyNeutralNoticeStrip(c, ui.Column(c).FillWidth().Gap(t.Density.Row), scale, compact)
	strip.Children(func() {
		singleLineCompact := compact && opts.Title == "" && opts.Details == nil
		rowAlign := ui.Start
		if singleLineCompact || (opts.Actions != nil && opts.Details == nil) {
			rowAlign = ui.Center
		}
		gap := t.Density.Stack
		if compact {
			gap = t.Density.Row
		}
		inlineActions := opts.Actions != nil && opts.Details == nil
		ui.Row(c).FillWidth().AlignItems(rowAlign).Wrap().Gap(gap).Children(func() {
			studioIcon(c, icon, semanticAccentColor(c, scale))
			ui.Column(c).Grow(1).MinWidth(180).Gap(t.Density.Tight).Children(func() {
				renderNoticeBodyText(c, opts, compact, false)
				if !inlineActions && opts.Actions != nil {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(opts.Actions)
				}
				if opts.Details != nil {
					opts.Details()
				}
			})
			if inlineActions {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(opts.Actions)
			}
		})
	})
}

func infoBanner(c *ui.Context, message string) {
	if message == "" {
		return
	}
	noticeBanner(c, noticeBannerOptions{Kind: stateInfo, Message: message})
}

func validatedField(c *ui.Context, opts validatedFieldOptions) ui.Element {
	t := tokens(c)
	f := ui.Field(c, opts.Label, func() {
		ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
			if opts.Input != nil {
				opts.Input()
			}
			if opts.Error != "" {
				ui.Text(c, opts.Error).FontSize(t.Type.Caption).TextColor(t.Color.Danger.Fg).Role(ui.RoleAlert)
			}
		})
	})
	if opts.Description != "" {
		f.Description(opts.Description)
	}
	return f
}

func switchField(c *ui.Context, label, description string, on *bool) ui.Element {
	return labeledSwitchRow(c, label, description, label, on)
}

func labeledSwitchRow(c *ui.Context, label, description, switchLabel string, on *bool) ui.Element {
	t := tokens(c)
	return ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Section).Children(func() {
		labelCol := ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline)
		if on != nil && labelCol.Clicked() {
			*on = !*on
		}
		labelCol.Children(func() {
			ui.Text(c, label).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
			if description != "" {
				ui.Text(c, description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
		})
		components.Switch(c, on, switchLabel, components.SwitchOptions{HideLabel: true})
	})
}

func monoField(c *ui.Context, label, description, placeholder, errMsg string, value *string) ui.Element {
	return validatedField(c, validatedFieldOptions{
		Label:       label,
		Description: description,
		Error:       errMsg,
		Input: func() {
			el := studioTextBox(c, value).FillWidth()
			if placeholder != "" {
				el.Placeholder(placeholder)
			}
		},
	})
}
