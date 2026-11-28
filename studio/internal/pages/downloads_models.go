package pages

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
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
				ui.Row(c).Grow(1).MinWidth(0).Children(func() { heading(c, tr(c, "library.section.catalog_models"), 2) })
				prev := st.modelSource
				segmentedControl(c, tr(c, "downloads.models.source_label"), &st.modelSource, []components.NativeSelectOption[string]{
					{Value: "huggingface", Label: "HuggingFace"},
					{Value: "modelscope", Label: "ModelScope"},
				})
				if st.modelSource != prev {
					st.setDirectModelSource(st.modelSource)
				}
			})
			if len(supported) == 0 {
				ui.Text(c, tr(c, "downloads.models.none_preset")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				return
			}
			ui.Column(c).FillWidth().Children(func() {
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				for i, entry := range supported {
					if i > 0 {
						ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
					}
					s.modelCatalogRow(c, st, entry)
				}
			})
		})

		if len(otherPlatform) > 0 {
			componentFoldKeyed(c, "downloads-other-platform-models", trf(c, "downloads.models.other_platform_fold", "count", strconv.Itoa(len(otherPlatform))), func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, entry := range otherPlatform {
						if i > 0 {
							ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
						}
						s.otherPlatformModelRow(c, st, entry)
					}
				})
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
	sizeText := tr(c, "downloads.models.dir_weights")
	if entry.Bytes > 0 {
		sizeText = catalogHumanSize(entry.Bytes)
	}
	title := modelCatalogTitle(entry)

	ui.Row(c.Key("downloads-model-"+entry.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		modelIconBox(c, title+" "+entry.ID+" "+entry.URL, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, title).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, core.ModelFormatLabel(modelCatalogFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				if downloaded {
					inlineBadge(c, tr(c, "downloads.metric.downloaded"), stateSuccess, badgeOptions{Style: badgeSoft, Size: sizeSM})
				} else if downloading {
					inlineBadge(c, tr(c, "downloads.stage.downloading"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				}
			})
			meta := joinParts([]string{modelCatalogEngineLabel(entry), sizeText, activeSourceLabel})
			ui.Text(c, meta).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
		})
		studioTooltipGroup(c, ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(t.Density.Row), func() {
			outlineIconButton(c, reicon.Info, tr(c, "downloads.models.details_urls")).Label(trf(c, "downloads.models.details_label", "id", entry.ID)).OnClick(func() {
				st.openModelDrawer(entry.ID)
			})
			if downloading {
				outlineIconButton(c, reicon.Download, tr(c, "downloads.action.downloading")).Label(trf(c, "downloads.models.download_label", "id", entry.ID)).Disabled(true)
			} else {
				s.modelDownloadIconButton(c, tr(c, "downloads.action.download")).Label(trf(c, "downloads.models.download_label", "id", entry.ID)).Disabled(s.busy).OnClick(func() {
					s.acquireModelSource(activeURL, "")
				})
			}
		})
	})
}

func (s *viewState) modelDownloadIconButton(c *ui.Context, label string) ui.Element {
	if s.downloadPrimaryKind() == "model" {
		return primaryIconButton(c, reicon.Download, label)
	}
	return outlineIconButton(c, reicon.Download, label)
}

func (s *viewState) otherPlatformModelRow(c *ui.Context, st *downloadsUIState, entry core.CatalogEntry) {
	t := tokens(c)
	title := modelCatalogTitle(entry)
	ui.Row(c.Key("downloads-other-model-"+entry.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Stack, t.Density.Tight).Children(func() {
		modelIconBox(c, title+" "+entry.ID+" "+entry.URL, 36, t.Radius.Control)
		ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, title).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
				inlineBadge(c, core.ModelFormatLabel(modelCatalogFormat(entry)), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			})
			ui.Text(c, trf(c, "downloads.platform.supported_line", "platforms", core.PlatformLabel(entry.Platform))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
			outlineIconButton(c, reicon.Info, tr(c, "downloads.models.details_urls")).Label(trf(c, "downloads.models.details_label", "id", entry.ID)).OnClick(func() {
				st.openModelDrawer(entry.ID)
			})
		})
	})
}

