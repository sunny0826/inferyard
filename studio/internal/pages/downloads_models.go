package pages

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsModelsTab(c *ui.Context) {
	st := s.ensureDownloadsUI()
	t := tokens(c)
	hostPlatform := s.downloadsHostPlatform()

	var supported, otherPlatform []core.CatalogEntry
	for _, entry := range core.Catalog() {
		if entry.Kind != core.CatalogModel || !entry.Downloadable() {
			continue
		}
		if entry.Platform == "any" || entry.Platform == hostPlatform {
			supported = append(supported, entry)
		} else {
			otherPlatform = append(otherPlatform, entry)
		}
	}

	ui.Column(c.Key("downloads-models-tab")).FillWidth().Gap(t.Density.Section).Children(func() {
		applyMaterial(c, ui.Column(c).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Children(func() { heading(c, "模型", 2) })
				prev := st.modelSource
				segmentedControl(c, "下载源", &st.modelSource, []components.NativeSelectOption[string]{
					{Value: "huggingface", Label: "HuggingFace"},
					{Value: "modelscope", Label: "ModelScope"},
				})
				if st.modelSource != prev {
					st.setDirectModelSource(st.modelSource)
				}
			})
			if len(supported) == 0 {
				ui.Text(c, "当前平台暂无预设模型。").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			for i, entry := range supported {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				s.modelCatalogRow(c, st, entry)
			}
		})

		if len(otherPlatform) > 0 {
			componentFoldKeyed(c, "downloads-other-platform-models", fmt.Sprintf("其他平台模型（%d）", len(otherPlatform)), func() {
				for i, entry := range otherPlatform {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					s.otherPlatformModelRow(c, st, entry)
				}
			})
		}

		s.customModelURLFold(c, st)
	})
}

func (s *viewState) modelCatalogRow(c *ui.Context, st *downloadsUIState, entry core.CatalogEntry) {
	t := tokens(c)
	activeURL, activeSourceLabel := modelCatalogSource(entry, st.modelSource)
	downloaded := s.isModelEntryDownloaded(entry)
	downloading := s.isModelEntryDownloading(entry)
	sizeText := "目录权重"
	if entry.Bytes > 0 {
		sizeText = catalogHumanSize(entry.Bytes)
	}

	ui.Row(c.Key("downloads-model-" + entry.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, modelCatalogTitle(entry)).Bold()
				badge(c, core.ModelFormatLabel(modelCatalogFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if downloaded {
					badge(c, "已下载", stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if downloading {
					badge(c, "下载中", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			meta := joinParts([]string{modelCatalogEngineLabel(entry), sizeText, activeSourceLabel})
			ui.Text(c, meta).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			outlineButton(c, "详情与地址").Label("模型详情 · " + entry.ID).OnClick(func() {
				st.openModelDrawer(entry.ID)
			})
			if downloading {
				button(c, "下载中…").Label("下载模型 · " + entry.ID).Disabled(true)
			} else {
				s.downloadAction(c, "model", "下载").Label("下载模型 · " + entry.ID).Disabled(s.busy).OnClick(func() {
					s.acquireModelSource(activeURL, "")
				})
			}
		})
	})
}

func (s *viewState) otherPlatformModelRow(c *ui.Context, st *downloadsUIState, entry core.CatalogEntry) {
	t := tokens(c)
	ui.Row(c.Key("downloads-other-model-" + entry.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, modelCatalogTitle(entry)).Bold()
				badge(c, core.ModelFormatLabel(modelCatalogFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			})
			ui.Text(c, "支持平台："+core.PlatformLabel(entry.Platform)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		outlineButton(c, "详情与地址").Label("模型详情 · " + entry.ID).OnClick(func() {
			st.openModelDrawer(entry.ID)
		})
	})
}

func (s *viewState) customModelURLFold(c *ui.Context, st *downloadsUIState) {
	t := tokens(c)
	resState := st.customModelResolve

	componentFoldKeyed(c, "downloads-custom-model-url", "自定义模型地址", func() {
		trimmed := strings.TrimSpace(st.customModelURL)
		tokenEnv := s.tokenEnv
		if resState.source != trimmed || resState.tokenEnv != tokenEnv {
			resState.invalidate(trimmed, tokenEnv)
		}
		resState.mailbox.mu.Lock()
		pending := resState.mailbox.pending
		resState.mailbox.pending = nil
		resState.mailbox.mu.Unlock()
		if pending != nil && pending.reqID == resState.reqID && pending.source == trimmed && pending.tokenEnv == tokenEnv {
			resState.resolving = false
			resState.resolved = pending.resolved
			resState.errText = pending.errText
		}

		ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
			field(c, "模型地址", &st.customModelURL).
				Description("支持 HuggingFace 或 ModelScope 的 GGUF 文件或 /tree 目录地址。").
				Error(customModelURLError(st.customModelURL))
			componentFoldKeyed(c, "downloads-custom-model-auth", "访问凭据（私有仓库可选）", func() {
				field(c, "凭据环境变量名", &s.tokenEnv).
					Description("仅填写变量名，不填写凭据值。").
					Error(tokenNameError(s.tokenEnv))
			})

			tokenOK := tokenNameError(s.tokenEnv) == ""
			urlValid := trimmed != "" && customModelURLError(trimmed) == "" && tokenOK
			canResolve := !s.busy && !resState.resolving && trimmed != "" && core.IsResolvableModelURL(trimmed) && tokenOK
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				outlineButton(c, "解析地址").Disabled(!canResolve).OnClick(func() {
					s.triggerModelResolve(resState, trimmed, tokenEnv)
				})
				primaryButton(c, "下载模型").Label("下载自定义模型").Disabled(s.busy || !urlValid).OnClick(func() {
					source := trimmed
					if resState.resolved != nil && resState.resolved.PinnedURL != "" {
						source = resState.resolved.PinnedURL
					}
					s.acquireModelSource(source, s.tokenEnv)
				})
			})

			switch {
			case resState.resolving:
				ui.Text(c, "正在解析远端模型版本…").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			case resState.errText != "":
				ui.Text(c, resState.errText).FontSize(t.Type.Caption).TextColor(stateColor(c, stateDanger))
			case resState.resolved != nil:
				sizeText := "大小未知"
				if resState.resolved.Bytes != nil && *resState.resolved.Bytes > 0 {
					sizeText = catalogHumanSize(*resState.resolved.Bytes)
				}
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
					ui.Text(c, "已解析版本："+resState.resolved.Revision).Selectable().FontSize(t.Type.Caption)
					ui.Text(c, "文件："+resState.resolved.Filename+" · "+sizeText).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					ui.Text(c, "固定地址："+resState.resolved.PinnedURL).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					outlineButton(c, "复制固定地址").OnClick(func() {
						s.copyDownloadText(c, "固定地址", resState.resolved.PinnedURL)
					})
				})
			}
		})
	})
}

