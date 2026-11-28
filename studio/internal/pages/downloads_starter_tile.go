package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) starterOptionTile(c *ui.Context, st *downloadsUIState, idx int, opt starterOption, selected bool) {
	t := tokens(c)
	installed := s.isStarterOptionInstalled(opt)

	tile := ui.Column(c.Key("starter-option-" + opt.ModelID)).MinWidth(210).Grow(1).Gap(t.Density.Row).Padding(t.Density.Stack).Radius(t.Radius.Floating).Role(ui.RoleButton).Focusable().Label(trf(c, "downloads.starter.select_option_label", "id", opt.ModelID))
	bg := t.Color.Sunken
	border := t.Color.BorderSubtle
	if c.Preferences().HighContrast {
		border = t.Color.BorderDefault
	}
	if selected {
		bg = t.Color.Surface
		if c.Theme().Dark {
			bg = t.Color.Elevated
		}
		border = t.Color.Accent
	} else if tile.Hovered() {
		bg = t.Color.SurfaceHover
		border = t.Color.BorderStrong
	}
	tile.Background(bg).Border(1.5, border)
	if selected {
		applyShadow(tile, t.Elevation.XS)
	}
	tile.OnClick(func() {
		st.starterChoice = idx
		if opt.Engine == "polystrata" {
			st.polystrataDrawerModelID = opt.ModelID
		}
	})

	tile.Children(func() {
		// Top row: tier badge, fit badge, installed badge, selection circle
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				badge(c, starterTierLabel(opt.Tier), starterTierState(opt.Tier), badgeOptions{Style: badgeSoft, Size: sizeSM})
				if opt.FitLevel != "" {
					badge(c, deviceLabel(opt.FitLevel), starterFitState(opt.FitLevel), badgeOptions{Style: badgeOutline, Size: sizeSM})
				}
				if installed {
					badge(c, tr(c, "downloads.badge.installed"), stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			checkCircle := ui.Box(c).Size(16, 16).Radius(t.Radius.Full).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
			if selected {
				checkCircle.Background(t.Color.Accent).Children(func() {
					components.Icon(c, reicon.Check).Size(10, 10).TextColor(t.Color.FgOnAccent)
				})
			} else {
				checkCircle.Border(1.5, t.Color.BorderStrong)
			}
		})

		// Title & Engine/Backend line
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			modelIconBox(c, opt.ModelTitle+" "+opt.Family+" "+opt.ModelID, 32, t.Radius.Control)
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
				ui.Text(c, starterOptionTitle(opt)).Bold()
				engineMeta := starterEngineLabel(opt.Engine)
				if opt.Backend != "" {
					engineMeta += " · " + deviceLabel(opt.Backend)
				}
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					renderBrandEngineIcon(c, opt.Engine, 12, t.Color.FgSecondary)
					ui.Text(c, engineMeta).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				})
			})
		})

		ui.Spacer(c)

		// Metrics row: Download / RAM / Context
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c).FillWidth().Wrap().Gap(t.Density.Stack).Children(func() {
			if opt.EstimatedDownloadBytes > 0 {
				starterTileMetric(c, tr(c, "downloads.starter.metric.download"), formatDownloadBytes(opt.EstimatedDownloadBytes))
			}
			if opt.EstimatedRAMBytes > 0 {
				starterTileMetric(c, tr(c, "downloads.starter.metric.ram"), formatDownloadBytes(opt.EstimatedRAMBytes))
			}
			if opt.RecommendedContext > 0 {
				starterTileMetric(c, tr(c, "downloads.starter.metric.context"), starterContextShort(opt.RecommendedContext))
			}
		})
	})
}

func starterTileMetric(c *ui.Context, label, value string) {
	t := tokens(c)
	ui.Column(c).Gap(t.Density.Hairline).Children(func() {
		ui.Text(c, label).FontSize(t.Type.ShellDetail).TextColor(t.Color.FgTertiary)
		ui.Text(c, value).FontSize(t.Type.Caption).Bold().FontFeatures("tnum").TextColor(t.Color.FgPrimary)
	})
}

