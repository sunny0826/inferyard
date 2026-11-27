package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

const (
	libraryTabModels      = 0
	libraryTabEngines     = 1
	libraryTabRecommended = 2
)

func (s *viewState) libraryView(c *ui.Context) {
	t := tokens(c)
	host := ui.Column(c.Key("library-page-host")).FillWidth().Gap(t.Density.Section)
	pending := ui.Local(host, "pending-delete", func() pendingAssetDelete { return pendingAssetDelete{} })
	wizardOpen := ui.Local(host, "local-engine-wizard-open", func() bool { return false })
	if s.wizardOpenRequested {
		*wizardOpen = true
		s.libraryTab = 1
		s.wizardOpenRequested = false
	}

	host.Children(func() {
		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			s.header(c, tr(c, "library.page.title"))
			ui.Box(c).Grow(1)
			refreshIconButton(c, tr(c, "models.action.rescan"), s.registry.Refreshing).
				Disabled(s.busy || s.registry.Refreshing).OnClick(s.refreshAssetsAction)
			outlineIconButton(c, reicon.Settings, tr(c, "downloads.page.settings")).OnClick(func() { s.openGeneralSettings() })
		})

		s.downloadTaskGuide(c)
		s.libraryHardwareCard(c)

		if len(s.downloads) > 0 {
			card(c, tr(c, "downloads.section.tasks"), func() { s.downloadsView(c) })
		}

		s.libraryTabBar(c)

		switch s.libraryTab {
		case libraryTabRecommended:
			s.renderStarterRecommendationPanel(c, "library")
		case libraryTabEngines:
			s.libraryEnginesTab(c, pending, wizardOpen)
		default:
			s.libraryModelsTab(c, pending)
		}
	})

	s.standaloneDrawer(c)
	s.standaloneStopDialog(c)
	s.assetDeleteDialog(c, pending)
	s.downloadsEngineDrawer(c)
	s.downloadsModelDrawer(c)
}

func (s *viewState) libraryHardwareCard(c *ui.Context) {
	t := tokens(c)
	assets := s.availableTaskAssets()
	missingAssets := !assets.model || !assets.engine
	snapshot, parseError := parseDeviceSnapshot(s.device.Snapshot)
	summary := s.hardwareCompactSummary(c, snapshot, parseError)

	outerR := t.Radius.LG
	if s.libraryHwExpanded {
		outerR = t.Radius.Surface
	}
	applyMaterial(c, ui.Column(c.Key("library-hw-card")).FillWidth().Padding(t.Card.Padding).Gap(t.Density.Row), materialCard).Radius(outerR).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			studioIcon(c, reicon.Cpu, stateColor(c, stateActive))
			ui.Text(c, summary).Bold().FontSize(t.Type.Body).SingleLine()
			if missingAssets {
				inlineBadge(c, tr(c, "library.hw.missing"), stateWarning, badgeOptions{Size: sizeSM})
			} else {
				inlineBadge(c, tr(c, "library.hw.ready"), stateSuccess, badgeOptions{Size: sizeSM})
			}
			ui.Box(c).Grow(1)
			btnLabel := tr(c, "library.hw.expand")
			btnIcon := reicon.ChevronDown
			if s.libraryHwExpanded {
				btnLabel = tr(c, "library.hw.collapse")
				btnIcon = reicon.ChevronUp
			}
			if iconButton(c, btnIcon, btnLabel, components.IconButtonCompact).Clicked() {
				s.libraryHwExpanded = !s.libraryHwExpanded
			}
		})

		if s.libraryHwExpanded {
			s.libraryHardwareDetails(c, snapshot, parseError)
		}
	})
}

