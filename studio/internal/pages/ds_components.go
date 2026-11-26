package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
	Leading   func()
	Title     string
	Subtitle  string
	Trailing  func()
	Radius    float32
	Selected  bool
	BoldTitle bool
	OnClick   func()
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
	return components.IconButton(c, icon(), label, style).Radius(tokens(c).Radius.Control)
}

func sizedButton(c *ui.Context, variant components.ButtonVariant, size controlSize, label string) ui.Element {
	return styleButtonSize(c, components.Button(c, label, variant), size)
}

func studioTextBox(c *ui.Context, value *string, opts ...components.TextBoxOptions) ui.Element {
	t := tokens(c)
	var o components.TextBoxOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	el := components.TextBox(c, value, o).Radius(t.Radius.Control)
	if !o.Multiline && o.LeadingIcon == nil && o.LeadingText == "" {
		el.Padding(c.Theme().Space(1.75), t.Density.Stack).MinHeight(t.Control.MD)
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

func badge(c *ui.Context, label string, kind stateKind, opts badgeOptions) ui.Element {
	t := tokens(c)
	scale := stateScale(c, kind)
	fontSize := t.Type.Caption
	padY, padX := t.Density.Tight, t.Density.Stack
	if opts.Size == sizeSM {
		fontSize = max(11, t.Type.ShellLabel)
		padY, padX = t.Density.Tight, t.Density.Row
	}
	if opts.Style == badgeDot {
		row := ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Label(label)
		row.Children(func() {
			dotSize := float32(6)
			if opts.Size == sizeSM {
				dotSize = 5
			}
			ui.Box(c).Size(dotSize, dotSize).Radius(t.Radius.Full).Background(scale.Solid)
			ui.Text(c, label).FontSize(fontSize).TextColor(t.Color.FgPrimary).SingleLine()
		})
		return row
	}
	el := ui.Box(c).AlignSelf(ui.Start).AlignItems(ui.Center).Justify(ui.Center).
		Padding(padY, padX).Radius(t.Radius.Full).Label(label)
	switch {
	case opts.Style == badgeOutline || c.Preferences().HighContrast:
		el.Background(t.Color.Surface).Border(1, scale.Border)
	default:
		el.Background(scale.SubtleBg)
	}
	el.Children(func() {
		ui.Text(c, label).FontSize(fontSize).TextColor(scale.Fg).SingleLine()
	})
	return el
}

func defaultStateIcon(kind stateKind) func() *ui.SVG {
	switch kind {
	case stateSuccess:
		return lucide.CircleCheck
	case stateWarning:
		return lucide.TriangleAlert
	case stateDanger:
		return lucide.CircleAlert
	default:
		return lucide.Info
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
	pad := t.Density.Stack
	if opts.Size == bannerCompact {
		pad = t.Density.Row
	}
	banner := ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(pad).Radius(t.Radius.Inset)
	if c.Preferences().HighContrast {
		banner.Background(scale.SubtleBg).Border(1, scale.Border)
	} else {
		banner.Background(scale.SubtleBg).BorderWidth(0, 0, 0, 3).BorderColor(scale.Solid)
	}
	banner.Children(func() {
		rowAlign := ui.Start
		if opts.Size == bannerCompact && opts.Title == "" && opts.Actions == nil && opts.Details == nil {
			rowAlign = ui.Center
		}
		ui.Row(c).AlignItems(rowAlign).Gap(t.Density.Row).Children(func() {
			iconSize := t.Type.Body
			if opts.Size == bannerCompact {
				iconSize = t.Type.Caption
			}
			ui.Box(c).Children(func() {
				components.Icon(c, icon).Size(iconSize, iconSize).TextColor(scale.Fg)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
				if opts.Title != "" {
					ui.Text(c, opts.Title).Bold().FontSize(t.Type.Body).TextColor(scale.Fg)
				}
				if opts.Message != "" {
					size := t.Type.Body
					if opts.Size == bannerCompact {
						size = t.Type.Caption
					}
					ui.Text(c, opts.Message).FontSize(size).Role(ui.RoleStatus).TextColor(scale.Fg)
				}
				if opts.Actions != nil {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(opts.Actions)
				}
				if opts.Details != nil {
					opts.Details()
				}
			})
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

func listRow(c *ui.Context, opts listRowOptions) ui.Element {
	t := tokens(c)
	radius := opts.Radius
	if radius <= 0 {
		radius = concentricRadius(t.Radius.Inset, t.Density.Tight)
	}
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Row, t.Density.Stack).Radius(radius).Label(opts.Title)
	switch {
	case opts.Selected:
		row.Background(t.Color.AccentSelection)
	case opts.OnClick != nil && row.Hovered():
		row.Background(t.Color.SurfaceHover)
	}
	if opts.OnClick != nil {
		row.Role(ui.RoleButton).Focusable().OnClick(opts.OnClick)
	}
	row.Children(func() {
		if opts.Leading != nil {
			opts.Leading()
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			title := ui.Text(c, opts.Title).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
			if opts.Selected || opts.BoldTitle {
				title.Bold()
			}
			if opts.Subtitle != "" {
				ui.Text(c, opts.Subtitle).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
			}
		})
		if opts.Trailing != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(opts.Trailing)
		}
	})
	return row
}

func keyValueRow(c *ui.Context, key, value string, tabular bool) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Density.Section).
		Padding(t.Density.Tight, 0).Children(func() {
		ui.Box(c).Width(140).Shrink(0).Children(func() {
			ui.Text(c, key).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		display := value
		valColor := t.Color.FgPrimary
		if display == "" {
			display = tr(c, "common.missing")
			valColor = t.Color.FgTertiary
		}
		val := ui.Text(c, display).FontSize(t.Type.Body).TextColor(valColor).Selectable()
		if tabular {
			val.FontFeatures("tnum")
		}
	})
}

func pathKeyValueRow(c *ui.Context, key, value string) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Section).
		Padding(t.Density.Tight, 0).Children(func() {
		ui.Box(c).Width(140).Shrink(0).Children(func() {
			ui.Text(c, key).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		display := value
		valColor := t.Color.FgPrimary
		if display == "" {
			display = tr(c, "common.missing")
			valColor = t.Color.FgTertiary
		}
		val := ui.Text(c, display).FontSize(t.Type.Body).TextColor(valColor).Grow(1).MinWidth(0).SingleLine().TruncateMiddle().Selectable()
		if value != "" {
			components.Tooltip(c, val, value)
		}
	})
}
