package pages

import "github.com/egoist/mygo/ui"

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
			studioIcon(c, opts.Icon, t.Color.FgTertiary)
		}
		if opts.Title != "" {
			ui.Text(c, opts.Title).FontWeight(600).FontSize(t.Type.Section).TextColor(t.Color.FgPrimary)
		}
		if opts.Description != "" {
			ui.Text(c, opts.Description).FontSize(t.Type.Body).TextColor(t.Color.FgSecondary)
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
			trailing := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Tight)
			if opts.RevealTrailingOnIntent && !opts.Selected && !row.Hovered() && !row.Focused() && !row.FocusWithin() {
				trailing.Opacity(0)
			}
			trailing.Children(opts.Trailing)
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
			studioTooltipIfTruncated(c, val, display, value, t.Type.Body, false)
		}
	})
}