func (s *viewState) hardwareCompactSummary(c *ui.Context, snapshot deviceObject, parseError error) string {
	if snapshot == nil || parseError != nil {
		return tr(c, "library.hw.title")
	}
	hw := snapshot.object("hardware")
	missing := tr(c, "device.missing")
	var parts []string
	if cpu := deviceMeasurementFor(c, hw, "cpu_model", false); cpu != "" && cpu != missing {
		parts = append(parts, cpu)
	}
	if mem := deviceBytesFor(c, hw["memory_total_bytes"]); mem != "" && mem != missing {
		parts = append(parts, mem)
	}
	if len(parts) == 0 {
		return tr(c, "library.hw.title")
	}
	return strings.Join(parts, " · ")
}

func (s *viewState) libraryHardwareDetails(c *ui.Context, snapshot deviceObject, parseError error) {
	t := tokens(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		s.libraryDeviceRefreshButton(c, tr(c, "device.action.refresh"))
	})
	if snapshot != nil && parseError == nil {
		deviceHardware(c, snapshot.object("hardware"))
	}
	deviceErr := s.device.LastError
	if s.suppressOutdatedCLIError(deviceErr) {
		deviceErr = nil
	}
	errorRecoveryBanner(c, deviceErr, func() {
		s.libraryDeviceRefreshButton(c, tr(c, "device.action.retry"))
		if button(c, tr(c, "device.action.open_settings")).Clicked() {
			s.openGeneralSettings()
		}
	})
	if parseError != nil {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Message: tr(c, "device.error.unreadable_snapshot"),
			Actions: func() {
				s.libraryDeviceRefreshButton(c, tr(c, "device.action.recollect"))
				button(c, tr(c, "device.action.check_settings")).OnClick(func() { s.openGeneralSettings() })
			},
		})
		return
	}
	if snapshot == nil && s.device.LastError == nil && !s.device.Refreshing {
		ui.Text(c, tr(c, "library.hw.summary_uncollected")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	}
}

func (s *viewState) libraryDeviceRefreshButton(c *ui.Context, label string) {
	refreshIconButton(c, label, s.device.Refreshing || s.actionBusy("refresh-device")).Disabled(s.deviceCollectionBlocked()).OnClick(s.refreshDevice)
}

func (s *viewState) libraryTabBar(c *ui.Context) {
	t := tokens(c)
	recCount := len(starterOptions(s.starterRecommendation()))
	tabs := []struct {
		idx   int
		label string
		count int
		icon  func() *ui.SVG
	}{
		{libraryTabRecommended, tr(c, "library.tab.recommended"), recCount, reicon.Star},
		{libraryTabModels, tr(c, "library.tab.models"), len(s.registry.Models), reicon.Box},
		{libraryTabEngines, tr(c, "library.tab.engines"), len(s.registry.Engines), reicon.Cpu},
	}

	tabPos := 1
	for i, tab := range tabs {
		if tab.idx == s.libraryTab {
			tabPos = i
			break
		}
	}

	ui.Column(c.Key("library-tabbar")).FillWidth().Children(func() {
		bar := ui.TabsBase(c, &tabPos, len(tabs))
		bar.List.FillWidth().AlignItems(ui.End).Gap(t.Density.Section).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Color.BorderDefault).Children(func() {
			for i, tab := range tabs {
				active := s.libraryTab == tab.idx
				item := bar.Tab(i).Column().AlignItems(ui.Center).Label(tab.label)
				if active && item.Clicked() {
					s.pageScroll().ScrollTo(0, 0, ui.Animation{})
				}
				fg := t.Color.FgSecondary
				if active {
					fg = t.Color.FgPrimary
				} else if item.Hovered() {
					fg = t.Color.FgPrimary.Alpha(.82)
				}
				item.Children(func() {
					row := ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).
						Padding(t.Density.Row, t.Density.Tight, t.Density.Row, t.Density.Tight).
						Radius(t.Radius.Control)
					if !active && item.Hovered() {
						row.Background(t.Color.SurfaceHover)
					}
					row.Children(func() {
						studioIcon(c, tab.icon, fg)
						txt := ui.Text(c, tab.label).FontSize(t.Type.Body).TextColor(fg).SingleLine()
						if active {
							txt.Bold()
						}
						cntBg := t.Color.Sunken
						cntFg := t.Color.FgSecondary
						if active {
							cntBg = t.Color.AccentSubtleBg
							cntFg = t.Color.FgPrimary
						}
						ui.Box(c).AlignSelf(ui.Center).Padding(1, 7).Radius(t.Radius.Full).Background(cntBg).Children(func() {
							ui.Text(c, fmt.Sprintf("%d", tab.count)).FontSize(11).FontFeatures("tnum").TextColor(cntFg).SingleLine()
						})
					})
					barColor := ui.RGBA(0, 0, 0, 0)
					if active {
						barColor = t.Color.Accent
					}
					ui.Box(c).FillWidth().Height(2).Radius(t.Radius.Full).Background(barColor)
				})
			}
			ui.Box(c).Grow(1)
		})
		if tabPos >= 0 && tabPos < len(tabs) && tabs[tabPos].idx != s.libraryTab {
			s.libraryTab = tabs[tabPos].idx
		}
	})
}