func (s *viewState) customModelURLFold(c *ui.Context, st *downloadsUIState) {
	t := tokens(c)
	resState := st.customModelResolve

	componentFoldKeyed(c, "downloads-custom-model-url", tr(c, "downloads.models.custom_url_fold"), func() {
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
			field(c, tr(c, "downloads.models.custom_url_label"), &st.customModelURL).
				Description(tr(c, "downloads.models.custom_url_desc")).
				Error(customModelURLError(st.customModelURL))
			componentFoldKeyed(c, "downloads-custom-model-auth", tr(c, "downloads.models.auth_fold"), func() {
				field(c, tr(c, "downloads.models.token_env_label"), &s.tokenEnv).
					Description(tr(c, "downloads.models.token_env_desc")).
					Error(tokenNameError(s.tokenEnv))
			})

			tokenOK := tokenNameError(s.tokenEnv) == ""
			urlValid := trimmed != "" && customModelURLError(trimmed) == "" && tokenOK
			canResolve := !s.busy && !resState.resolving && trimmed != "" && core.IsResolvableModelURL(trimmed) && tokenOK
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				outlineButton(c, tr(c, "downloads.models.resolve_url")).Disabled(!canResolve).OnClick(func() {
					s.triggerModelResolve(resState, trimmed, tokenEnv)
				})
				primaryButton(c, tr(c, "downloads.models.download_button")).Label(tr(c, "downloads.models.download_custom_label")).Disabled(s.busy || !urlValid).OnClick(func() {
					source := trimmed
					if resState.resolved != nil && resState.resolved.PinnedURL != "" {
						source = resState.resolved.PinnedURL
					}
					s.acquireModelSource(source, s.tokenEnv)
				})
			})

			switch {
			case resState.resolving:
				ui.Text(c, tr(c, "downloads.models.resolving")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			case resState.errText != "":
				ui.Text(c, resState.errText).FontSize(t.Type.Caption).TextColor(stateColor(c, stateDanger))
			case resState.resolved != nil:
				sizeText := tr(c, "downloads.size.unknown")
				if resState.resolved.Bytes != nil && *resState.resolved.Bytes > 0 {
					sizeText = catalogHumanSize(*resState.resolved.Bytes)
				}
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
					ui.Text(c, trf(c, "downloads.models.resolved_revision", "revision", resState.resolved.Revision)).Selectable().FontSize(t.Type.Caption)
					ui.Text(c, trf(c, "downloads.models.resolved_file", "filename", resState.resolved.Filename, "size", sizeText)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					ui.Text(c, trf(c, "downloads.models.resolved_pinned_url", "url", resState.resolved.PinnedURL)).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					outlineIconButton(c, reicon.Copy, tr(c, "downloads.models.copy_pinned_url")).OnClick(func() {
						s.copyDownloadText(c, tr(c, "downloads.copy_subject.pinned_url"), resState.resolved.PinnedURL)
					})
				})
			}
		})
	})
}

