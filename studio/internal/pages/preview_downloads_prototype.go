//go:build ui_debug && !release

package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
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
	ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				s.header(c, "下载")
				badge(c, "交互原型", stateInfo, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			button(c, "设置下载目录与代理").OnClick(func() { s.selected = "settings" })
		})
		ui.Text(c, "集中查看所有引擎的正式版本、适用平台与下载地址；模型可切换镜像源并自动解析固定版本。").
			FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
				heading(c, "快速开始 · 一键获取推荐组合", 3)
				ui.Text(c, "当前缺少可用引擎与模型，可一键下载本机推荐 llama-server（prism-b10743-adfffbe）与 Qwen3-0.6B-Q8_0（GGUF）。").
					FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			if button(c, "一键下载推荐引擎与模型").Disabled(s.busy).Clicked() {
				s.acquireStarterBundle(engineURL, modelURL)
			}
		})
	})
}

func (p *previewSession) downloadsPrototypeTabs(c *ui.Context) {
	st := p.ensurePrototypeState()
	t := tokens(c)
	ui.Column(c.Key("proto-downloads-tabs")).FillWidth().Gap(t.Density.Row).Children(func() {
		components.Tabs(c, &st.activeTab, components.TabsUnderline, "推理引擎", "模型资源")
		if st.activeTab == 1 {
			card(c, "获取模型", func() { p.state.modelAssetsView(c) })
			return
		}
		p.downloadsPrototypeEnginesTab(c)
	})
}

func (p *previewSession) downloadsPrototypeEnginesTab(c *ui.Context) {
	s := p.state
	st := p.ensurePrototypeState()
	t := tokens(c)

	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Box(c.Key("proto-platform-filter")).Children(func() {
			p.protoPlatformSelect(c, st)
		})
		segmentedControl(c, "筛选格式", &st.formatFilter, []components.NativeSelectOption[string]{
			{Value: "all", Label: "全部格式"},
			{Value: "gguf", Label: "GGUF"},
			{Value: "directory", Label: "目录模型"},
		})
		components.Checkbox(c, &st.stableOnly, "仅显示正式版")
		outlineButton(c, "注册本地引擎").OnClick(func() {
			s.openLocalEngineWizard("llama-cpp")
		})
	})

	targetPlatform := protoTargetPlatform(st.platform)
	if st.platform != "current" && st.platform != "all" && st.platform != core.CurrentPlatform() {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateInfo,
			Size:    bannerCompact,
			Message: "当前正在查看 " + core.PlatformLabel(st.platform) + " 引擎构建；可复制或打开地址，不直接安装到本机。",
		})
	}

	reg := previewPrototypeCatalog(p.protoIncludeCustomEngine)
	primaryClaimed := false
	matched := 0
	for _, family := range reg.Families() {
		if st.formatFilter != "all" && family.ModelFormat != st.formatFilter {
			continue
		}
		releases := family.ReleasesForPlatform(targetPlatform, st.stableOnly)
		if len(releases) == 0 {
			continue
		}
		matched++
		active := protoPickRelease(st, family.ID, releases)
		p.protoFamilyCard(c, family, releases, active, &primaryClaimed)
	}

	if matched == 0 {
		emptyGuideCard(c, emptyGuideOptions{
			Icon:        lucide.FileSearch,
			Title:       "未找到符合条件的引擎",
			Description: "请调整平台、模型格式或版本筛选条件。",
			Surface:     emptySunken,
			Action: func() {
				ghostButton(c, "清空筛选").OnClick(func() {
					st.platform = "current"
					st.formatFilter = "all"
					st.stableOnly = false
				})
			},
		})
	}

	card(c, "更多选项", func() {
		componentFoldKeyed(c, "proto-custom-engine", "自定义引擎归档地址", func() {
			field(c, "自定义引擎 URL", &st.customEngineURL).
				Placeholder("https://github.com/.../llama-...tar.gz").
				Description("输入受信任的 https 引擎归档地址直接下载并准备。").
				Error(downloadURLError(st.customEngineURL))
			trimmed := strings.TrimSpace(st.customEngineURL)
			button(c, "下载自定义引擎").
				Disabled(s.busy || trimmed == "" || downloadURLError(trimmed) != "").
				OnClick(func() { s.acquireEngine(trimmed, "") })
		})
	})
}

func (p *previewSession) protoPlatformSelect(c *ui.Context, st *downloadsProtoState) {
	options := []struct{ value, label string }{
		{"current", "当前平台 (" + core.PlatformLabel(core.CurrentPlatform()) + ")"},
		{"all", "全部平台"},
	}
	for _, plat := range core.CatalogPlatforms() {
		options = append(options, struct{ value, label string }{plat, core.PlatformLabel(plat)})
	}
	currentLabel := options[0].label
	for _, opt := range options {
		if opt.value == st.platform {
			currentLabel = opt.label
			break
		}
	}
	sel := components.Select(c, &st.platform)
	sel.Trigger.Label("筛选平台").Value(currentLabel).Children(func() {
		ui.Text(c, currentLabel).SingleLine()
	})
	sel.Popup(func(ui.Element) {
		components.ScrollView(c, components.ScrollViewOptions{}).Label("平台列表").MaxHeight(240).Children(func() {
			for _, opt := range options {
				sel.Item(opt.value).Label(opt.label).Children(func() {
					ui.Text(c, opt.label).SingleLine()
				})
			}
		})
	})
}

