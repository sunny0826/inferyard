package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			s.header(c, tr(c, "library.page.title"))
			ui.Box(c).Grow(1)
			refreshButton(c, tr(c, "models.action.rescan"), s.registry.Refreshing).
				Disabled(s.busy || s.registry.Refreshing).OnClick(s.refreshAssetsAction)
			button(c, tr(c, "downloads.page.settings")).OnClick(func() { s.openGeneralSettings() })
		})

		s.downloadTaskGuide(c)
		s.libraryHardwareCard(c)

		if len(s.downloads) > 0 {
			card(c, tr(c, "downloads.section.tasks"), func() { s.downloadsView(c) })
		}

		s.libraryTabBar(c)

		if s.libraryTab == 1 {
			s.libraryEnginesTab(c, pending, wizardOpen)
		} else {
			s.libraryModelsTab(c, pending)
		}
	})

	s.standaloneDrawer(c)
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
			components.Icon(c, lucide.Cpu).Size(16, 16).TextColor(stateColor(c, stateActive))
			ui.Text(c, summary).Bold().FontSize(t.Type.Body).SingleLine()
			if missingAssets {
				inlineBadge(c, tr(c, "library.hw.missing"), stateWarning, badgeOptions{Size: sizeSM})
			} else {
				inlineBadge(c, tr(c, "library.hw.ready"), stateSuccess, badgeOptions{Size: sizeSM})
			}
			ui.Box(c).Grow(1)
			btnLabel := tr(c, "library.hw.expand")
			if s.libraryHwExpanded {
				btnLabel = tr(c, "library.hw.collapse")
			}
			if inlineButton(c, components.ButtonGhost, btnLabel).Clicked() {
				s.libraryHwExpanded = !s.libraryHwExpanded
			}
		})

		if s.libraryHwExpanded {
			s.libraryHardwareDetails(c, snapshot, parseError)
		}
	})

	if missingAssets || s.starterCardVisible() {
		s.downloadsStarterCard(c)
	}
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
		s.deviceRefreshButton(c, tr(c, "device.action.refresh"))
	})
	if snapshot != nil && parseError == nil {
		deviceHardware(c, snapshot.object("hardware"))
	}
	deviceErr := s.device.LastError
	if s.suppressOutdatedCLIError(deviceErr) {
		deviceErr = nil
	}
	errorRecoveryBanner(c, deviceErr, func() {
		s.deviceRefreshButton(c, tr(c, "device.action.retry"))
		if button(c, tr(c, "device.action.open_settings")).Clicked() {
			s.openGeneralSettings()
		}
	})
	if parseError != nil {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Message: tr(c, "device.error.unreadable_snapshot"),
			Actions: func() {
				s.deviceRefreshButton(c, tr(c, "device.action.recollect"))
				button(c, tr(c, "device.action.check_settings")).OnClick(func() { s.openGeneralSettings() })
			},
		})
		return
	}
	if snapshot == nil && s.device.LastError == nil && !s.device.Refreshing {
		ui.Text(c, tr(c, "library.hw.summary_uncollected")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	}
}

func (s *viewState) libraryTabBar(c *ui.Context) {
	t := tokens(c)
	tabs := []struct {
		idx   int
		label string
		count int
		icon  func() *ui.SVG
	}{
		{0, tr(c, "library.tab.models"), len(s.registry.Models), lucide.Box},
		{1, tr(c, "library.tab.engines"), len(s.registry.Engines), lucide.Cpu},
	}

	ui.Column(c.Key("library-tabbar")).FillWidth().Children(func() {
		bar := ui.TabsBase(c, &s.libraryTab, len(tabs))
		bar.List.FillWidth().AlignItems(ui.End).Gap(t.Density.Section).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Color.BorderDefault).Children(func() {
			for i, tab := range tabs {
				active := s.libraryTab == tab.idx
				item := bar.Tab(i).Column().AlignItems(ui.Center).Label(tab.label)
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
						components.Icon(c, tab.icon).Size(15, 15).TextColor(fg)
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
	})
}

func (s *viewState) libraryModelsTab(c *ui.Context, pending *pendingAssetDelete) {
	t := tokens(c)
	card(c, tr(c, "models.section.installed_models"), func() {
		modelErr := s.registry.ModelError
		if s.suppressOutdatedCLIError(modelErr) {
			modelErr = nil
		}
		errorView(c, modelErr)
		s.modelStorageAndRootsView(c)
		if len(s.registry.Models) == 0 {
			emptyGuideCard(c, emptyGuideOptions{
				Icon:    lucide.Layers,
				Title:   tr(c, "models.empty.no_models"),
				Surface: emptySunken,
			})
			return
		}
		applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Tight), materialInset).Children(func() {
			for _, model := range s.registry.Models {
				m := model
				c.Key(m.Name)
				listRow(c, listRowOptions{
					Leading:  func() { modelIconBox(c, modelLabel(m), 28, t.Radius.SM) },
					Title:    modelLabel(m),
					Subtitle: modelDetails(m),
					Trailing: func() { s.modelRowTrailing(c, m, pending) },
				})
			}
		})
	})

	section(c, tr(c, "library.section.catalog_models"), func() {
		s.downloadsModelsTab(c)
	})
}

func (s *viewState) libraryEnginesTab(c *ui.Context, pending *pendingAssetDelete, wizardOpen *bool) {
	t := tokens(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		btnLabel := tr(c, "models.action.register_local_engine")
		if *wizardOpen {
			btnLabel = tr(c, "models.action.collapse_local_engine")
		}
		button(c, btnLabel).OnClick(func() { *wizardOpen = !*wizardOpen })
		s.renderEngineUpdateCheckButton(c)
	})
	if *wizardOpen {
		card(c, tr(c, "models.wizard.card_title"), func() {
			s.localEngineWizardView(c)
			button(c, tr(c, "models.action.collapse")).OnClick(func() { *wizardOpen = false })
		})
	}

	card(c, tr(c, "models.section.installed_engines"), func() {
		if len(s.registry.Engines) == 0 {
			emptyGuideCard(c, emptyGuideOptions{
				Icon:    lucide.Cpu,
				Title:   tr(c, "models.empty.no_engines"),
				Surface: emptySunken,
				Action: func() {
					button(c, tr(c, "models.action.configure_local_engine")).OnClick(func() { *wizardOpen = true })
				},
			})
			return
		}
		ui.Text(c, s.engineFit()).TextColor(t.Color.FgSecondary)
		applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Tight), materialInset).Children(func() {
			for _, engine := range s.registry.Engines {
				eng := engine
				c.Key(eng.Name)
				listRow(c, listRowOptions{
					Leading:  func() { engineIconBox(c, engineFamilyTitle(eng.Family, eng.Name), 28, t.Radius.SM) },
					Title:    eng.Name,
					Subtitle: s.engineStateLine(eng),
					Trailing: func() { s.engineRowTrailing(c, eng, pending) },
				})
			}
		})
	})

	section(c, tr(c, "library.section.catalog_engines"), func() {
		s.downloadsEnginesTab(c)
	})
}
