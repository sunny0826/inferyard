package pages

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsStarterCard(c *ui.Context) {
	rec := s.starterRecommendation()
	if !s.hasStarterContent(rec) {
		return
	}
	if !s.starterPanelExpanded() {
		s.downloadsCompactStarterBar(c, rec)
		return
	}
	s.renderStarterRecommendationPanel(c, "downloads")
}

func (s *viewState) downloadsCompactStarterBar(c *ui.Context, rec core.StarterRecommendation) {
	t := tokens(c)
	opts := starterOptions(rec)
	hwSummary := s.starterHardwareSummary(rec)

	applyMaterial(c, ui.Row(c.Key("downloads-starter-compact-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Stack, t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			if rec.Status == "blocked" && len(opts) == 0 {
				badge(c, "硬件推荐暂不可用", stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				reason := deviceLabel(rec.Reason)
				if reason == "" {
					reason = "当前硬件不满足默认推荐条件。"
				}
				ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			} else if len(opts) > 0 {
				top := opts[0]
				badge(c, "本机硬件推荐", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, starterEngineLabel(top.Engine)+" · "+starterOptionTitle(top)).Bold().SingleLine()
				meta := []string{}
				if hwSummary != "" {
					meta = append(meta, hwSummary)
				}
				if len(opts) > 1 {
					meta = append(meta, fmt.Sprintf("共 %d 套方案", len(opts)))
				}
				if len(meta) > 0 {
					ui.Text(c, joinParts(meta)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
			}
		})
		outlineButton(c, "展开推荐方案").OnClick(func() {
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
				heading(c, "推荐组合", 3)
				badge(c, "检测中", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Text(c, "正在检测本机硬件配置与可用磁盘空间…").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
	showCollapse := contextMode == "downloads" && s.starterDefaultCollapsed()

	applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		if rec.Status == "blocked" && len(opts) == 0 {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						heading(c, "推荐组合", 3)
						badge(c, "暂不可用", stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
					})
					if hwSummary != "" {
						ui.Text(c, "本机配置："+hwSummary).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
					reason := deviceLabel(rec.Reason)
					if reason == "" {
						reason = "当前硬件不满足默认推荐条件。"
					}
					ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if notes := localizedDeviceNotes(rec.Notes); len(notes) > 0 {
						ui.Text(c, strings.Join(notes, "；")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
				})
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					outlineButton(c, "重新检测").Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
					button(c, "查看设备").OnClick(func() { s.selected = "device" })
					if showCollapse {
						ghostButton(c, "收起").OnClick(func() { s.setStarterPanelExpanded(false) })
					}
				})
			})
			return
		}

		// Header row: title + option count + refresh / collapse controls
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				heading(c, "推荐组合", 3)
				badge(c, fmt.Sprintf("%d 套可选", len(opts)), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ghostButton(c, "重新检测").Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
				if showCollapse {
					outlineButton(c, "收起").OnClick(func() { s.setStarterPanelExpanded(false) })
				}
			})
		})

		// Hardware summary strip
		if hwSummary != "" {
			applyMaterial(c, ui.Row(c.Key("starter-hardware-strip")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Inset, t.Density.Stack), materialSunken).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					components.Icon(c, lucide.Cpu).Size(14, 14).TextColor(t.Color.FgSecondary)
					ui.Text(c, "本机配置："+hwSummary).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).FontFeatures("tnum")
				})
				ghostButton(c, "设备详情").OnClick(func() { s.selected = "device" })
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

		// Flat option cards row (2~4 cards side-by-side, wrapping cleanly on narrow widths)
		ui.Row(c.Key("starter-options-grid")).FillWidth().Wrap().AlignItems(ui.Stretch).Gap(t.Density.Row).Children(func() {
			for i, opt := range opts {
				idx, option := i, opt
				s.starterOptionTile(c, st, idx, option, idx == st.starterChoice)
			}
		})

		chosen := opts[st.starterChoice]

		// Inline macOS Xcode CLT notice when PolyStrata is selected
		if chosen.Engine == "polystrata" && slices.Contains(rec.Notes, "xcode_clt_missing") {
			applyMaterial(c, ui.Row(c.Key("starter-xcode-clt-hint")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
				badge(c, "前置工具", stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, "未检测到 Xcode Command Line Tools，首次构建前请先安装：").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				ui.Text(c, "xcode-select --install").Selectable().FontSize(t.Type.Caption)
				outlineButton(c, "复制命令").Label("复制 Xcode CLT 安装命令").OnClick(func() {
					s.copyDownloadText(c, "安装命令", "xcode-select --install")
				})
			})
		}

		// Other hardware/environment notes
		if notes := starterCardNotes(rec.Notes, chosen.Engine == "polystrata"); len(notes) > 0 {
			ui.Text(c, "提示："+strings.Join(notes, "；")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}

		// Unified bottom bar: model source selector + primary CTA
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c.Key("starter-action-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c.Key("starter-model-source-row")).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, "模型下载源").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				if chosen.Engine == "polystrata" {
					ui.Box(c.Key("starter-source-polystrata")).Children(func() {
						prev := st.polystrataModelSource
						segmentedControl(c, "PolyStrata 模型源", &st.polystrataModelSource, []components.NativeSelectOption[string]{
							{Value: "auto", Label: "自动"},
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
						segmentedControl(c, "推荐模型源", &st.modelSource, []components.NativeSelectOption[string]{
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
					outlineButton(c, "更多配置…").Label("更多 PolyStrata 配置").OnClick(func() {
						st.polystrataDrawerModelID = chosen.ModelID
						if contextMode == "dashboard" {
							s.selected = "downloads"
						}
						s.openPolystrataDrawerForCurrentHost()
					})
				}
				if contextMode == "dashboard" {
					outlineButton(c, "到下载页").OnClick(func() {
						s.selected = "downloads"
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

	tile := ui.Column(c.Key("starter-option-" + opt.ModelID)).MinWidth(210).Grow(1).Gap(t.Density.Row).Padding(t.Density.Stack).Radius(t.Radius.Floating).Label("选择推荐方案 · " + opt.ModelID)
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
					badge(c, "已安装", stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
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
				starterTileMetric(c, "预计下载", formatDownloadBytes(opt.EstimatedDownloadBytes))
			}
			if opt.EstimatedRAMBytes > 0 {
				starterTileMetric(c, "预计内存", formatDownloadBytes(opt.EstimatedRAMBytes))
			}
			if opt.RecommendedContext > 0 {
				starterTileMetric(c, "上下文", starterContextShort(opt.RecommendedContext))
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
			button(c, "安装中…").Disabled(true)
			return
		}
		if s.isStarterOptionInstalled(chosen) {
			button(c, "已就绪").Disabled(true)
			return
		}
		label := "一键安装 PolyStrata"
		if s.isEngineFamilyInstalled("polystrata") {
			label = "安装所选 PolyStrata 模型"
		}
		if primaryButton(c, label).Disabled(s.busy).Clicked() {
			s.startStarterOption(chosen, navigateToDownloads)
		}
	default:
		engineInstalled := s.isStarterEngineInstalled(chosen)
		modelInstalled := s.isStarterModelInstalled(chosen)
		if engineInstalled && modelInstalled {
			button(c, "已就绪").Disabled(true)
			return
		}
		hostPlatform := s.downloadsHostPlatform()
		engineURL := starterLlamaCppEngineURL(hostPlatform, chosen.Backend)
		modelURL := starterModelURLForOption(chosen, st.modelSource)
		switch {
		case engineInstalled && modelURL != "":
			if primaryButton(c, "下载推荐模型").Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		case modelInstalled && engineURL != "":
			if primaryButton(c, "下载推荐引擎").Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		case !engineInstalled && !modelInstalled && engineURL != "" && modelURL != "":
			if primaryButton(c, "下载推荐组合").Disabled(s.busy).Clicked() {
				s.startStarterOption(chosen, navigateToDownloads)
			}
		default:
			t := tokens(c)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				primaryButton(c, "查看引擎获取方式").OnClick(func() {
					if navigateToDownloads {
						s.selected = "downloads"
					}
					relID, buildID := starterLlamaCppDrawerTarget(s.engineCatalogRegistry(), hostPlatform, chosen.Backend)
					st.openEngineDrawer("llama-cpp", relID, hostPlatform, buildID)
				})
				if !modelInstalled && modelURL != "" {
					outlineButton(c, "下载推荐模型").Disabled(s.busy).OnClick(func() {
						if navigateToDownloads {
							s.setStarterPanelExpanded(false)
							s.selected = "downloads"
						}
						s.acquireModelSource(modelURL, "")
					})
				}
			})
		}
	}
}
