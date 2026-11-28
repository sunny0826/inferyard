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
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, label, components.ButtonOutline), sizeMD)
	if !c.Preferences().HighContrast {
		b.Background(t.Color.Surface).TextColor(t.Color.FgPrimary).Border(1, t.Color.BorderDefault)
		applyDeferredButtonFace(b, t.Radius.Control, t.Color.SurfaceHover, t.Color.SurfacePressed, t.Color.BorderStrong, t.Color.BorderStrong)
	}
	return b
}

func ghostButton(c *ui.Context, label string) ui.Element {
	return styleButtonSize(c, components.Button(c, label, components.ButtonGhost), sizeMD)
}

func iconButton(c *ui.Context, icon func() *ui.SVG, label string, style components.IconButtonStyle) ui.Element {
	b := components.IconButton(c, icon(), "", style).Radius(tokens(c).Radius.Control)
	if label != "" {
		b.Label(label)
		studioTooltip(c, b, label)
	}
	return b
}

func sizedButton(c *ui.Context, variant components.ButtonVariant, size controlSize, label string) ui.Element {
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, label, variant), size)
	switch variant {
	case components.ButtonSecondary:
		applySecondaryButtonPalette(c, b)
	case components.ButtonOutline:
		if !c.Preferences().HighContrast {
			b.Background(t.Color.Surface).TextColor(t.Color.FgPrimary).Border(1, t.Color.BorderDefault)
			applyDeferredButtonFace(b, t.Radius.Control, t.Color.SurfaceHover, t.Color.SurfacePressed, t.Color.BorderStrong, t.Color.BorderStrong)
		}
	}
	return b
}

func badge(c *ui.Context, label string, kind stateKind, opts badgeOptions) ui.Element {
	t := tokens(c)
	scale := stateScale(c, kind)
	fontSize := t.Type.Caption
	padY, padX := t.Density.Tight, t.Density.Stack
	if opts.Size == sizeSM {
		fontSize = c.Theme().Rem(.86)
		padY, padX = t.Density.Tight, t.Density.Row
	}
	if opts.Style == badgeDot {
		row := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Tight).Label(label)
		row.Children(func() {
			dotSize := float32(6)
			if opts.Size == sizeSM {
				dotSize = 5
			}
			ui.Box(c).Size(dotSize, dotSize).Shrink(0).Radius(t.Radius.Full).Background(scale.Solid)
			ui.Text(c, label).FontSize(fontSize).FontFeatures("tnum").TextColor(t.Color.FgPrimary).SingleLine()
		})
		return row
	}
	el := ui.Box(c).Shrink(0).AlignSelf(ui.Start).AlignItems(ui.Center).Justify(ui.Center).
		Padding(padY, padX).Radius(t.Radius.Full).Label(label)
	switch {
	case opts.Style == badgeOutline || c.Preferences().HighContrast:
		el.Background(t.Color.Surface).Border(1, scale.Border)
	default:
		el.Background(scale.SubtleBg)
	}
	el.Children(func() {
		ui.Text(c, label).FontSize(fontSize).FontFeatures("tnum").TextColor(scale.Fg).SingleLine()
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
	return el.Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle)
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

	if isQuietInlineNotice(opts) {
		align := ui.Center
		if opts.Title != "" {
			align = ui.Start
		}
		ui.Row(c).FillWidth().AlignItems(align).Gap(t.Density.Row).Padding(2, 0).Children(func() {
			if opts.Kind == stateSuccess || opts.Icon != nil {
				topPad := float32(0)
				if opts.Title != "" {
					topPad = 2
				}
				ui.Box(c).Padding(topPad, 0, 0, 0).Shrink(0).Children(func() {
					components.Icon(c, icon).Size(14, 14).TextColor(semanticAccentColor(c, scale))
				})
			} else {
				topPad := float32(0)
				if opts.Title != "" {
					topPad = 5
				}
				ui.Box(c).Padding(topPad, 0, 0, 0).Shrink(0).Children(func() {
					ui.Box(c).Size(7, 7).Radius(t.Radius.Full).Background(scale.Solid)
				})
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				renderNoticeBodyText(c, opts, compact, opts.Kind != stateSuccess)
			})
		})
		return
	}

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
			iconTop := float32(0)
			if rowAlign == ui.Start {
				iconTop = 2
			}
			ui.Box(c).Padding(iconTop, 0, 0, 0).Shrink(0).Children(func() {
				components.Icon(c, icon).Size(15, 15).TextColor(semanticAccentColor(c, scale))
			})
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

func emptyState(c *ui.Context, opts emptyStateOptions) {
	t := tokens(c)
	col := ui.Column(c).FillWidth().AlignItems(ui.Center).Justify(ui.Center).
		Gap(t.Density.Row).Padding(t.Card.EmptyPadding, t.Density.Section)
	switch opts.Surface {
	case emptyCard:
		applyMaterial(c, col, materialCard)
	case emptySunken:
		applyMaterial(c, col, materialSunken)
	}
	col.Children(func() {
		if opts.Icon != nil {
			components.Icon(c, opts.Icon).Size(28, 28).TextColor(t.Color.FgTertiary)
		}
		if opts.Title != "" {
			ui.Text(c, opts.Title).Bold().FontSize(t.Type.Section).TextColor(t.Color.FgPrimary)
		}
		if opts.Description != "" {
			ui.Text(c, opts.Description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if opts.Action != nil {
			ui.Row(c).Padding(t.Density.Tight, 0, 0, 0).Children(opts.Action)
		}
	})
}
