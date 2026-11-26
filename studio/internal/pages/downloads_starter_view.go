package pages

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsStarterCard(c *ui.Context) {
	s.downloadsStarterCardInContext(c, "downloads")
}

func (s *viewState) downloadsStarterCardInContext(c *ui.Context, contextMode string) {
	rec := s.starterRecommendation()
	if !s.hasStarterContent(rec) {
		return
	}
	if !s.starterPanelExpanded() {
		s.downloadsCompactStarterBar(c, rec)
		return
	}
	s.renderStarterRecommendationPanel(c, contextMode)
}

func (s *viewState) downloadsCompactStarterBar(c *ui.Context, rec core.StarterRecommendation) {
	t := tokens(c)
	opts := starterOptions(rec)
	hwSummary := s.starterHardwareSummary(rec)

	applyMaterial(c, ui.Row(c.Key("downloads-starter-compact-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Stack, t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			if rec.Status == "blocked" && len(opts) == 0 {
				badge(c, tr(c, "downloads.starter.badge.hw_unavailable"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				reason := deviceLabel(rec.Reason)
				if reason == "" {
					reason = tr(c, "downloads.starter.blocked_default")
				}
				ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			} else if len(opts) > 0 {
				top := opts[0]
				badge(c, tr(c, "downloads.starter.badge.hw_recommended"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, starterEngineLabel(top.Engine)+" · "+starterOptionTitle(top)).Bold().SingleLine()
				meta := []string{}
				if hwSummary != "" {
					meta = append(meta, hwSummary)
				}
				if len(opts) > 1 {
					meta = append(meta, trf(c, "downloads.starter.options_total", "count", strconv.Itoa(len(opts))))
				}
				if len(meta) > 0 {
					ui.Text(c, joinParts(meta)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
			}
		})
		outlineButton(c, tr(c, "downloads.starter.expand")).OnClick(func() {
			s.setStarterPanelExpanded(true)
		})
	})
}

func (s *viewState) renderStarterRecommendationPanel(c *ui.Context, contextMode string) {
	st := s.ensureDownloadsUI()
	rec := s.starterRecommendation()
	t := tokens(c)

	if s.device.Refreshing && !rec.FromSnapshot {
		applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				heading(c, tr(c, "downloads.starter.heading"), 3)
				badge(c, tr(c, "downloads.starter.badge.detecting"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Text(c, tr(c, "downloads.starter.detecting_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		return
	}

	opts := starterOptions(rec)
	if len(opts) == 0 && rec.Status != "blocked" {
		return
	}
	if st.starterChoice < 0 || st.starterChoice >= len(opts) {
		st.starterChoice = 0
	}

	hwSummary := s.starterHardwareSummary(rec)
	showCollapse := (contextMode == "downloads" || contextMode == "library") && s.starterDefaultCollapsed()

	applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		if rec.Status == "blocked" && len(opts) == 0 {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						heading(c, tr(c, "downloads.starter.heading"), 3)
						badge(c, tr(c, "downloads.starter.badge.unavailable"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
					})
					if hwSummary != "" {
						ui.Text(c, trf(c, "downloads.starter.hw_line", "summary", hwSummary)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
					reason := deviceLabel(rec.Reason)
					if reason == "" {
						reason = tr(c, "downloads.starter.blocked_default")
					}
					ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if notes := localizedDeviceNotes(rec.Notes); len(notes) > 0 {
						ui.Text(c, strings.Join(notes, tr(c, "downloads.punctuation.semicolon"))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
				})
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					outlineButton(c, tr(c, "downloads.starter.redetect")).Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
					button(c, tr(c, "downloads.starter.view_device")).OnClick(func() {
						s.libraryHwExpanded = true
						s.selected = "library"
					})
					if showCollapse {
						ghostButton(c, tr(c, "downloads.starter.collapse")).OnClick(func() { s.setStarterPanelExpanded(false) })
					}
				})
			})
			return
		}

		// Header row: title + option count + refresh / collapse controls
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				heading(c, tr(c, "downloads.starter.heading"), 3)
				badge(c, trf(c, "downloads.starter.options_count_badge", "count", strconv.Itoa(len(opts))), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ghostButton(c, tr(c, "downloads.starter.redetect")).Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
				if showCollapse {
					outlineButton(c, tr(c, "downloads.starter.collapse")).OnClick(func() { s.setStarterPanelExpanded(false) })
				}
			})
		})

		// Hardware summary strip (omitted inside libraryView where libraryHardwareCard already sits directly above)
		if hwSummary != "" && contextMode != "library" {
			applyMaterial(c, ui.Row(c.Key("starter-hardware-strip")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Inset, t.Density.Stack), materialSunken).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					components.Icon(c, lucide.Cpu).Size(14, 14).TextColor(t.Color.FgSecondary)
					ui.Text(c, trf(c, "downloads.starter.hw_line", "summary", hwSummary)).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).FontFeatures("tnum")
				})
				ghostButton(c, tr(c, "downloads.starter.device_details")).OnClick(func() {
					s.libraryHwExpanded = true
					s.selected = "library"
				})
			})
		}

		// Fallback reason notice (e.g. GPU unsupported -> switched to llama.cpp)
		if rec.Reason != "" && rec.Reason != "polystrata_hardware_fit" {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateInfo,
				Size:    bannerCompact,
				Message: deviceLabel(rec.Reason),
			})
		}

		// Balanced responsive grid (2x2 for 4 plans instead of 3 + 1 full-width)
		cols := min(3, max(1, len(opts)))
		if len(opts) == 4 {
			cols = 2
		}
		if compactLayout(c) {
			cols = min(2, cols)
		}
		if w, _ := c.Size(); w > 0 && w < 680 {
			cols = 1
		}
		ui.Grid(c.Key("starter-options-grid")).FillWidth().Columns(cols).Gap(t.Density.Row).Children(func() {
			for i, opt := range opts {
				idx, option := i, opt
				s.starterOptionTile(c, st, idx, option, idx == st.starterChoice)
			}
		})

		chosen := opts[st.starterChoice]

		// Inline macOS Xcode CLT notice when PolyStrata is selected
		if chosen.Engine == "polystrata" && slices.Contains(rec.Notes, "xcode_clt_missing") {
			applyMaterial(c, ui.Row(c.Key("starter-xcode-clt-hint")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
				badge(c, tr(c, "downloads.starter.xcode_badge"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, tr(c, "downloads.starter.xcode_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				ui.Text(c, "xcode-select --install").Selectable().FontSize(t.Type.Caption)
				outlineButton(c, tr(c, "downloads.action.copy_cmd")).Label(tr(c, "downloads.starter.copy_xcode_cmd_label")).OnClick(func() {
					s.copyDownloadText(c, tr(c, "downloads.copy_subject.install_cmd"), "xcode-select --install")
				})
			})
		}

		// Other hardware/environment notes
		if notes := starterCardNotes(rec.Notes, chosen.Engine == "polystrata"); len(notes) > 0 {
			ui.Text(c, trf(c, "downloads.starter.notes_line", "notes", strings.Join(notes, tr(c, "downloads.punctuation.semicolon")))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}

		// Unified bottom bar: model source selector + primary CTA
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c.Key("starter-action-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c.Key("starter-model-source-row")).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, tr(c, "downloads.starter.model_source")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				if chosen.Engine == "polystrata" {
					ui.Box(c.Key("starter-source-polystrata")).Children(func() {
						prev := st.polystrataModelSource
						segmentedControl(c, tr(c, "downloads.starter.polystrata_source_label"), &st.polystrataModelSource, []components.NativeSelectOption[string]{
							{Value: "auto", Label: tr(c, "downloads.source.auto")},
							{Value: "modelscope", Label: "ModelScope"},
							{Value: "huggingface", Label: "HuggingFace"},
						})
						if st.polystrataModelSource != prev {
							st.setPolystrataModelSource(st.polystrataModelSource)
						}
					})
				} else {
					ui.Box(c.Key("starter-source-direct")).Children(func() {
						prev := st.modelSource
						segmentedControl(c, tr(c, "downloads.starter.direct_source_label"), &st.modelSource, []components.NativeSelectOption[string]{
							{Value: "huggingface", Label: "HuggingFace"},
							{Value: "modelscope", Label: "ModelScope"},
						})
						if st.modelSource != prev {
							st.setDirectModelSource(st.modelSource)
						}
					})
				}
			})

			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				if chosen.Engine == "polystrata" {
					outlineButton(c, tr(c, "downloads.starter.more_config")).Label(tr(c, "downloads.starter.more_polystrata_config_label")).OnClick(func() {
						st.polystrataDrawerModelID = chosen.ModelID
						if contextMode == "dashboard" {
							s.selected = "library"
						}
						s.openPolystrataDrawerForCurrentHost()
					})
				}
				if contextMode == "dashboard" {
					outlineButton(c, tr(c, "downloads.starter.go_downloads")).OnClick(func() {
						s.selected = "library"
					})
				}
				s.starterPrimaryCTA(c, st, chosen, contextMode == "dashboard")
			})
		})
	})
}

func (s *viewState) starterOptionTile(c *ui.Context, st *downloadsUIState, idx int, opt starterOption, selected bool) {
	t := tokens(c)
	installed := s.isStarterOptionInstalled(opt)

	tile := ui.Column(c.Key("starter-option-" + opt.ModelID)).MinWidth(210).Grow(1).Gap(t.Density.Row).Padding(t.Density.Stack).Radius(t.Radius.Floating).Label(trf(c, "downloads.starter.select_option_label", "id", opt.ModelID))
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
					components.Icon(c, lucide.Check).Size(10, 10).TextColor(t.Color.FgOnAccent)
				})
			} else {
				checkCircle.Border(1.5, t.Color.BorderStrong)
			}
		})

		// Title & Engine/Backend line
		ui.Column(c).FillWidth().Gap(t.Density.Hairline).Children(func() {
			ui.Text(c, starterOptionTitle(opt)).Bold()
			engineMeta := starterEngineLabel(opt.Engine)
			if opt.Backend != "" {
				engineMeta += " · " + deviceLabel(opt.Backend)
			}
			ui.Text(c, engineMeta).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
