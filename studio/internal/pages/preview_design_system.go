//go:build ui_debug && !release

package pages

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func previewDesignSystemScene(s *viewState) {
	s.downloads = nil
	s.selected = "design_system"
	s.detailID = ""
}

func (p *previewSession) openDesignSystemScene() {
	for i, scene := range previewScenes {
		if scene.labelKey == "preview.scene.design_system" {
			p.loadScene(i)
			return
		}
	}
}

func colorHex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func relativeLuminance(c ui.Color) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255.0
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func contrastRatio(a, b ui.Color) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func swatchRim(c *ui.Context) ui.Color {
	alpha := float32(.12)
	if c.Theme().Dark {
		alpha = .18
	}
	return tokens(c).Color.FgPrimary.Alpha(alpha)
}

func inlineHexChip(c *ui.Context, col ui.Color) {
	t := tokens(c)
	rim := swatchRim(c)
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Box(c).Size(10, 10).Radius(3).Shrink(0).Background(col).Border(1, rim)
		ui.Text(c, colorHex(col)).FontSize(t.Type.ShellDetail).TextColor(t.Color.FgSecondary).FontFeatures("tnum").SingleLine()
	})
}

func (p *previewSession) designSystemView(c *ui.Context) {
	t := tokens(c)
	spec := studioAccentSpecFor(p.accentMode)
	pal := paletteFor(c.Theme().Dark)

	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		ui.Scroll(c.Key("design-system")).Grow(1).Padding(t.Density.Page).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.Section).Children(func() {
				p.dsHeaderCard(c, spec)
				p.dsButtonsCard(c, pal)
				p.dsSurfacesCard(c)
				p.dsStatesCard(c)
				p.dsControlsCard(c)
				p.dsChartCard(c)
				p.dsTokensAndContrastCard(c, pal)
			})
		})
	})
}

func (p *previewSession) dsHeaderCard(c *ui.Context, spec studioAccentSpec) {
	t := tokens(c)
	rim := swatchRim(c)
	selected := core.NormalizeAccent(p.accentMode)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(220).Gap(t.Density.Hairline).Children(func() {
				heading(c, tr(c, "preview.ds.title"), 1)
				ui.Text(c, tr(c, spec.descKey)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			ui.Row(c).Wrap().AlignItems(ui.End).Gap(t.Density.Row).Children(func() {
				prevFont := p.fontSizeMode
				fontOpts := make([]components.NativeSelectOption[string], 0, len(studioFontSizeSpecs))
				for _, fspec := range studioFontSizeSpecs {
					fontOpts = append(fontOpts, components.NativeSelectOption[string]{
						Label: tr(c, fspec.labelKey),
						Value: fspec.id,
					})
				}
				ui.Box(c).MinWidth(296).Children(func() {
					previewCompactSegmentedField(c, tr(c, "settings.appearance.font_size_label"), &p.fontSizeMode, fontOpts)
				})
				if p.fontSizeMode != prevFont {
					p.syncStateFontSize()
					if p.win != nil {
						p.win.Invalidate()
					}
				}
				prevTheme := p.themeMode
				ui.Box(c).MinWidth(184).Children(func() {
					previewCompactSegmentedField(c, tr(c, "preview.theme.label"), &p.themeMode, []components.NativeSelectOption[int]{
						{Label: tr(c, "preview.theme.system"), Value: 0},
						{Label: tr(c, "preview.theme.light"), Value: 1},
						{Label: tr(c, "preview.theme.dark"), Value: 2},
					})
				})
				if p.themeMode != prevTheme {
					p.syncStateTheme()
					p.syncNativeTheme()
				}
			})
		})

		ui.Row(c).FillWidth().Wrap().Gap(t.Density.Row).Children(func() {
			for _, candidate := range studioAccentSpecs {
				candidate := candidate
				active := selected == candidate.id
				chip := ui.Row(c.Key(candidate.id)).AlignItems(ui.Center).MinHeight(32).Gap(t.Density.Row).
					Padding(t.Density.Tight, t.Density.Stack).Radius(t.Radius.Inset).
					Role(ui.RoleButton).Focusable().Label(tr(c, candidate.labelKey))
				if active {
					chip.Background(t.Color.AccentSelection).Border(1, t.Color.Accent)
				} else if chip.Hovered() {
					chip.Background(t.Color.SurfaceHover).Border(1, t.Color.BorderStrong)
				} else {
					chip.Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle)
				}
				if chip.Clicked() {
					p.selectAccent(candidate.id)
				}
				chip.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
						ui.Box(c).Size(10, 10).Radius(t.Radius.Full).Background(candidate.light.background).Border(1, rim)
						ui.Box(c).Size(10, 10).Radius(t.Radius.Full).Background(candidate.light.accent).Border(1, rim)
						ui.Box(c).Size(10, 10).Radius(t.Radius.Full).Background(candidate.dark.background).Border(1, rim)
						ui.Box(c).Size(10, 10).Radius(t.Radius.Full).Background(candidate.dark.accent).Border(1, rim)
					})
					fg := t.Color.FgSecondary
					if active {
						fg = t.Color.FgPrimary
					}
					ui.Text(c, tr(c, candidate.labelKey)).FontSize(t.Type.Caption).TextColor(fg).SingleLine()
					dotColor := ui.Color{}
					if active {
						dotColor = t.Color.Accent
					}
					ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(dotColor)
				})
			}
		})

		ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "preview.ds.jump_hint")).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
			if inlineButton(c, components.ButtonOutline, tr(c, "nav.benchmarks")).Clicked() {
				p.state.selectPage("benchmarks")
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "nav.services")).Clicked() {
				p.state.selectPage("services")
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "nav.library")).Clicked() {
				p.state.selectPage("library")
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "nav.reports")).Clicked() {
				p.state.selectPage("reports")
			}
			if inlineButton(c, components.ButtonOutline, tr(c, "nav.settings")).Clicked() {
				p.state.selectPage("settings")
			}
		})
	})
}

