package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func studioDialog(c *ui.Context, open *bool, options components.DialogOptions, build func(panel ui.Element)) ui.Element {
	var panel ui.Element
	if !*open {
		ui.Overlay(c, func() {})
		return panel
	}
	t := tokens(c)
	w, h := c.Size()
	inset := options.Inset
	if inset <= 0 {
		inset = 20
	}
	ui.Overlay(c, func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, .28))
		back := ui.Column(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			AlignItems(ui.Center).Padding(inset).Modal()
		clicked := back.Clicked()
		escape := back.OverlayShortcut(0, ui.KeyEscape)
		if clicked && !options.DisableBackdropClose || escape && !options.DisableEscapeClose {
			*open = false
		}
		back.Children(func() {
			ui.Box(c).Grow(1).MinHeight(0)
			panel = ui.Column(c).Role(ui.RoleDialog).Label(options.Label).
				Width(c.Theme().Space(110)).
				MaxWidth(max(0, w-2*inset)).MaxHeight(max(0, h-2*inset)).Shrink(0).
				Padding(t.Density.Section).Gap(t.Density.Stack).Radius(t.Radius.Modal).
				Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).
				TextColor(t.Color.FgPrimary)
			panel = applyShadow(panel, t.Elevation.LG)
			panel.Clicked()
			panel.Children(func() {
				if build != nil {
					build(panel)
				}
			})
			ui.Box(c).Grow(2).MinHeight(0)
		})
	})
	return panel
}

func studioAlertDialog(c *ui.Context, open *bool, options components.AlertDialogOptions) components.DialogResult {
	return studioAlertDialogStyled(c, open, options, false)
}

func studioDestructiveAlertDialog(c *ui.Context, open *bool, options components.AlertDialogOptions) components.DialogResult {
	return studioAlertDialogStyled(c, open, options, true)
}

func studioAlertDialogStyled(c *ui.Context, open *bool, options components.AlertDialogOptions, destructive bool) components.DialogResult {
	result := components.DialogNone
	wasOpen := *open
	opts := options.DialogOptions
	if opts.Label == "" {
		opts.Label = options.Title
	}
	t := tokens(c)
	studioDialog(c, open, opts, func(panel ui.Element) {
		panel.Role(ui.RoleAlertDialog)
		if options.Title != "" {
			components.Text(c, options.Title).Bold()
		}
		if options.Message != "" {
			components.Text(c, options.Message).TextColor(t.Color.FgSecondary)
		}
		ui.Row(c).FillWidth().Wrap().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if !options.HideCancel && button(c, options.CancelLabel).Clicked() {
				result = components.DialogCancelled
				*open = false
			}
			confirmBtn := primaryButton
			if destructive {
				confirmBtn = dangerButton
			}
			if confirmBtn(c, options.ConfirmLabel).Clicked() {
				result = components.DialogConfirmed
				*open = false
			}
		})
	})
	if wasOpen && !*open && result == components.DialogNone {
		result = components.DialogCancelled
	}
	return result
}
