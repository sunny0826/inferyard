package pages

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

type validatedFieldOptions struct {
	Label       string
	Description string
	Error       string
	Input       func()
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

type studioSelectParts[T comparable] struct {
	*components.SelectParts[T]
	ctx *ui.Context
}

func (s *studioSelectParts[T]) Popup(build func(panel ui.Element)) ui.Element {
	t := tokens(s.ctx)
	return s.SelectParts.Popup(func(panel ui.Element) {
		panel.Padding(t.Density.Tight).Radius(t.Radius.Floating).
			Gradient(t.Color.Elevated, t.Color.Elevated, 0).
			Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).
			TextColor(t.Color.FgPrimary)
		panel = applyShadow(panel, t.Elevation.MD)
		if build != nil {
			build(panel)
		}
	})
}

func studioSelect[T comparable](c *ui.Context, selected *T) *studioSelectParts[T] {
	t := tokens(c)
	sel := components.Select(c, selected)
	sel.Trigger.MinHeight(t.Control.MD).
		Padding(t.Density.Tight, c.Theme().Space(6), t.Density.Tight, t.Density.Stack).
		Radius(t.Radius.Control)
	return &studioSelectParts[T]{SelectParts: sel, ctx: c}
}

// studioUnderlineTabs renders an accent-underlined tab strip using cached layout bounds
// during exit transitions so closing a parent Dialog or Drawer never reads an expired Element.
func studioUnderlineTabs(c *ui.Context, selected *int, labels ...string) ui.TabsParts {
	theme := c.Theme()
	t := tokens(c)
	tabs := ui.TabsBase(c, selected, len(labels))
	tabs.List.Gap(theme.Space(1))
	var active ui.Element
	tabs.List.Children(func() {
		for i, label := range labels {
			tab := tabs.Tab(i).Padding(theme.Space(2), theme.Space(3)).FocusRing(false)
			on := i == *selected
			fg := t.Color.FgSecondary
			if on {
				fg = t.Color.FgPrimary
				active = tab
			}
			tab.TextColor(fg)
			focus := tab.FocusVisible()
			tab.DrawOver(func(p *ui.Painter, r ui.Rect) {
				if focus {
					p.FocusRing(r, [4]float32{t.Radius.Control, t.Radius.Control, t.Radius.Control, t.Radius.Control})
				}
			})
			tab.AlignItems(ui.Center).Gap(theme.Space(1.5)).Children(func() {
				components.Text(c, label).TextColor(fg).SingleLine()
			})
		}
	})
	if len(labels) > 0 {
		var lastBounds ui.Rect
		var hasBounds bool
		tabs.List.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if active.Valid() {
				lastBounds = active.LayoutBounds()
				hasBounds = true
			}
			if !hasBounds {
				return
			}
			in, h := theme.Space(1.5), theme.Space(0.5)
			x, w := lastBounds.X-r.X+in, max(0, lastBounds.W-2*in)
			if tabs.List.Valid() && !c.Preferences().ReduceMotion {
				x = tabs.List.Animate("indicator-x", x, 180*time.Millisecond)
				w = tabs.List.Animate("indicator-width", w, 180*time.Millisecond)
			}
			p.Fill(ui.Rect{X: r.X + x, Y: lastBounds.Y + lastBounds.H - h, W: w, H: h}, t.Color.Accent, h/2)
		})
	}
	return tabs
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
				ui.Text(c, opts.Subtitle).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
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
		val := ui.Text(c, display).FontSize(t.Type.Body).TextColor(valColor).Grow(1).MinWidth(0).Selectable()
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
			studioTooltipIfTruncated(c, val, display, value, t.Type.Body, false)
		}
	})
}