func (s *viewState) starterPrimaryCTA(c *ui.Context, st *downloadsUIState, chosen starterOption, navigateToDownloads bool) {
	switch chosen.Engine {
	case "polystrata":
		if s.downloadActive("polystrata") {
			button(c, tr(c, "downloads.action.installing")).Disabled(true)
			return
		}
		if s.isStarterOptionInstalled(chosen) {
			button(c, tr(c, "downloads.starter.cta.ready")).Disabled(true)
			return
		}
		label := tr(c, "downloads.starter.cta.install_polystrata")
		if s.isEngineFamilyInstalled("polystrata") {
			label = tr(c, "downloads.starter.cta.install_polystrata_model")
		}
		if primaryButton(c, label).Disabled(s.busy).Clicked() {
			s.startStarterOption(chosen, navigateToDownloads)
		}
	case "mlx-lm":
		engineInstalled := s.isStarterEngineInstalled(chosen)
		modelInstalled := s.isStarterModelInstalled(chosen)
		if engineInstalled && modelInstalled {
			button(c, tr(c, "downloads.starter.cta.ready")).Disabled(true)
			return
		}
		if fam, _, _, ok := s.starterManagedVenvTarget("mlx-lm"); ok && s.isEngineFamilyDownloading(fam) {
			button(c, tr(c, "downloads.action.installing")).Disabled(true)
			return
		}
		if entry, ok := core.CatalogEntryByID(chosen.ModelID); ok && s.isModelEntryDownloading(entry) {
			button(c, tr(c, "downloads.action.downloading")).Disabled(true)
			return
		}
		label := tr(c, "downloads.starter.cta.download_bundle")
		switch {
		case engineInstalled:
			label = tr(c, "downloads.starter.cta.download_model")
		case modelInstalled:
			label = tr(c, "downloads.starter.cta.download_engine")
		}
		if primaryButton(c, label).Disabled(s.busy).Clicked() {
			s.startStarterOption(chosen, navigateToDownloads)
		}
	default:
		engineInstalled := s.isStarterEngineInstalled(chosen)
		modelInstalled := s.isStarterModelInstalled(chosen)
		if engineInstalled && modelInstalled {
			button(c, tr(c, "downloads.starter.cta.ready")).Disabled(true)
			return
		}
		hostPlatform := s.downloadsHostPlatform()
		engineURL := starterLlamaCppEngineURL(hostPlatform, chosen.Backend)
		modelURL := starterModelURLForOption(chosen, st.modelSource)
		switch {
		case engineInstalled && modelURL != "":
			if primaryButton(c, tr(c, "downloads.starter.cta.download_model")).Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		case modelInstalled && engineURL != "":
			if primaryButton(c, tr(c, "downloads.starter.cta.download_engine")).Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		case !engineInstalled && !modelInstalled && engineURL != "" && modelURL != "":
			if primaryButton(c, tr(c, "downloads.starter.cta.download_bundle")).Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		default:
			t := tokens(c)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				primaryButton(c, tr(c, "downloads.starter.cta.view_engine_install")).OnClick(func() {
					if navigateToDownloads {
						s.selected = "library"
					}
					relID, buildID := starterLlamaCppDrawerTarget(s.engineCatalogRegistry(), hostPlatform, chosen.Backend)
					st.openEngineDrawer("llama-cpp", relID, hostPlatform, buildID)
				})
				if !modelInstalled && modelURL != "" {
					outlineButton(c, tr(c, "downloads.starter.cta.download_model")).Disabled(s.busy).OnClick(func() {
						if navigateToDownloads {
							s.setStarterPanelExpanded(false)
							s.selected = "library"
						}
						s.acquireModelSource(modelURL, "")
					})
				}
			})
		}
	}
}
