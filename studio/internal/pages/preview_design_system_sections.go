//go:build ui_debug && !release

package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

func (p *previewSession) dsSurfacesCard(c *ui.Context) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.surfaces"), 2)
		cols := 4
		if compactLayout(c) {
			cols = 2
		}
		ui.Grid(c).Columns(cols).Gap(t.Density.Row).Children(func() {
			layers := []struct {
				nameKey string
				kind    materialKind
				col     ui.Color
			}{
				{"preview.ds.surface.canvas", materialCanvas, t.Color.Canvas},
				{"preview.ds.surface.sunken", materialSunken, t.Color.Sunken},
				{"preview.ds.surface.card", materialCard, t.Color.Surface},
				{"preview.ds.surface.floating", materialFloating, t.Color.Elevated},
			}
			for _, layer := range layers {
				box := applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Stack), layer.kind)
				if layer.kind == materialCanvas {
					box.Radius(t.Radius.Inset).Border(1, t.Color.BorderSubtle)
				}
				box.Children(func() {
					ui.Text(c, tr(c, layer.nameKey)).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
					ui.Text(c, tr(c, "preview.ds.surface.secondary_sample")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					inlineHexChip(c, layer.col)
				})
			}
		})
	})
}

func (p *previewSession) dsStatesCard(c *ui.Context) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.states"), 2)

		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			badge(c, tr(c, "preview.ds.state.neutral"), stateNeutral, badgeOptions{Style: badgeSoft})
			badge(c, tr(c, "preview.ds.state.info"), stateInfo, badgeOptions{Style: badgeSoft})
			badge(c, tr(c, "preview.ds.state.success"), stateSuccess, badgeOptions{Style: badgeSoft})
			badge(c, tr(c, "preview.ds.state.warning"), stateWarning, badgeOptions{Style: badgeSoft})
			badge(c, tr(c, "preview.ds.state.danger"), stateDanger, badgeOptions{Style: badgeSoft})
			badge(c, tr(c, "preview.ds.state.dot"), stateSuccess, badgeOptions{Style: badgeDot})
			badge(c, tr(c, "preview.ds.state.outline"), stateInfo, badgeOptions{Style: badgeOutline})
		})

		ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
			noticeBanner(c, noticeBannerOptions{Kind: stateInfo, Size: bannerCompact, Message: tr(c, "preview.ds.banner.info")})
			noticeBanner(c, noticeBannerOptions{Kind: stateSuccess, Size: bannerCompact, Message: tr(c, "preview.ds.banner.success")})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Wrap().Gap(t.Density.Row).Children(func() {
				applyMaterial(c, ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).
					Padding(t.Density.Tight+2, t.Density.Stack).Radius(t.Radius.Full), materialFloating).Children(func() {
					components.Icon(c, reicon.CircleCheck).Size(14, 14).TextColor(semanticAccentColor(c, t.Color.Success))
					ui.Text(c, tr(c, "preview.ds.banner.success")).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).SingleLine()
				})
				if inlineButton(c, components.ButtonOutline, tr(c, "preview.ds.alt.c.try_toast")).Clicked() {
					p.state.enqueueStatusToast(tr(c, "preview.ds.banner.success"))
				}
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Title:   tr(c, "preview.ds.banner.warning_title"),
				Message: tr(c, "preview.ds.banner.warning"),
				Actions: func() { inlineButton(c, components.ButtonSecondary, tr(c, "preview.ds.alt.c.inspect")) },
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateDanger,
				Title:   tr(c, "preview.ds.banner.danger_title"),
				Message: tr(c, "preview.ds.banner.danger"),
				Actions: func() { inlineDangerButton(c, tr(c, "preview.ds.alt.retry")) },
			})
		})

		ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
			pipelineRail(c, pipelineCardOptions{
				Key:      "ds-pipeline-active",
				RowLabel: tr(c, "preview.ds.progress.active"),
				Steps: []workflowStepItem{
					{Label: tr(c, "dashboard.stage.prepare")},
					{Label: tr(c, "dashboard.stage.device_check")},
					{Label: tr(c, "dashboard.stage.configure")},
					{Label: tr(c, "dashboard.stage.bind")},
					{Label: tr(c, "dashboard.stage.run")},
					{Label: tr(c, "dashboard.stage.report")},
				},
				Current: 4,
				Progress: workflowProgressInfo{
					Active:       true,
					Value:        0.68,
					PercentLabel: "68%",
					AriaLabel:    tr(c, "preview.ds.progress.active"),
				},
			})
			progressBar(c, 0.68, tr(c, "preview.ds.progress.active"))
			progressBar(c, 1.0, tr(c, "preview.ds.progress.done"))
		})
	})
}

