package pages

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) downloadsEnginesTab(c *ui.Context) {
	st := s.ensureDownloadsUI()
	t := tokens(c)
	hostPlatform := s.downloadsHostPlatform()
	families := s.engineCatalogRegistry().Families()

	var managed, externalApps, otherPlatform []core.EngineFamilyCatalog
	for _, fam := range families {
		hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
		switch {
		case len(hostReleases) == 0:
			otherPlatform = append(otherPlatform, fam)
		case fam.ExternalApp:
			externalApps = append(externalApps, fam)
		default:
			managed = append(managed, fam)
		}
	}

	ui.Column(c.Key("downloads-engines-tab")).FillWidth().Gap(t.Density.Section).Children(func() {
		applyMaterial(c, ui.Column(c).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Children(func() { heading(c, tr(c, "library.section.catalog_engines"), 2) })
				outlineButton(c, tr(c, "downloads.engines.link_existing")).OnClick(func() { s.openLocalEngineWizard("") })
			})
			if len(managed) == 0 {
				ui.Text(c, tr(c, "downloads.engines.none_available")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				return
			}
			ui.Column(c).FillWidth().Children(func() {
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				for i, fam := range managed {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
					rel := resolveEngineReleaseForInstall(st.selectedRelease[fam.ID], hostReleases, hostPlatform)
					build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)
					s.engineFamilyRow(c, fam, rel, build, hostPlatform)
				}
			})
		})

		if len(externalApps) > 0 {
			card(c, tr(c, "downloads.engines.external_apps"), func() {
				ui.Column(c).FillWidth().Children(func() {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					for i, fam := range externalApps {
						if i > 0 {
							ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
						}
						hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
						rel := resolveEngineRelease(st.selectedRelease[fam.ID], hostReleases)
						build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)
						s.externalEngineAppRow(c, fam, rel, build)
					}
				})
			})
		}

		if len(otherPlatform) > 0 {
			componentFoldKeyed(c, "downloads-other-platform-engines", trf(c, "downloads.engines.other_platform_fold", "count", strconv.Itoa(len(otherPlatform))), func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, fam := range otherPlatform {
						if i > 0 {
							ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
						}
						s.otherPlatformEngineRow(c, fam)
					}
				})
			})
		}

		componentFoldKeyed(c, "downloads-custom-engine-url", tr(c, "downloads.engines.custom_url_fold"), func() {
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				field(c, tr(c, "downloads.engines.custom_url_label"), &st.customEngineURL).
					Error(downloadURLError(st.customEngineURL))
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					trimmed := strings.TrimSpace(st.customEngineURL)
					button(c, tr(c, "downloads.engines.download_verify")).Disabled(s.busy || trimmed == "" || downloadURLError(trimmed) != "").OnClick(func() {
						s.acquireEngine(trimmed, "")
					})
				})
			})
		})
	})
}