func dsSolidDangerButton(c *ui.Context, label string, pal paletteColors) ui.Element {
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, label, components.ButtonPrimary), sizeMD)
	bg := pal.buttons.dangerSolid
	if bg.A == 0 {
		bg = t.Color.Danger.Solid
	}
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			if pal.buttons.dangerSolidPressed.A > 0 {
				bg = pal.buttons.dangerSolidPressed
			}
		case b.Hovered():
			if pal.buttons.dangerSolidHover.A > 0 {
				bg = pal.buttons.dangerSolidHover
			}
		}
	}
	return b.Background(bg).TextColor(ui.Hex("#ffffff")).Border(1, bg)
}

func (p *previewSession) dsButtonsCard(c *ui.Context, pal paletteColors) {
	t := tokens(c)
	disabledHint := tr(c, "preview.ds.btn.disabled_desc")
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.buttons"), 2)
		ui.Text(c, tr(c, "preview.ds.section.buttons_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)

		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			primaryButton(c, tr(c, "preview.ds.btn.primary"))
			button(c, tr(c, "preview.ds.btn.secondary"))
			outlineButton(c, tr(c, "preview.ds.btn.outline"))
			ghostButton(c, tr(c, "preview.ds.btn.ghost"))
			dangerButton(c, tr(c, "preview.ds.btn.danger_soft"))
			quietDangerButton(c, tr(c, "preview.ds.btn.danger_quiet"))
			dsSolidDangerButton(c, tr(c, "preview.ds.btn.danger_solid"), pal)
		})

		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			leadingIconButton(c, components.ButtonPrimary, reicon.Play, tr(c, "preview.ds.btn.icon_start"))
			leadingIconButton(c, components.ButtonSecondary, reicon.RefreshCw, tr(c, "preview.ds.btn.icon_refresh"))
			trailingIconButton(c, components.ButtonOutline, tr(c, "preview.ds.btn.icon_external"), reicon.ArrowUpRight)
			trailingIconButton(c, components.ButtonGhost, tr(c, "preview.ds.btn.icon_next"), reicon.ChevronRight)
			primaryIconButton(c, reicon.Play, tr(c, "preview.ds.btn.primary"))
			outlineIconButton(c, reicon.SlidersHorizontal, tr(c, "preview.ds.btn.outline"))
			dangerIconButton(c, reicon.Trash, tr(c, "preview.ds.btn.danger_soft"))
			refreshIconButton(c, tr(c, "preview.ds.btn.icon_refresh"), false)
		})

		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			primaryButton(c, tr(c, "preview.ds.btn.primary_disabled")).Disabled(true).Description(disabledHint)
			button(c, tr(c, "preview.ds.btn.secondary_disabled")).Disabled(true).Description(disabledHint)
			dangerButton(c, tr(c, "preview.ds.btn.danger_disabled")).Disabled(true).Description(disabledHint)
			inlineButton(c, components.ButtonPrimary, tr(c, "preview.ds.btn.inline_primary"))
			inlineButton(c, components.ButtonSecondary, tr(c, "preview.ds.btn.inline_secondary"))
			inlineDangerButton(c, tr(c, "preview.ds.btn.inline_danger"))
		})

		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "preview.ds.btn.engine_pills")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			engineHeaderActionButton(c, engineHeaderActionOptions{
				statusText: tr(c, "common.state.running"), actionLabel: tr(c, "common.state.running"),
				kind: stateSuccess, action: "stop",
			})
			engineHeaderActionButton(c, engineHeaderActionOptions{
				statusText: tr(c, "common.state.stopped"), actionLabel: tr(c, "common.state.stopped"),
				kind: stateNeutral, action: "start",
			})
			engineHeaderActionButton(c, engineHeaderActionOptions{
				statusText: tr(c, "common.state.failed"), actionLabel: tr(c, "common.state.failed"),
				kind: stateDanger, action: "start",
			})
		})
	})
}