func (p *previewSession) dsControlsCard(c *ui.Context) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.controls"), 2)
		cols := 2
		if compactLayout(c) {
			cols = 1
		}
		ui.Grid(c).Columns(cols).Gap(t.Density.Section).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
				monoField(c, tr(c, "preview.ds.control.input"), tr(c, "preview.ds.control.input_hint"), "http://127.0.0.1:48857", "", &p.dsInput)
				switchField(c, tr(c, "preview.ds.control.switch"), tr(c, "preview.ds.control.switch_desc"), &p.dsSwitch)
				components.Checkbox(c, &p.dsCheck, tr(c, "preview.ds.control.checkbox"))
			})
			ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
				listRow(c, listRowOptions{
					Leading:  func() { components.Icon(c, reicon.Play).Size(15, 15).TextColor(t.Color.FgPrimary) },
					Title:    tr(c, "preview.ds.row.selected_title"),
					Subtitle: tr(c, "preview.ds.row.selected_sub"),
					Selected: true,
					Trailing: func() { badge(c, tr(c, "preview.ds.state.success"), stateSuccess, badgeOptions{Size: sizeSM}) },
				})
				listRow(c, listRowOptions{
					Leading:  func() { components.Icon(c, reicon.Server).Size(15, 15).TextColor(t.Color.FgSecondary) },
					Title:    tr(c, "preview.ds.row.normal_title"),
					Subtitle: tr(c, "preview.ds.row.normal_sub"),
					OnClick:  func() {},
					Trailing: func() { badge(c, tr(c, "preview.ds.state.info"), stateInfo, badgeOptions{Size: sizeSM}) },
				})
			})
		})
	})
}

func (p *previewSession) dsChartCard(c *ui.Context) {
	t := tokens(c)
	rim := swatchRim(c)
	heights := [6]float32{82, 68, 56, 74, 48, 38}
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.chart"), 2)
		ui.Row(c).FillWidth().AlignItems(ui.End).Gap(t.Density.Stack).Height(112).Children(func() {
			for i, col := range t.Color.Chart {
				ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Justify(ui.End).Gap(t.Density.Tight).Children(func() {
					ui.Box(c).FillWidth().MaxWidth(48).Height(heights[i]).Radius(t.Radius.SM).Background(col).Border(1, rim)
					inlineHexChip(c, col)
				})
			}
		})
	})
}

func (p *previewSession) dsTokensAndContrastCard(c *ui.Context, pal paletteColors) {
	t := tokens(c)
	rim := swatchRim(c)
	checks := []struct {
		labelKey string
		fg, bg   ui.Color
	}{
		{"preview.ds.contrast.body", t.Color.FgPrimary, t.Color.Surface},
		{"preview.ds.contrast.muted", t.Color.FgSecondary, t.Color.Surface},
		{"preview.ds.contrast.primary_btn", t.Color.FgOnAccent, t.Color.Accent},
		{"preview.ds.contrast.primary_hover", t.Color.FgOnAccent, t.Color.AccentHover},
		{"preview.ds.contrast.secondary_btn", t.Color.FgPrimary, pal.buttons.secondaryBg},
		{"preview.ds.contrast.danger_btn", pal.buttons.dangerFg, pal.buttons.dangerBg},
		{"preview.ds.contrast.danger_solid", ui.Hex("#ffffff"), pal.buttons.dangerSolid},
		{"preview.ds.contrast.success_badge", t.Color.Success.Fg, t.Color.Success.SubtleBg},
		{"preview.ds.contrast.warning_badge", t.Color.Warning.Fg, t.Color.Warning.SubtleBg},
	}

	swatches := []struct {
		name string
		col  ui.Color
	}{
		{"Canvas", t.Color.Canvas},
		{"Sunken", t.Color.Sunken},
		{"Surface", t.Color.Surface},
		{"Border", t.Color.BorderDefault},
		{"Accent", t.Color.Accent},
		{"OnAccent", t.Color.FgOnAccent},
		{"Info", t.Color.Info.Solid},
		{"Success", t.Color.Success.Solid},
		{"Warning", t.Color.Warning.Solid},
		{"Danger", t.Color.Danger.Solid},
	}

	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Stack).Padding(t.Card.Padding), materialCard).Children(func() {
		heading(c, tr(c, "preview.ds.section.audit"), 2)
		cols := 3
		if compactLayout(c) {
			cols = 1
		}
		ui.Grid(c).Columns(cols).Gap(t.Density.Row).Children(func() {
			for _, chk := range checks {
				ratio := contrastRatio(chk.fg, chk.bg)
				kind := stateSuccess
				if ratio < 4.5 {
					kind = stateWarning
				}
				applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
					Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
					ui.Text(c, tr(c, chk.labelKey)).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Grow(1)
					badge(c, fmt.Sprintf("%.2f:1 AA", ratio), kind, badgeOptions{Size: sizeSM})
				})
			}
		})

		swatchCols := 5
		if compactLayout(c) {
			swatchCols = 2
		}
		swatchInnerR := concentricRadius(t.Radius.Inset, t.Density.Row)
		ui.Grid(c).Columns(swatchCols).Gap(t.Density.Row).Children(func() {
			for _, sw := range swatches {
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Row), materialInset).Children(func() {
					ui.Box(c).FillWidth().Height(24).Radius(swatchInnerR).Background(sw.col).Border(1, rim)
					ui.Text(c, sw.name).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).SingleLine()
					ui.Text(c, colorHex(sw.col)).FontSize(t.Type.ShellDetail).TextColor(t.Color.FgSecondary).FontFeatures("tnum").SingleLine()
				})
			}
		})
	})
}
