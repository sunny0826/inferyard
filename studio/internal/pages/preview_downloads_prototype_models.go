//go:build ui_debug && !release

package pages

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func (p *previewSession) downloadsPrototypeModelsTab(c *ui.Context, st *downloadsProtoState) {
	if st.modelSource != "modelscope" {
		st.modelSource = "huggingface"
	}
	hostPlatform := st.effectiveHostPlatform()
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

	t := tokens(c)
	ui.Column(c.Key("proto-models-tab")).FillWidth().Gap(t.Density.Section).Children(func() {
		applyMaterial(c, ui.Column(c).MinWidth(0).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Children(func() { heading(c, "模型", 2) })
				segmentedControl(c, "下载源", &st.modelSource, []components.NativeSelectOption[string]{
					{Value: "huggingface", Label: "HuggingFace"},
					{Value: "modelscope", Label: "ModelScope"},
				})
			})
			if len(supported) == 0 {
				ui.Text(c, "当前平台暂无预设模型。").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			for i, entry := range supported {
				if i > 0 {
					ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				}
				p.protoModelRow(c, st, entry)
			}
		})

		if len(otherPlatform) > 0 {
			componentFoldKeyed(c, "proto-other-platform-models", fmt.Sprintf("其他平台模型（%d）", len(otherPlatform)), func() {
				for i, entry := range otherPlatform {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					p.protoOtherPlatformModelRow(c, st, entry)
				}
			})
		}

		p.protoCustomModelFold(c, st)
	})
}

func (p *previewSession) protoModelRow(c *ui.Context, st *downloadsProtoState, entry core.CatalogEntry) {
	t := tokens(c)
	activeURL, activeSourceLabel := protoModelSource(entry, st.modelSource)
	downloaded := p.isProtoModelDownloaded(entry)
	downloading := p.isProtoModelDownloading(entry.ID)
	sizeText := "目录权重"
	if entry.Bytes > 0 {
		sizeText = catalogHumanSize(entry.Bytes)
	}

	ui.Row(c.Key("proto-model-" + entry.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, protoModelTitle(entry)).Bold()
				badge(c, core.ModelFormatLabel(protoModelFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if downloaded {
					badge(c, "已下载", stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if downloading {
					badge(c, "下载中", stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			meta := joinParts([]string{protoModelEngineLabel(entry), sizeText, activeSourceLabel})
			ui.Text(c, meta).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			outlineButton(c, "详情与地址").Label("模型详情 · " + entry.ID).OnClick(func() {
				st.openModelDrawer(entry.ID)
			})
			if downloading {
				button(c, "下载中…").Label("下载模型 · " + entry.ID).Disabled(true)
			} else {
				button(c, "下载").Label("下载模型 · " + entry.ID).Disabled(p.state.busy).OnClick(func() {
					p.startPrototypeModelDownload(entry, activeURL)
				})
			}
		})
	})
}

func (p *previewSession) protoOtherPlatformModelRow(c *ui.Context, st *downloadsProtoState, entry core.CatalogEntry) {
	t := tokens(c)
	ui.Row(c.Key("proto-other-model-" + entry.ID)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, protoModelTitle(entry)).Bold()
				badge(c, core.ModelFormatLabel(protoModelFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			})
			ui.Text(c, "支持平台："+core.PlatformLabel(entry.Platform)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		outlineButton(c, "详情与地址").Label("模型详情 · " + entry.ID).OnClick(func() {
			st.openModelDrawer(entry.ID)
		})
	})
}

func (p *previewSession) protoCustomModelFold(c *ui.Context, st *downloadsProtoState) {
	s := p.state
	t := tokens(c)
	componentFoldKeyed(c, "proto-custom-model-url", "自定义模型地址", func() {
		trimmed := strings.TrimSpace(st.customModelURL)
		if trimmed != st.customResolveURL {
			st.customResolved = nil
			st.customResolveErr = ""
		}
		ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
			field(c, "模型地址", &st.customModelURL).
				Description("支持 HuggingFace 或 ModelScope 的 GGUF 文件或 /tree 目录地址。").
				Error(customModelURLError(st.customModelURL))
			componentFoldKeyed(c, "proto-custom-model-auth", "访问凭据（私有仓库可选）", func() {
				field(c, "凭据环境变量名", &st.customTokenEnv).
					Description("仅填写变量名，不填写凭据值。").
					Error(tokenNameError(st.customTokenEnv))
			})

			urlValid := trimmed != "" && customModelURLError(trimmed) == "" && tokenNameError(st.customTokenEnv) == ""
			canResolve := !s.busy && trimmed != "" && core.IsResolvableModelURL(trimmed) && tokenNameError(st.customTokenEnv) == ""
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				outlineButton(c, "解析地址").Disabled(!canResolve).OnClick(func() {
					res := previewResolveModelURL(trimmed)
					st.customResolveURL = trimmed
					st.customResolved = &res
					st.customResolveErr = ""
				})
				primaryButton(c, "下载模型").Label("下载自定义模型").Disabled(s.busy || !urlValid).OnClick(func() {
					p.startPrototypeCustomModelDownload(trimmed, st.customResolved)
				})
			})

			if st.customResolved != nil {
				sizeText := "大小未知"
				if st.customResolved.Bytes != nil && *st.customResolved.Bytes > 0 {
					sizeText = catalogSize(*st.customResolved.Bytes)
				}
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
					ui.Text(c, "版本："+st.customResolved.Revision).Selectable().FontSize(t.Type.Caption)
					ui.Text(c, "文件："+st.customResolved.Filename+" · "+sizeText).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					ui.Text(c, "固定地址："+st.customResolved.PinnedURL).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					outlineButton(c, "复制固定地址").OnClick(func() {
						p.copyProtoText("固定地址", st.customResolved.PinnedURL)
					})
				})
			}
		})
	})
}

