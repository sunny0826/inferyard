//go:build ui_debug && !release

package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/core"
)

func (p *previewSession) ensurePrototypeState() *downloadsProtoState {
	if p.protoState == nil {
		p.protoState = newDownloadsProtoState()
	}
	if p.protoState.selectedRelease == nil {
		p.protoState.selectedRelease = map[string]string{}
	}
	if p.protoState.selectedPlatform == nil {
		p.protoState.selectedPlatform = map[string]string{}
	}
	if p.protoState.selectedBuild == nil {
		p.protoState.selectedBuild = map[string]string{}
	}
	return p.protoState
}

func (p *previewSession) syncPrototypeAssetToggle() {
	s := p.state
	if !s.assetsPreset {
		s.assetsPreset = true
		if s.modelURL == "" {
			s.modelURL = core.DefaultModelURL()
		}
		if s.engineURL == "" {
			s.engineURL = core.DefaultEngineURL()
		}
	}
	if p.protoNoAssets == p.protoNoAssetsApplied {
		return
	}
	p.protoNoAssetsApplied = p.protoNoAssets
	if p.protoNoAssets {
		s.registry.Engines = nil
		s.registry.Models = nil
		s.registry.DefaultEngine = ""
		s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{}}
		s.engine, s.model = "", ""
		return
	}
	seedPrototypeAssets(s)
}

func (p *previewSession) downloadsPrototypeView(c *ui.Context) {
	s := p.state
	p.ensurePrototypeState()
	p.syncPrototypeAssetToggle()
	s.consumeFeedback(c)
	s.dropStaleStopPrompt()
	s.clearPromptOnNavigation()
	t := tokens(c)

	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		s.cornerStatus(c)
		ui.Scroll(c.Key("downloads-prototype-scroll")).Grow(1).TrackScroll(s.pageScroll()).Padding(t.Density.Page).Children(func() {
			ui.Column(c).Gap(t.Density.Section).Children(func() {
				s.feedback(c)
				p.downloadsPrototypeHeader(c)
				s.downloadTaskGuide(c)
				p.downloadsPrototypeStarterCard(c)
				if len(s.downloads) > 0 {
					card(c, "下载任务", func() { s.downloadsView(c) })
				}
				p.downloadsPrototypeTabs(c)
			})
		})
	})
	p.downloadsPrototypeDetailDrawer(c)
	s.studioNotifications(c)
	studioToasts(c)
}

func (p *previewSession) downloadsPrototypeHeader(c *ui.Context) {
	s := p.state
	t := tokens(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Children(func() { s.header(c, "下载") })
		button(c, "下载设置").OnClick(func() { s.selected = "settings" })
	})
}

func (p *previewSession) downloadsPrototypeStarterCard(c *ui.Context) {
	s := p.state
	assets := s.availableTaskAssets()
	engineURL := core.DefaultEngineURL()
	modelURL := core.DefaultModelURL()
	if assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || engineURL == "" || modelURL == "" {
		return
	}
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
				heading(c, "推荐组合", 3)
				ui.Text(c, "llama-server (prism-b10743-adfffbe) · Qwen3-0.6B-Q8_0 (GGUF)").
					FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			if primaryButton(c, "下载推荐组合").Disabled(s.busy).Clicked() {
				s.acquireStarterBundle(engineURL, modelURL)
			}
		})
	})
}

func (p *previewSession) downloadsPrototypeTabs(c *ui.Context) {
	st := p.ensurePrototypeState()
	t := tokens(c)
	ui.Column(c.Key("proto-downloads-tabs")).FillWidth().Gap(t.Density.Row).Children(func() {
		components.Tabs(c, &st.activeTab, components.TabsUnderline, "引擎", "模型")
		if st.activeTab == 1 {
			applyMaterial(c, ui.Column(c).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
				p.state.modelAssetsView(c)
			})
			return
		}
		p.downloadsPrototypeEnginesTab(c, st)
	})
}