func (p *previewSession) protoFamilyCard(c *ui.Context, family core.EngineFamilyCatalog, releases []core.EngineRelease, active core.EngineRelease, primaryClaimed *bool) {
	t := tokens(c)
	st := p.ensurePrototypeState()
	ui.Column(c.Key("proto-family-" + family.ID)).FillWidth().Children(func() {
		applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, family.Name, 2)
					badge(c, core.ModelFormatLabel(family.ModelFormat), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
					if p.isProtoFamilyInstalled(family.ID) {
						badge(c, "已安装", stateSuccess, badgeOptions{Style: badgeDot, Size: sizeSM})
					}
					if len(releases) > 1 {
						ui.Text(c, fmt.Sprintf("%d 个可选版本", len(releases))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
				})
				if len(releases) > 1 {
					ui.Box(c.Key("proto-ver-" + family.ID)).Children(func() {
						chosenID := active.ID
						sel := components.Select(c, &chosenID)
						sel.Trigger.Label("选择版本 · " + family.ID).Value(active.Version + " · " + active.Distribution).Children(func() {
							ui.Text(c, active.Version+" · "+active.Distribution).SingleLine()
						})
						sel.Popup(func(ui.Element) {
							components.ScrollView(c, components.ScrollViewOptions{}).Label(family.Name + " 版本列表").MaxHeight(220).Children(func() {
								for _, rel := range releases {
									itemLabel := protoReleaseOptionLabel(rel)
									sel.Item(rel.ID).Label(itemLabel).Children(func() {
										ui.Text(c, itemLabel).SingleLine()
									})
								}
							})
						})
						if sel.Trigger.Changed() {
							st.selectedRelease[family.ID] = chosenID
						}
					})
				} else {
					badge(c, active.Version, stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				}
			})

			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, family.Summary).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).MinWidth(0)
				ghostButton(c, "官方发布页").Label("官方发布页 · " + family.ID).OnClick(func() {
					p.openProtoURL(family.Name+" 官方发布页", protoReleaseLink(family, active))
				})
			})

			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				for _, build := range active.Builds {
					p.protoBuildRow(c, family, active, build, primaryClaimed)
				}
			})
		})
	})
}

func (p *previewSession) protoBuildRow(c *ui.Context, family core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild, primaryClaimed *bool) {
	s := p.state
	st := p.ensurePrototypeState()
	t := tokens(c)
	primaryURL := build.PrimaryURL()

	ui.Column(c.Key("proto-build-" + rel.ID + "-" + build.ID)).FillWidth().Children(func() {
		applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Stack), materialInset).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, core.PlatformLabel(build.Platform)+" · "+build.Variant).Bold()
				badge(c, core.ChannelLabel(rel.Channel), protoChannelState(rel.Channel), badgeOptions{Style: badgeSoft, Size: sizeSM})
				if rel.Recommended {
					badge(c, "推荐", stateInfo, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
				if build.CanDirectDownloadOn(core.CurrentPlatform()) {
					badge(c, "应用内下载", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if build.AcquireMode == core.EngineAcquireDirect {
					badge(c, "仅限 "+core.PlatformLabel(build.Platform), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				} else {
					badge(c, core.AcquireModeLabel(build.AcquireMode), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				}
			})

			ui.Text(c, protoBuildMeta(rel, build)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			if build.Note != "" {
				ui.Text(c, build.Note).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
			}

			if primaryURL != "" {
				urlLabel := primaryURL
				if len(build.Artifacts) > 1 {
					urlLabel = "主包：" + primaryURL
				}
				urlText := ui.Text(c, urlLabel).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				components.Tooltip(c, urlText, primaryURL)
			}
			if len(build.Artifacts) > 1 {
				ui.Text(c, fmt.Sprintf("含 %d 个配套文件，点击「全部地址与校验」查看全部下载地址与 SHA-256。", len(build.Artifacts))).
					FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
			}

			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if build.CanDirectDownloadOn(core.CurrentPlatform()) {
					isPrimary := !*primaryClaimed && s.downloadPrimaryKind() == "engine" && rel.Recommended
					if isPrimary {
						*primaryClaimed = true
					}
					if p.isProtoSourceDownloading(primaryURL) {
						button(c, "下载中…").Label("下载中 · " + build.ID).Disabled(true)
					} else if isPrimary {
						primaryButton(c, "下载引擎").Label("下载引擎 · " + build.ID).Disabled(s.busy).OnClick(func() {
							s.acquireEngine(primaryURL, "")
						})
					} else {
						button(c, "下载引擎").Label("下载引擎 · " + build.ID).Disabled(s.busy).OnClick(func() {
							s.acquireEngine(primaryURL, "")
						})
					}
				} else if primaryURL != "" {
					openLabel := "打开官网下载"
					if build.AcquireMode == core.EngineAcquireManualArchive {
						openLabel = "浏览器打开"
					}
					button(c, openLabel).Label(openLabel + " · " + build.ID).OnClick(func() {
						p.openProtoURL(family.Name, primaryURL)
					})
				}
				if primaryURL != "" {
					outlineButton(c, "复制地址").Label("复制地址 · " + build.ID).OnClick(func() {
						p.copyProtoText("下载地址", primaryURL)
					})
				}
				detailLabel := "详情与校验"
				if len(build.Artifacts) > 1 {
					detailLabel = "全部地址与校验"
				}
				outlineButton(c, detailLabel).Label(detailLabel + " · " + build.ID).OnClick(func() {
					st.openDrawer(family.ID, rel.ID, build.ID)
				})
				if !build.CanDirectDownloadOn(core.CurrentPlatform()) && family.SupportsWizard {
					outlineButton(c, "去注册").Label("去注册 · " + build.ID).OnClick(func() {
						s.openLocalEngineWizard(family.ID)
					})
				}
			})
		})
	})
}