func (p *previewSession) downloadsPrototypeModelDrawer(c *ui.Context) {
	st := p.ensurePrototypeState()
	c.Key("proto-model-detail-drawer")
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

	hostPlatform := st.effectiveHostPlatform()
	activeURL, activeSourceLabel := protoModelSource(entry, st.modelSource)
	usingMirror := entry.MirrorURL != "" && st.modelSource == "modelscope"
	versionText := entry.Revision
	if usingMirror || versionText == "" {
		versionText = "自动解析"
	}

	drawer(c, &st.modelDrawerOpen, drawerOptions{
		Label: "模型详情",
		Title: protoModelTitle(entry),
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("proto-model-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				if entry.MirrorURL != "" {
					segmentedControl(c, "模型详情下载源", &st.modelSource, []components.NativeSelectOption[string]{
						{Value: "huggingface", Label: "HuggingFace"},
						{Value: "modelscope", Label: protoMirrorLabel(entry)},
					})
				}
				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					keyValueRow(c, "格式", core.ModelFormatLabel(protoModelFormat(entry)), false)
					keyValueRow(c, "适用引擎", protoModelEngineLabel(entry), false)
					keyValueRow(c, "支持平台", core.PlatformLabel(entry.Platform), false)
					if entry.Bytes > 0 {
						keyValueRow(c, "文件大小", catalogSize(entry.Bytes), true)
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
							p.copyProtoText("下载地址", activeURL)
						})
						if !usingMirror && entry.SHA256 != "" {
							outlineButton(c, "复制 SHA-256").Label("复制模型校验 · " + entry.ID).OnClick(func() {
								p.copyProtoText("SHA-256", entry.SHA256)
							})
						}
						outlineButton(c, "打开链接").Label("打开模型链接 · " + entry.ID).OnClick(func() {
							p.openProtoURL(protoModelTitle(entry), activeURL)
						})
					})
				})

				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if p.isProtoModelDownloading(entry.ID) {
						button(c, "下载中…").Label("下载此模型 · " + entry.ID).Disabled(true)
					} else {
						primaryButton(c, "下载此模型").Label("下载此模型 · " + entry.ID).Disabled(p.state.busy).OnClick(func() {
							p.startPrototypeModelDownload(entry, activeURL)
						})
					}
					if repoURL := protoModelRepoURL(activeURL); repoURL != "" && repoURL != activeURL {
						ghostButton(c, "打开仓库").OnClick(func() {
							p.openProtoURL(protoModelTitle(entry), repoURL)
						})
					}
				})
			})
		})
	})
}

func (p *previewSession) startPrototypeModelDownload(entry core.CatalogEntry, sourceURL string) {
	if p.isProtoModelDownloading(entry.ID) {
		return
	}
	total := entry.Bytes
	if total <= 0 {
		total = 360 << 20
	}
	title := protoModelTitle(entry)
	format := protoModelFormat(entry)
	assetName := protoModelAssetName(entry)
	taskID := p.nextID("proto-model-" + entry.ID)
	p.runPrototypeModelDownload(taskID, title, assetName, format, sourceURL, total)
}

func (p *previewSession) startPrototypeCustomModelDownload(sourceURL string, resolved *inferyard.ModelResolve) {
	if sourceURL == "" {
		return
	}
	filename := protoCustomModelFilename(sourceURL)
	format := "gguf"
	if strings.Contains(sourceURL, "/tree/") {
		format = "directory"
	}
	var total int64 = 360 << 20
	if resolved != nil {
		if resolved.Filename != "" {
			filename = resolved.Filename
		}
		if resolved.Bytes != nil && *resolved.Bytes > 0 {
			total = *resolved.Bytes
		}
		if resolved.Kind == "directory" {
			format = "directory"
		}
	}
	taskID := p.nextID("proto-model-custom")
	p.runPrototypeModelDownload(taskID, filename, filename, format, sourceURL, total)
}

