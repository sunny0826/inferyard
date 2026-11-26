package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func studioDialog(c *ui.Context, open *bool, options components.DialogOptions, build func(panel ui.Element)) ui.Element {
	t := tokens(c)
	return components.Dialog(c, open, options, func(panel ui.Element) {
		// Clear onevra's glass fillMaterial before setting the solid elevated surface:
		// MyGo's Background() only resets fillGradient/fillConic/fillRadial.
		panel.Padding(t.Density.Section).Gap(t.Density.Stack).Radius(t.Radius.Modal).
			Gradient(t.Color.Elevated, t.Color.Elevated, 0).
			Background(t.Color.Elevated).Border(1, t.Color.BorderDefault).
			TextColor(t.Color.FgPrimary)
		panel = applyShadow(panel, t.Elevation.LG)
		if build != nil {
			build(panel)
		}
	})
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