func (s *viewState) downloadsModelDrawer(c *ui.Context) {
	st := s.ensureDownloadsUI()
	c.Key("downloads-model-detail-drawer")
	drawerTitle := tr(c, "downloads.model_drawer.title")
	if !st.modelDrawerOpen {
		drawer(c, &st.modelDrawerOpen, drawerOptions{Label: drawerTitle, Title: drawerTitle}, nil)
		return
	}
	entry, ok := core.CatalogEntryByID(st.modelDrawerID)
	if !ok || entry.Kind != core.CatalogModel {
		st.modelDrawerOpen = false
		drawer(c, &st.modelDrawerOpen, drawerOptions{Label: drawerTitle, Title: drawerTitle}, nil)
		return
	}

	hostPlatform := s.downloadsHostPlatform()
	activeURL, activeSourceLabel := modelCatalogSource(entry, st.modelSource)
	usingMirror := entry.MirrorURL != "" && st.modelSource == "modelscope"
	versionText := entry.Revision
	if usingMirror || versionText == "" {
		versionText = tr(c, "downloads.model_drawer.auto_resolve")
	}

	drawer(c, &st.modelDrawerOpen, drawerOptions{
		Label: drawerTitle,
		Title: modelCatalogTitle(entry),
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("downloads-model-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				if entry.MirrorURL != "" {
					prev := st.modelSource
					segmentedControl(c, tr(c, "downloads.model_drawer.source_label"), &st.modelSource, []components.NativeSelectOption[string]{
						{Value: "huggingface", Label: "HuggingFace"},
						{Value: "modelscope", Label: modelCatalogMirrorLabel(entry)},
					})
					if st.modelSource != prev {
						st.setDirectModelSource(st.modelSource)
					}
				}
				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					keyValueRow(c, tr(c, "downloads.model_drawer.field.format"), core.ModelFormatLabel(modelCatalogFormat(entry)), false)
					keyValueRow(c, tr(c, "downloads.model_drawer.field.engines"), modelCatalogEngineLabel(entry), false)
					keyValueRow(c, tr(c, "downloads.model_drawer.field.platform"), core.PlatformLabel(entry.Platform), false)
					if entry.Bytes > 0 {
						keyValueRow(c, tr(c, "downloads.model_drawer.field.file_size"), catalogHumanSize(entry.Bytes), true)
					}
					keyValueRow(c, tr(c, "downloads.model_drawer.field.version"), versionText, false)
				})
				if entry.Platform != "any" && entry.Platform != hostPlatform {
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Message: trf(c, "downloads.model_drawer.platform_mismatch", "platform", core.PlatformLabel(entry.Platform)),
					})
				}

				heading(c, tr(c, "downloads.engine_drawer.section.urls"), 3)
				applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, activeSourceLabel).Bold()
						if entry.Bytes > 0 {
							badge(c, catalogHumanSize(entry.Bytes), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
						}
					})
					ui.Text(c, activeURL).Selectable().FontSize(t.Type.Caption)
					if !usingMirror && entry.SHA256 != "" {
						ui.Text(c, trf(c, "downloads.engine_drawer.sha256_value", "sha256", entry.SHA256)).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
					studioTooltipGroup(c, ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight), func() {
						outlineIconButton(c, reicon.Copy, tr(c, "downloads.action.copy_url")).Label(trf(c, "downloads.model_drawer.copy_url_label", "id", entry.ID)).OnClick(func() {
							s.copyDownloadText(c, tr(c, "downloads.engine_drawer.section.urls"), activeURL)
						})
						if !usingMirror && entry.SHA256 != "" {
							outlineIconButton(c, reicon.Copy, tr(c, "downloads.action.copy_sha256")).Label(trf(c, "downloads.model_drawer.copy_sha256_label", "id", entry.ID)).OnClick(func() {
								s.copyDownloadText(c, "SHA-256", entry.SHA256)
							})
						}
						outlineIconButton(c, reicon.ExternalLink, tr(c, "downloads.action.open_link")).Label(trf(c, "downloads.model_drawer.open_link_label", "id", entry.ID)).OnClick(func() {
							s.openDownloadURL(c, modelCatalogTitle(entry), activeURL)
						})
					})
				})

				studioTooltipGroup(c, ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row), func() {
					if s.isModelEntryDownloading(entry) {
						outlineIconButton(c, reicon.Download, tr(c, "downloads.action.downloading")).Label(trf(c, "downloads.model_drawer.download_this_label", "id", entry.ID)).Disabled(true)
					} else {
						primaryIconButton(c, reicon.Download, tr(c, "downloads.model_drawer.download_this")).Label(trf(c, "downloads.model_drawer.download_this_label", "id", entry.ID)).Disabled(s.busy).OnClick(func() {
							s.acquireModelSource(activeURL, "")
						})
					}
					if repoURL := modelCatalogRepoURL(activeURL); repoURL != "" && repoURL != activeURL {
						outlineIconButton(c, reicon.ExternalLink, tr(c, "downloads.model_drawer.open_repo")).OnClick(func() {
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
		if task.State == "downloading" && modelTaskMatchesEntry(task, entry) && s.downloadTaskInCurrentWorkspace(task) {
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
	title := entry.Title
	for _, key := range []string{"downloads.model.title_suffix.gguf", "downloads.model.title_suffix.directory"} {
		title = strings.TrimSuffix(title, l10n.TR(key))
		title = strings.TrimSuffix(title, l10n.T(l10n.LanguageZhHans, key))
		title = strings.TrimSuffix(title, l10n.T(l10n.LanguageEn, key))
	}
	title = strings.TrimSuffix(title, " (Directory Model)")
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