func (s *viewState) downloadsModelDrawer(c *ui.Context) {
	st := s.ensureDownloadsUI()
	c.Key("downloads-model-detail-drawer")
	if !st.modelDrawerOpen {
		drawer(c, &st.modelDrawerOpen, drawerOptions{Label: "模型详情", Title: "模型详情"}, nil)
		return
	}
	entry, ok := core.CatalogEntryByID(st.modelDrawerID)
	if !ok || entry.Kind != core.CatalogModel {
		st.modelDrawerOpen = false
		drawer(c, &st.modelDrawerOpen, drawerOptions{Label: "模型详情", Title: "模型详情"}, nil)
		return
	}

	hostPlatform := s.downloadsHostPlatform()
	activeURL, activeSourceLabel := modelCatalogSource(entry, st.modelSource)
	usingMirror := entry.MirrorURL != "" && st.modelSource == "modelscope"
	versionText := entry.Revision
	if usingMirror || versionText == "" {
		versionText = "自动解析"
	}

	drawer(c, &st.modelDrawerOpen, drawerOptions{
		Label: "模型详情",
		Title: modelCatalogTitle(entry),
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("downloads-model-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				if entry.MirrorURL != "" {
					prev := st.modelSource
					segmentedControl(c, "模型详情下载源", &st.modelSource, []components.NativeSelectOption[string]{
						{Value: "huggingface", Label: "HuggingFace"},
						{Value: "modelscope", Label: modelCatalogMirrorLabel(entry)},
					})
					if st.modelSource != prev {
						st.setDirectModelSource(st.modelSource)
					}
				}
				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					keyValueRow(c, "格式", core.ModelFormatLabel(modelCatalogFormat(entry)), false)
					keyValueRow(c, "适用引擎", modelCatalogEngineLabel(entry), false)
					keyValueRow(c, "支持平台", core.PlatformLabel(entry.Platform), false)
					if entry.Bytes > 0 {
						keyValueRow(c, "文件大小", catalogHumanSize(entry.Bytes), true)
					}
					keyValueRow(c, "版本", versionText, false)
				})
				if entry.Platform != "any" && entry.Platform != hostPlatform {
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Message: "当前模型仅适用于 " + core.PlatformLabel(entry.Platform) + "，无法在本机直接运行。",
					})
				}

				heading(c, "下载地址", 3)
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, activeSourceLabel).Bold()
						if entry.Bytes > 0 {
							badge(c, catalogHumanSize(entry.Bytes), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
						}
					})
					ui.Text(c, activeURL).Selectable().FontSize(t.Type.Caption)
					if !usingMirror && entry.SHA256 != "" {
						ui.Text(c, "SHA-256："+entry.SHA256).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						outlineButton(c, "复制地址").Label("复制模型地址 · " + entry.ID).OnClick(func() {
							s.copyDownloadText(c, "下载地址", activeURL)
						})
						if !usingMirror && entry.SHA256 != "" {
							outlineButton(c, "复制 SHA-256").Label("复制模型校验 · " + entry.ID).OnClick(func() {
								s.copyDownloadText(c, "SHA-256", entry.SHA256)
							})
						}
						outlineButton(c, "打开链接").Label("打开模型链接 · " + entry.ID).OnClick(func() {
							s.openDownloadURL(c, modelCatalogTitle(entry), activeURL)
						})
					})
				})

				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if s.isModelEntryDownloading(entry) {
						button(c, "下载中…").Label("下载此模型 · " + entry.ID).Disabled(true)
					} else {
						primaryButton(c, "下载此模型").Label("下载此模型 · " + entry.ID).Disabled(s.busy).OnClick(func() {
							s.acquireModelSource(activeURL, "")
						})
					}
					if repoURL := modelCatalogRepoURL(activeURL); repoURL != "" && repoURL != activeURL {
						ghostButton(c, "打开仓库").OnClick(func() {
							s.openDownloadURL(c, modelCatalogTitle(entry), repoURL)
						})
					}
				})
			})
		})
	})
}