func (s *viewState) engineFamilyRow(c *ui.Context, fam core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild, hostPlatform string) {
	st := s.ensureDownloadsUI()
	t := tokens(c)
	installed := s.isEngineFamilyInstalled(fam.ID)
	installing := s.isEngineFamilyDownloading(fam)

	ui.Row(c.Key("downloads-engine-"+fam.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		engineIconBox(c, fam.Name+" "+fam.ID, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, fam.Name).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if installed {
					inlineBadge(c, tr(c, "downloads.badge.installed"), stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if installing {
					inlineBadge(c, tr(c, "downloads.badge.installing"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				metaParts := []string{rel.Distribution, rel.Version, core.PlatformLabel(build.Platform)}
				if build.Variant != "" {
					metaParts = append(metaParts, build.Variant)
				}
				if total := build.TotalBytes(); total > 0 {
					metaParts = append(metaParts, catalogHumanSize(total))
				}
				ui.Text(c, joinParts(metaParts)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
				inlineBadge(c, core.ChannelLabel(rel.Channel), engineChannelState(rel.Channel), badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			if fam.Description != "" {
				ui.Text(c, fam.Description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			outlineIconButton(c, lucide.Info, trf(c, "downloads.engines.versions_urls_label", "id", fam.ID)).Label(trf(c, "downloads.engines.versions_urls_label", "id", fam.ID)).OnClick(func() {
				if fam.ID == "polystrata" {
					s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
					return
				}
				st.openEngineDrawer(fam.ID, rel.ID, build.Platform, build.ID)
			})
			switch {
			case build.AcquireMode == core.EngineAcquireManagedSetup && (build.Platform == hostPlatform || build.Platform == "any"):
				if s.downloadActive("polystrata") {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(true)
				} else {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(s.busy).OnClick(func() {
						s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
					})
				}
			case build.CanManagedVenvInstallOn(hostPlatform):
				if s.isEngineFamilyDownloading(fam) {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(true)
				} else {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(s.busy).OnClick(func() {
						s.acquireManagedEngine(fam, rel, build)
					})
				}
			case build.CanDirectDownloadOn(hostPlatform):
				if s.isEngineSourceDownloading(build.PrimaryURL()) {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(true)
				} else {
					primaryIconButton(c, lucide.Download, trf(c, "downloads.engines.install_label", "id", fam.ID)).Disabled(s.busy).OnClick(func() {
						s.acquireEngine(build.PrimaryURL(), "")
					})
				}
			case build.PrimaryURL() != "":
				outlineIconButton(c, lucide.ExternalLink, trf(c, "downloads.action.open_download_page_label", "id", fam.ID)).OnClick(func() {
					s.openDownloadURL(c, fam.Name, build.PrimaryURL())
				})
			}
		})
	})
}

func (s *viewState) externalEngineAppRow(c *ui.Context, fam core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild) {
	t := tokens(c)
	installed := s.isEngineFamilyInstalled(fam.ID)

	ui.Row(c.Key("downloads-ext-"+fam.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		engineIconBox(c, fam.Name+" "+fam.ID, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, fam.Name).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if installed {
					inlineBadge(c, tr(c, "downloads.badge.linked"), stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			metaParts := []string{rel.Distribution, rel.Version, core.PlatformLabel(build.Platform)}
			if build.Variant != "" {
				metaParts = append(metaParts, build.Variant)
			}
			ui.Text(c, joinParts(metaParts)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			if fam.Description != "" {
				ui.Text(c, fam.Description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			outlineIconButton(c, lucide.ExternalLink, trf(c, "downloads.engines.open_website_label", "id", fam.ID)).OnClick(func() {
				s.openDownloadURL(c, fam.Name, build.PrimaryURL())
			})
			outlineIconButton(c, lucide.Link, trf(c, "downloads.engines.link_label", "id", fam.ID)).OnClick(func() {
				s.openLocalEngineWizard(fam.ID)
			})
		})
	})
}

func (s *viewState) otherPlatformEngineRow(c *ui.Context, fam core.EngineFamilyCatalog) {
	st := s.ensureDownloadsUI()
	t := tokens(c)
	rel := resolveEngineRelease(st.selectedRelease[fam.ID], fam.Releases)
	platforms := rel.Platforms()
	firstPlatform := "any"
	if len(platforms) > 0 {
		firstPlatform = platforms[0]
	}
	builds := rel.BuildsForPlatform(firstPlatform)
	firstBuild := rel.Builds[0]
	if len(builds) > 0 {
		firstBuild = builds[0]
	}

	ui.Row(c.Key("downloads-other-engine-"+fam.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		engineIconBox(c, fam.Name+" "+fam.ID, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, fam.Name).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			})
			ui.Text(c, trf(c, "downloads.platform.supported_line", "platforms", enginePlatformSummary(rel))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			if fam.Description != "" {
				ui.Text(c, fam.Description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
			outlineIconButton(c, lucide.Info, trf(c, "downloads.engines.versions_urls_label", "id", fam.ID)).Label(trf(c, "downloads.engines.versions_urls_label", "id", fam.ID)).OnClick(func() {
				st.openEngineDrawer(fam.ID, rel.ID, firstPlatform, firstBuild.ID)
			})
		})
	})
}

func (s *viewState) isEngineFamilyInstalled(familyID string) bool {
	for _, eng := range s.registry.Engines {
		if eng.Family == familyID {
			return true
		}
		if familyID == "llama-cpp" && eng.Family == "" && strings.Contains(strings.ToLower(eng.Name), "llama") {
			return true
		}
	}
	return false
}

func (s *viewState) isEngineSourceDownloading(sourceURL string) bool {
	if sourceURL == "" {
		return false
	}
	for _, task := range s.downloads {
		if task.Kind == "engine" && task.State == "downloading" && task.SourceUrl == sourceURL && s.downloadTaskInCurrentWorkspace(task) {
			return true
		}
	}
	return false
}

func (s *viewState) isEngineFamilyDownloading(fam core.EngineFamilyCatalog) bool {
	for _, task := range s.downloads {
		if task.State != "downloading" {
			continue
		}
		if fam.ID == "polystrata" && task.Kind == "polystrata" {
			return true
		}
		if task.Kind != "engine" && task.Kind != "engine_install" || !s.downloadTaskInCurrentWorkspace(task) {
			continue
		}
		if task.Kind == "engine_install" && strings.HasPrefix(task.RequestedSourceUrl, fam.ID+":") {
			return true
		}
		for _, rel := range fam.Releases {
			for _, build := range rel.Builds {
				if build.PrimaryURL() != "" && task.SourceUrl == build.PrimaryURL() {
					return true
				}
			}
		}
		if fam.ID == "llama-cpp" && (task.DisplayName == "llama-server" || strings.Contains(strings.ToLower(task.SourceUrl), "llama")) {
			return true
		}
	}
	return false
}

func resolveEngineReleaseForInstall(wantID string, releases []core.EngineRelease, hostPlatform string) core.EngineRelease {
	for _, release := range releases {
		if release.ID == wantID {
			return release
		}
	}
	for _, release := range releases {
		for _, build := range release.Builds {
			if build.CanDirectDownloadOn(hostPlatform) || build.CanManagedVenvInstallOn(hostPlatform) || build.AcquireMode == core.EngineAcquireManagedSetup {
				return release
			}
		}
	}
	return resolveEngineRelease(wantID, releases)
}

func enginePlatformSummary(rel core.EngineRelease) string {
	platforms := rel.Platforms()
	labels := make([]string, 0, len(platforms))
	for _, plat := range platforms {
		labels = append(labels, core.PlatformLabel(plat))
	}
	return strings.Join(labels, l10n.TR("downloads.list.separator"))
}
