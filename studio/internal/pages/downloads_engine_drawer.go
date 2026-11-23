package pages

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsEngineDrawer(c *ui.Context) {
	st := s.ensureDownloadsUI()
	c.Key("downloads-engine-detail-drawer")
	if !st.engineDrawerOpen {
		drawer(c, &st.engineDrawerOpen, drawerOptions{Label: "引擎详情", Title: "引擎详情"}, nil)
		return
	}
	family, ok := s.engineCatalogRegistry().FamilyByID(st.engineDrawerFamilyID)
	if !ok || len(family.Releases) == 0 {
		st.engineDrawerOpen = false
		drawer(c, &st.engineDrawerOpen, drawerOptions{Label: "引擎详情", Title: "引擎详情"}, nil)
		return
	}

	hostPlatform := s.downloadsHostPlatform()
	hasStable, hasNonStable := engineChannelMix(family.Releases)
	releases := filterEngineReleases(family.Releases, st.drawerStableOnly && hasStable)
	rel := resolveEngineRelease(st.selectedRelease[family.ID], releases)
	platforms := rel.Platforms()
	activePlatform := resolveEnginePlatform(st.selectedPlatform[family.ID], hostPlatform, platforms)
	builds := rel.BuildsForPlatform(activePlatform)
	build := pickEngineBuildForID(st.selectedBuild[family.ID], builds)

	drawer(c, &st.engineDrawerOpen, drawerOptions{
		Label: "引擎详情",
		Title: family.Name,
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("downloads-engine-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				s.engineDrawerConfigSection(c, st, family, releases, rel, platforms, activePlatform, builds, build, hostPlatform, hasStable && hasNonStable)
				s.engineDrawerArtifactsSection(c, build)
				s.engineDrawerInstallSection(c, st, family, rel, build, hostPlatform)
			})
		})
	})
}

func (s *viewState) engineDrawerConfigSection(
	c *ui.Context,
	st *downloadsUIState,
	family core.EngineFamilyCatalog,
	releases []core.EngineRelease,
	rel core.EngineRelease,
	platforms []string,
	activePlatform string,
	builds []core.EnginePlatformBuild,
	build core.EnginePlatformBuild,
	hostPlatform string,
	showStableToggle bool,
) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
		if showStableToggle {
			prev := st.drawerStableOnly
			components.Checkbox(c, &st.drawerStableOnly, "仅正式版")
			if st.drawerStableOnly != prev {
				nextReleases := filterEngineReleases(family.Releases, st.drawerStableOnly)
				nextRel := resolveEngineRelease(st.selectedRelease[family.ID], nextReleases)
				if nextRel.ID != st.selectedRelease[family.ID] {
					st.selectedRelease[family.ID] = nextRel.ID
					st.selectedBuild[family.ID] = ""
				}
			}
		}
		if len(releases) > 1 {
			ui.Box(c.Key("downloads-drawer-ver-" + family.ID)).Children(func() {
				chosenID := rel.ID
				sel := components.Select(c, &chosenID)
				sel.Trigger.Label("切换版本").Value(rel.Version + " · " + rel.Distribution).Children(func() {
					ui.Text(c, rel.Version+" · "+rel.Distribution).SingleLine()
				})
				sel.Popup(func(ui.Element) {
					components.ScrollView(c, components.ScrollViewOptions{}).Label("版本列表").MaxHeight(220).Children(func() {
						for _, candidate := range releases {
							itemLabel := engineReleaseOptionLabel(candidate)
							sel.Item(candidate.ID).Label(itemLabel).Children(func() {
								ui.Text(c, itemLabel).SingleLine()
							})
						}
					})
				})
				if sel.Trigger.Changed() {
					st.selectedRelease[family.ID] = chosenID
					st.selectedBuild[family.ID] = ""
				}
			})
		}
		if len(platforms) > 1 {
			ui.Box(c.Key("downloads-drawer-plat-" + family.ID)).Children(func() {
				chosenPlat := activePlatform
				sel := components.Select(c, &chosenPlat)
				sel.Trigger.Label("选择平台").Value(core.PlatformLabel(activePlatform)).Children(func() {
					ui.Text(c, core.PlatformLabel(activePlatform)).SingleLine()
				})
				sel.Popup(func(ui.Element) {
					components.ScrollView(c, components.ScrollViewOptions{}).Label("平台列表").MaxHeight(220).Children(func() {
						for _, plat := range platforms {
							platLabel := core.PlatformLabel(plat)
							sel.Item(plat).Label(platLabel).Children(func() {
								ui.Text(c, platLabel).SingleLine()
							})
						}
					})
				})
				if sel.Trigger.Changed() {
					st.selectedPlatform[family.ID] = chosenPlat
					st.selectedBuild[family.ID] = ""
				}
			})
		}
		if len(builds) > 1 {
			opts := make([]components.NativeSelectOption[string], 0, len(builds))
			for _, candidate := range builds {
				label := candidate.Variant
				if label == "" {
					label = candidate.ID
				}
				opts = append(opts, components.NativeSelectOption[string]{Value: candidate.ID, Label: label})
			}
			chosenBuild := build.ID
			segmentedControl(c, "选择变体", &chosenBuild, opts)
			if chosenBuild != build.ID {
				st.selectedBuild[family.ID] = chosenBuild
			}
		}

		keyValueRow(c, "版本状态", core.ChannelLabel(rel.Channel), false)
		keyValueRow(c, "发行来源", rel.Distribution, false)
		keyValueRow(c, "目标平台", core.PlatformLabel(build.Platform), false)
		if build.Variant != "" {
			keyValueRow(c, "构建变体", build.Variant, false)
		}
		keyValueRow(c, "获取方式", core.AcquireModeLabel(build.AcquireMode), false)
		if total := build.TotalBytes(); total > 0 {
			keyValueRow(c, "总大小", catalogHumanSize(total), true)
		}
		if build.Platform != "any" && build.Platform != hostPlatform {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: "当前所选平台与本机（" + core.PlatformLabel(hostPlatform) + "）不同，无法在本机直接运行。",
			})
		}
	})
}