func modelTaskMatchesEntry(task core.DownloadTask, entry core.CatalogEntry) bool {
	if task.Kind != "model" {
		return false
	}
	for _, src := range []string{task.SourceUrl, task.RequestedSourceUrl} {
		if src == "" {
			continue
		}
		if src == entry.URL || (entry.MirrorURL != "" && src == entry.MirrorURL) {
			return true
		}
	}
	return false
}

func modelAssetMatchesOutDir(assetPath, outDir string) bool {
	asset := filepath.Clean(strings.TrimSpace(assetPath))
	dir := filepath.Clean(strings.TrimSpace(outDir))
	if asset == "" || dir == "" || asset == "." || dir == "." {
		return false
	}
	rel, err := filepath.Rel(dir, asset)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func (s *viewState) isModelEntryDownloaded(entry core.CatalogEntry) bool {
	for _, task := range s.downloads {
		if task.State != "done" || !modelTaskMatchesEntry(task, entry) {
			continue
		}
		for _, m := range s.registry.Models {
			if m.Available && modelAssetMatchesOutDir(m.Path, task.OutDir) {
				return true
			}
		}
	}
	return false
}

func (s *viewState) isModelEntryDownloading(entry core.CatalogEntry) bool {
	for _, task := range s.downloads {
		if task.State == "downloading" && modelTaskMatchesEntry(task, entry) {
			return true
		}
	}
	return false
}

func modelCatalogMirrorLabel(entry core.CatalogEntry) string {
	if entry.MirrorLabel != "" {
		return entry.MirrorLabel
	}
	return "ModelScope"
}

func modelCatalogSource(entry core.CatalogEntry, source string) (activeURL, activeLabel string) {
	if source == "modelscope" && entry.MirrorURL != "" {
		return entry.MirrorURL, modelCatalogMirrorLabel(entry)
	}
	return entry.URL, "HuggingFace"
}

func modelCatalogTitle(entry core.CatalogEntry) string {
	title := strings.TrimSuffix(entry.Title, "（GGUF）")
	title = strings.TrimSuffix(title, "（目录模型）")
	return title
}

func modelCatalogFormat(entry core.CatalogEntry) string {
	if entry.Format == "directory" {
		return "directory"
	}
	return "gguf"
}

func modelCatalogEngineLabel(entry core.CatalogEntry) string {
	switch entry.Family {
	case "mlx-lm":
		return "mlx-lm"
	case "llama-cpp":
		return "llama.cpp / LM Studio"
	default:
		return entry.Family
	}
}

func modelCatalogRepoURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if strings.EqualFold(u.Host, "modelscope.cn") {
		if len(parts) >= 4 && parts[0] == "models" {
			return u.Scheme + "://" + u.Host + "/models/" + parts[1] + "/" + parts[2]
		}
		if len(parts) >= 2 {
			return u.Scheme + "://" + u.Host + "/" + parts[0] + "/" + parts[1]
		}
		return ""
	}
	if len(parts) >= 2 {
		return u.Scheme + "://" + u.Host + "/" + parts[0] + "/" + parts[1]
	}
	return ""
}