func (p *previewSession) runPrototypeModelDownload(taskID, displayName, assetName, format, sourceURL string, total int64) {
	s := p.state
	task := core.DownloadTask{
		TaskId:          taskID,
		Kind:            "model",
		DisplayName:     displayName,
		State:           "downloading",
		Stage:           "downloading",
		SourceUrl:       sourceURL,
		OutName:         assetName,
		OutDir:          "/tmp/preview/models",
		TotalBytes:      previewValue(total),
		DownloadedBytes: total / 4,
		BytesPerSecond:  previewValue(32.0 * 1024 * 1024),
		StartedAt:       p.timestamp(),
	}
	s.downloads = append(s.downloads, task)
	fail := p.takeFailure()

	group := "proto-model-" + taskID
	p.after(group, 350*time.Millisecond, func() {
		idx := p.findProtoTaskIndex(taskID)
		if idx < 0 || terminal(s.downloads[idx].State) {
			return
		}
		if fail {
			s.downloads[idx].State = "failed"
			s.downloads[idx].Stage = ""
			s.downloads[idx].BytesPerSecond = nil
			s.downloads[idx].Error = &core.ApiError{Code: "download_failed", Message: "模拟模型下载失败，请重试。"}
			s.downloads[idx].FinishedAt = previewValue(p.timestamp())
			p.notice = "模拟下载 " + displayName + " 失败。"
			s.enqueueStatusToast(displayName + " 下载失败")
			return
		}
		s.downloads[idx].Stage = "verifying"
		s.downloads[idx].DownloadedBytes = total * 4 / 5
		s.downloads[idx].BytesPerSecond = nil

		p.after(group, 350*time.Millisecond, func() {
			doneIdx := p.findProtoTaskIndex(taskID)
			if doneIdx < 0 || terminal(s.downloads[doneIdx].State) {
				return
			}
			s.downloads[doneIdx].State = "done"
			s.downloads[doneIdx].Stage = ""
			s.downloads[doneIdx].DownloadedBytes = total
			s.downloads[doneIdx].BytesPerSecond = nil
			s.downloads[doneIdx].FinishedAt = previewValue(p.timestamp())
			p.registerProtoDownloadedModel(assetName, format, total)
			p.notice = "已模拟下载 " + displayName + "。"
			s.enqueueStatusToast(displayName + " 已下载")
		})
	})
}

func (p *previewSession) registerProtoDownloadedModel(assetName, format string, total int64) {
	s := p.state
	for _, m := range s.registry.Models {
		if m.Name == assetName && m.Available {
			return
		}
	}
	s.registry.Models = append(s.registry.Models, core.ModelAsset{
		Name:      assetName,
		Format:    format,
		Available: true,
		SizeBytes: previewValue(total),
	})
}

func (p *previewSession) isProtoModelDownloaded(entry core.CatalogEntry) bool {
	targetName := protoModelAssetName(entry)
	targetTitle := protoModelTitle(entry)
	for _, m := range p.state.registry.Models {
		if m.Available && (m.Name == targetName || m.Name == targetTitle) {
			return true
		}
	}
	return false
}

func (p *previewSession) isProtoModelDownloading(entryID string) bool {
	prefix := "preview-proto-model-" + entryID + "-"
	for _, task := range p.state.downloads {
		if task.State != "downloading" {
			continue
		}
		if strings.HasPrefix(task.TaskId, prefix) {
			return true
		}
		if entryID == "model-qwen3-0.6b-q8" && task.Kind == "model" && !strings.HasPrefix(task.TaskId, "preview-proto-") {
			return true
		}
	}
	return false
}

func protoMirrorLabel(entry core.CatalogEntry) string {
	if entry.MirrorLabel != "" {
		return entry.MirrorLabel
	}
	return "ModelScope"
}

func protoModelSource(entry core.CatalogEntry, source string) (activeURL, activeLabel string) {
	if source == "modelscope" && entry.MirrorURL != "" {
		return entry.MirrorURL, protoMirrorLabel(entry)
	}
	return entry.URL, "HuggingFace"
}

func protoModelTitle(entry core.CatalogEntry) string {
	title := strings.TrimSuffix(entry.Title, "（GGUF）")
	title = strings.TrimSuffix(title, "（目录模型）")
	return title
}

func protoModelFormat(entry core.CatalogEntry) string {
	if entry.Format == "directory" {
		return "directory"
	}
	return "gguf"
}

func protoModelEngineLabel(entry core.CatalogEntry) string {
	switch entry.Family {
	case "mlx-lm":
		return "mlx-lm"
	case "llama-cpp":
		return "llama.cpp / LM Studio"
	default:
		return entry.Family
	}
}

func protoModelAssetName(entry core.CatalogEntry) string {
	if entry.Format == "directory" {
		return protoModelTitle(entry)
	}
	if u, err := url.Parse(entry.URL); err == nil && u.Path != "" {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return protoModelTitle(entry)
}

func protoCustomModelFilename(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "model.gguf"
	}
	if strings.Contains(raw, "/tree/") {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
		return base
	}
	return "model.gguf"
}

func protoModelRepoURL(rawURL string) string {
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