func (s *viewState) engineDrawerArtifactsSection(c *ui.Context, build core.EnginePlatformBuild) {
	t := tokens(c)
	sectionTitle := "下载文件"
	if len(build.Artifacts) == 1 && build.Artifacts[0].Role == "release_page" {
		sectionTitle = "下载地址"
	}
	heading(c, sectionTitle, 3)

	for i, art := range build.Artifacts {
		index, artifact := i, art
		title := artifact.Label
		if title == "" {
			title = artifact.Filename
		}
		applyMaterial(c, ui.Column(c.Key(fmt.Sprintf("downloads-art-%s-%d", build.ID, index))).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, title).Bold()
				if artifact.Bytes > 0 {
					badge(c, catalogHumanSize(artifact.Bytes), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				}
			})
			ui.Text(c, artifact.URL).Selectable().FontSize(t.Type.Caption)
			if artifact.Role != "release_page" {
				shaLine := "SHA-256：未提供"
				if sha := strings.TrimSpace(artifact.SHA256); sha != "" {
					shaLine = "SHA-256：" + sha
				}
				ui.Text(c, shaLine).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				outlineButton(c, "复制地址").Label(fmt.Sprintf("复制地址 · %s · %d", build.ID, index)).OnClick(func() {
					s.copyDownloadText(c, "下载地址", artifact.URL)
				})
				if sha := strings.TrimSpace(artifact.SHA256); sha != "" {
					outlineButton(c, "复制 SHA-256").Label(fmt.Sprintf("复制 SHA-256 · %s · %d", build.ID, index)).OnClick(func() {
						s.copyDownloadText(c, "SHA-256", sha)
					})
				}
				outlineButton(c, "打开链接").Label(fmt.Sprintf("打开链接 · %s · %d", build.ID, index)).OnClick(func() {
					s.openDownloadURL(c, title, artifact.URL)
				})
			})
		})
	}
}