func (s *viewState) libraryModelsTab(c *ui.Context, pending *pendingAssetDelete) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("library-installed-models")).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, tr(c, "models.section.installed_models"), 2)
			ui.Box(c).Grow(1)
			outlineIconButton(c, reicon.FolderPlus, tr(c, "models.action.add_model_dir")).Disabled(s.busy || !s.canBrowse()).OnClick(func() {
				s.pickExtraModelsRoot(emptyText(s.settings.Effective.ModelsRoot))
			})
		})
		modelErr := s.registry.ModelError
		if s.suppressOutdatedCLIError(modelErr) {
			modelErr = nil
		}
		errorView(c, modelErr)
		s.modelStorageAndRootsView(c)
		if len(s.registry.Models) == 0 {
			emptyGuideCard(c, emptyGuideOptions{
				Icon:    reicon.Layers,
				Title:   tr(c, "models.empty.no_models"),
				Surface: emptySunken,
			})
			return
		}
		ui.Column(c).FillWidth().Children(func() {
			ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
			for i, model := range s.registry.Models {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				m := model
				c.Key(m.Name)
				s.installedModelRow(c, m, pending)
			}
		})
	})

	s.downloadsModelsTab(c)
}

func (s *viewState) libraryEnginesTab(c *ui.Context, pending *pendingAssetDelete, wizardOpen *bool) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("library-installed-engines")).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		studioTooltipGroup(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
			heading(c, tr(c, "models.section.installed_engines"), 2)
			if len(s.registry.Engines) > 0 {
				ui.Text(c, s.engineFit()).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			}
			ui.Box(c).Grow(1)
			btnLabel := tr(c, "models.action.register_local_engine")
			btnIcon := reicon.Link
			if *wizardOpen {
				btnLabel = tr(c, "models.action.collapse_local_engine")
				btnIcon = reicon.ChevronUp
			}
			outlineIconButton(c, btnIcon, btnLabel).OnClick(func() { *wizardOpen = !*wizardOpen })
			s.renderEngineUpdateCheckButton(c)
		})
		if *wizardOpen {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialInset).Children(func() {
				heading(c, tr(c, "models.wizard.card_title"), 3)
				s.localEngineWizardView(c)
				outlineIconButton(c, reicon.ChevronUp, tr(c, "models.action.collapse")).OnClick(func() { *wizardOpen = false })
			})
		}
		if len(s.registry.Engines) == 0 {
			emptyGuideCard(c, emptyGuideOptions{
				Icon:    reicon.Cpu,
				Title:   tr(c, "models.empty.no_engines"),
				Surface: emptySunken,
				Action: func() {
					button(c, tr(c, "models.action.configure_local_engine")).OnClick(func() { *wizardOpen = true })
				},
			})
			return
		}
		ui.Column(c).FillWidth().Children(func() {
			ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
			for i, engine := range s.registry.Engines {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				eng := engine
				c.Key(eng.Name)
				s.installedEngineRow(c, eng, pending)
			}
		})
	})

	s.downloadsEnginesTab(c)
}