func (p *previewSession) downloadsPrototypeEnginesTab(c *ui.Context, st *downloadsProtoState) {
	s := p.state
	t := tokens(c)
	hostPlatform := st.effectiveHostPlatform()
	families := previewPrototypeCatalog(p.protoIncludeCustomEngine).Families()

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

	ui.Column(c.Key("proto-engines-tab")).FillWidth().Gap(t.Density.Section).Children(func() {
		applyMaterial(c, ui.Column(c).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Children(func() { heading(c, "引擎", 2) })
				outlineButton(c, "关联已有引擎").OnClick(func() { s.openLocalEngineWizard("") })
			})
			if len(managed) == 0 {
				ui.Text(c, "当前平台暂无可用引擎。").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			for i, fam := range managed {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
				rel := protoPickRelease(st, fam.ID, hostReleases)
				build := protoPickBuildForPlatform(st, fam.ID, rel, hostPlatform)
				p.protoEngineRow(c, st, fam, rel, build)
			}
		})

		if len(externalApps) > 0 {
			card(c, "外部应用", func() {
				for i, fam := range externalApps {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
					rel := protoPickRelease(st, fam.ID, hostReleases)
					build := protoPickBuildForPlatform(st, fam.ID, rel, hostPlatform)
					p.protoExternalAppRow(c, fam, rel, build)
				}
			})
		}

		if len(otherPlatform) > 0 {
			componentFoldKeyed(c, "proto-other-platform", fmt.Sprintf("其他平台引擎（%d）", len(otherPlatform)), func() {
				for i, fam := range otherPlatform {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					p.protoOtherPlatformRow(c, st, fam)
				}
			})
		}

		componentFoldKeyed(c, "proto-custom-engine-url", "自定义引擎地址", func() {
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				field(c, "引擎归档 URL", &st.customEngineURL)
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					trimmed := strings.TrimSpace(st.customEngineURL)
					button(c, "下载并验证").Disabled(s.busy || trimmed == "").OnClick(func() {
						s.acquireEngine(trimmed, "")
					})
				})
			})
		})
	})
}

func (p *previewSession) protoEngineRow(c *ui.Context, st *downloadsProtoState, fam core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild) {
	t := tokens(c)
	installed := p.isProtoFamilyInstalled(fam.ID)
	installing := p.isProtoFamilyInstalling(fam.ID)

	ui.Row(c.Key("proto-engine-" + fam.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, fam.Name).Bold()
				badge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if installed {
					badge(c, "已安装", stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if installing {
					badge(c, "安装中", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				metaParts := []string{rel.Distribution, rel.Version}
				if build.Variant != "" {
					metaParts = append(metaParts, build.Variant)
				}
				if total := build.TotalBytes(); total > 0 {
					metaParts = append(metaParts, catalogHumanSize(total))
				}
				ui.Text(c, joinParts(metaParts)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				badge(c, core.ChannelLabel(rel.Channel), protoChannelState(rel.Channel), badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			outlineButton(c, "版本与地址").Label("版本与地址 · " + fam.ID).OnClick(func() {
				st.openDrawer(fam.ID, rel.ID, build.Platform, build.ID)
			})
			if installing {
				button(c, "安装中…").Label("安装 · " + fam.ID).Disabled(true)
			} else {
				button(c, "安装").Label("安装 · " + fam.ID).Disabled(p.state.busy).OnClick(func() {
					p.startPrototypeInstall(fam, rel, build)
				})
			}
		})
	})
}

func (p *previewSession) protoExternalAppRow(c *ui.Context, fam core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild) {
	t := tokens(c)
	installed := p.isProtoFamilyInstalled(fam.ID)

	ui.Row(c.Key("proto-ext-" + fam.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, fam.Name).Bold()
				badge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if installed {
					badge(c, "已关联", stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			metaParts := []string{rel.Distribution, rel.Version}
			if build.Variant != "" {
				metaParts = append(metaParts, build.Variant)
			}
			ui.Text(c, joinParts(metaParts)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			outlineButton(c, "打开官网").Label("打开官网 · " + fam.ID).OnClick(func() {
				p.openProtoURL(fam.Name, build.PrimaryURL())
			})
			button(c, "关联").Label("关联 · " + fam.ID).OnClick(func() {
				p.state.openLocalEngineWizard(fam.ID)
			})
		})
	})
}

func (p *previewSession) protoOtherPlatformRow(c *ui.Context, st *downloadsProtoState, fam core.EngineFamilyCatalog) {
	t := tokens(c)
	rel := protoPickRelease(st, fam.ID, fam.Releases)
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

	ui.Row(c.Key("proto-other-" + fam.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, fam.Name).Bold()
				badge(c, core.ModelFormatLabel(fam.ModelFormat), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			})
			ui.Text(c, "支持平台："+protoPlatformSummary(rel)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		outlineButton(c, "版本与地址").Label("版本与地址 · " + fam.ID).OnClick(func() {
			st.openDrawer(fam.ID, rel.ID, firstPlatform, firstBuild.ID)
		})
	})
}

func protoPlatformSummary(rel core.EngineRelease) string {
	platforms := rel.Platforms()
	labels := make([]string, 0, len(platforms))
	for _, plat := range platforms {
		labels = append(labels, core.PlatformLabel(plat))
	}
	return strings.Join(labels, "、")
}