func (s *viewState) engineDrawerInstallSection(
	c *ui.Context,
	st *downloadsUIState,
	family core.EngineFamilyCatalog,
	rel core.EngineRelease,
	build core.EnginePlatformBuild,
	hostPlatform string,
) {
	t := tokens(c)
	var polystrataSelected starterOption
	var hasPolystrataOpt bool
	note := strings.TrimSpace(build.Note)
	cmd := strings.TrimSpace(build.InstallCommand)
	if family.ID == "polystrata" && build.AcquireMode == core.EngineAcquireManagedSetup && (build.Platform == hostPlatform || build.Platform == "any") {
		polystrataSelected, hasPolystrataOpt = s.polystrataDrawerSetupSection(c, st, hostPlatform)
		if hasPolystrataOpt {
			cmd = polystrataCLICommand(build, polystrataRequestForOption(polystrataSelected, st.polystrataModelSource))
		}
	}
	if note != "" || cmd != "" {
		heading(c, "安装说明", 3)
		if note != "" {
			ui.Text(c, note).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if cmd != "" {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
				ui.Text(c, cmd).Selectable().FontSize(t.Type.Caption)
				outlineButton(c, "复制命令").OnClick(func() {
					s.copyDownloadText(c, "安装命令", cmd)
				})
			})
		}
	}

	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		switch {
		case build.AcquireMode == core.EngineAcquireManagedSetup && (build.Platform == hostPlatform || build.Platform == "any"):
			if s.downloadActive("polystrata") {
				button(c, "安装中…").Label("安装此版本 · " + family.ID).Disabled(true)
			} else {
				confirmLabel := "开始安装 PolyStrata"
				if s.isEngineFamilyInstalled(family.ID) {
					confirmLabel = "安装所选模型"
				}
				primaryButton(c, confirmLabel).Label("安装此版本 · " + family.ID).Disabled(s.busy || !hasPolystrataOpt).OnClick(func() {
					if !hasPolystrataOpt {
						return
					}
					req := polystrataRequestForOption(polystrataSelected, st.polystrataModelSource)
					st.engineDrawerOpen = false
					s.acquirePolystrataSetup(req)
				})
			}
		case build.CanDirectDownloadOn(hostPlatform):
			if s.isEngineSourceDownloading(build.PrimaryURL()) {
				button(c, "安装中…").Label("安装此版本 · " + family.ID).Disabled(true)
			} else {
				primaryButton(c, "安装此版本").Label("安装此版本 · " + family.ID).Disabled(s.busy).OnClick(func() {
					s.acquireEngine(build.PrimaryURL(), "")
				})
			}
		case build.PrimaryURL() != "":
			primaryButton(c, "打开下载页").Label("打开下载页 · " + family.ID).OnClick(func() {
				s.openDownloadURL(c, family.Name, build.PrimaryURL())
			})
		}
		if family.SupportsWizard {
			outlineButton(c, "注册本地引擎").Label("注册本地引擎 · " + family.ID).OnClick(func() {
				st.engineDrawerOpen = false
				s.openLocalEngineWizard(family.ID)
			})
		}
		if relURL := engineReleaseLink(family, rel); relURL != "" && relURL != build.PrimaryURL() {
			ghostButton(c, "打开发布页").OnClick(func() {
				s.openDownloadURL(c, family.Name+" "+rel.Version, relURL)
			})
		}
	})
}

func engineChannelMix(releases []core.EngineRelease) (hasStable, hasNonStable bool) {
	for _, rel := range releases {
		if rel.Channel == core.ReleaseStable || rel.Channel == core.ReleaseLatest {
			hasStable = true
		} else {
			hasNonStable = true
		}
	}
	return hasStable, hasNonStable
}

func filterEngineReleases(releases []core.EngineRelease, stableOnly bool) []core.EngineRelease {
	if !stableOnly {
		return releases
	}
	var out []core.EngineRelease
	for _, rel := range releases {
		if rel.Channel == core.ReleaseStable || rel.Channel == core.ReleaseLatest {
			out = append(out, rel)
		}
	}
	if len(out) == 0 {
		return releases
	}
	return out
}

func resolveEngineRelease(wantID string, releases []core.EngineRelease) core.EngineRelease {
	for _, rel := range releases {
		if rel.ID == wantID {
			return rel
		}
	}
	return releases[0]
}

func resolveEnginePlatform(saved, host string, platforms []string) string {
	for _, candidate := range []string{saved, host, "any"} {
		if candidate != "" && slices.Contains(platforms, candidate) {
			return candidate
		}
	}
	return platforms[0]
}

func pickEngineBuildForPlatform(st *downloadsUIState, familyID string, rel core.EngineRelease, platform string) core.EnginePlatformBuild {
	builds := rel.BuildsForPlatform(platform)
	if len(builds) == 0 {
		return rel.Builds[0]
	}
	return pickEngineBuildForID(st.selectedBuild[familyID], builds)
}

func pickEngineBuildForID(wantID string, builds []core.EnginePlatformBuild) core.EnginePlatformBuild {
	for _, b := range builds {
		if b.ID == wantID {
			return b
		}
	}
	return builds[0]
}

func engineReleaseOptionLabel(rel core.EngineRelease) string {
	label := rel.Version + " · " + rel.Distribution + " · " + core.ChannelLabel(rel.Channel)
	if rel.Recommended {
		label += " · 推荐"
	}
	return label
}

func engineReleaseLink(family core.EngineFamilyCatalog, rel core.EngineRelease) string {
	if rel.ReleaseURL != "" {
		return rel.ReleaseURL
	}
	if family.ReleasesURL != "" {
		return family.ReleasesURL
	}
	return family.HomepageURL
}

func engineChannelState(ch core.ReleaseChannel) stateKind {
	switch ch {
	case core.ReleaseStable, core.ReleaseLatest:
		return stateSuccess
	case core.ReleasePreRelease:
		return stateWarning
	default:
		return stateNeutral
	}
}
