package pages

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

type starterOption struct {
	Engine                 string
	Backend                string
	ModelID                string
	ModelTitle             string
	Family                 string
	Size                   string
	ModelFormat            string
	FitLevel               string
	RecommendedContext     int64
	EstimatedDownloadBytes int64
}

func (s *viewState) starterRecommendation() core.StarterRecommendation {
	if rec, ok := core.ParseStarterRecommendation(s.device.Snapshot); ok {
		return rec
	}
	if s.service != nil {
		return s.service.RecommendedStarter()
	}
	return core.StaticFallbackStarterRecommendation()
}

func starterOptions(rec core.StarterRecommendation) []starterOption {
	var opts []starterOption
	if rec.Status != "blocked" && strings.TrimSpace(rec.Engine) != "" && strings.TrimSpace(rec.ModelID) != "" {
		opts = append(opts, starterOption{
			Engine:                 rec.Engine,
			Backend:                rec.Backend,
			ModelID:                rec.ModelID,
			ModelTitle:             rec.ModelTitle,
			Family:                 emptyText(rec.Family),
			Size:                   emptyText(rec.Size),
			ModelFormat:            rec.ModelFormat,
			FitLevel:               rec.FitLevel,
			RecommendedContext:     rec.RecommendedContext,
			EstimatedDownloadBytes: rec.EstimatedDownloadBytes,
		})
	}
	for _, alt := range rec.Alternatives {
		if strings.TrimSpace(alt.Engine) == "" || strings.TrimSpace(alt.ModelID) == "" {
			continue
		}
		opts = append(opts, starterOption{
			Engine:                 alt.Engine,
			Backend:                alt.Backend,
			ModelID:                alt.ModelID,
			ModelTitle:             alt.ModelTitle,
			Family:                 emptyText(alt.Family),
			Size:                   emptyText(alt.Size),
			ModelFormat:            alt.ModelFormat,
			FitLevel:               alt.FitLevel,
			RecommendedContext:     alt.RecommendedContext,
			EstimatedDownloadBytes: alt.EstimatedDownloadBytes,
		})
	}
	return opts
}

func (s *viewState) defaultPolystrataSetupRequest() core.PolystrataSetupRequest {
	st := s.ensureDownloadsUI()
	rec := s.starterRecommendation()
	opts := starterOptions(rec)
	if st.starterChoice >= 0 && st.starterChoice < len(opts) && opts[st.starterChoice].Engine == "polystrata" {
		return polystrataRequestForOption(opts[st.starterChoice], st.polystrataModelSource)
	}
	req := core.PolystrataSetupRequest{
		ModelSource: st.polystrataModelSource,
	}
	if rec.Status != "blocked" && rec.Engine == "polystrata" {
		req.Family = emptyText(rec.Family)
		req.Model = emptyText(rec.Size)
		if rec.RecommendedContext > 0 && rec.RecommendedContext <= 524288 {
			req.Context = int(rec.RecommendedContext)
		}
		if validPolystrataBackend(rec.Backend) {
			req.Backend = rec.Backend
		}
	}
	return req
}

func polystrataRequestForOption(opt starterOption, modelSource string) core.PolystrataSetupRequest {
	req := core.PolystrataSetupRequest{
		Family:      opt.Family,
		Model:       opt.Size,
		ModelSource: modelSource,
	}
	if opt.RecommendedContext > 0 && opt.RecommendedContext <= 524288 {
		req.Context = int(opt.RecommendedContext)
	}
	if validPolystrataBackend(opt.Backend) {
		req.Backend = opt.Backend
	}
	return req
}

func validPolystrataBackend(backend string) bool {
	switch backend {
	case "auto", "cuda", "hip", "metal":
		return true
	default:
		return false
	}
}

func starterEngineLabel(engine string) string {
	switch engine {
	case "polystrata":
		return "PolyStrata"
	case "llama-cpp":
		return "llama.cpp"
	default:
		return engine
	}
}

func starterContextLabel(ctx int64) string {
	if ctx <= 0 {
		return ""
	}
	if ctx%1024 == 0 {
		return fmt.Sprintf("上下文 %dK", ctx/1024)
	}
	return fmt.Sprintf("上下文 %d", ctx)
}

func starterFitState(fitLevel string) stateKind {
	switch fitLevel {
	case "fits":
		return stateSuccess
	case "tight":
		return stateWarning
	default:
		return stateNeutral
	}
}

func starterOptionTitle(opt starterOption) string {
	if title := strings.TrimSpace(opt.ModelTitle); title != "" {
		return title
	}
	return opt.ModelID
}

func starterGPUChipText(rec core.StarterRecommendation) string {
	if rec.GPU == nil || strings.TrimSpace(rec.GPU.Name) == "" {
		return ""
	}
	gpuText := strings.TrimSpace(rec.GPU.Name)
	if rec.GPU.VRAMBytes != nil && *rec.GPU.VRAMBytes > 0 {
		return gpuText + " · " + formatDownloadBytes(*rec.GPU.VRAMBytes)
	}
	if rec.GPU.UnifiedMemoryBytes != nil && *rec.GPU.UnifiedMemoryBytes > 0 {
		return gpuText + " · " + formatDownloadBytes(*rec.GPU.UnifiedMemoryBytes)
	}
	return gpuText
}

func starterCardNotes(notes []string, excludeXcodeCLT bool) []string {
	if len(notes) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(notes))
	for _, note := range notes {
		if excludeXcodeCLT && strings.TrimSpace(note) == "xcode_clt_missing" {
			continue
		}
		filtered = append(filtered, note)
	}
	return localizedDeviceNotes(filtered)
}

func starterSummaryMeta(rec core.StarterRecommendation, opt starterOption) string {
	parts := []string{starterEngineLabel(opt.Engine), starterOptionTitle(opt)}
	if ctx := starterContextLabel(opt.RecommendedContext); ctx != "" {
		parts = append(parts, ctx)
	}
	if opt.EstimatedDownloadBytes > 0 {
		parts = append(parts, "预计下载 "+formatDownloadBytes(opt.EstimatedDownloadBytes))
	}
	if gpuText := starterGPUChipText(rec); gpuText != "" {
		parts = append(parts, gpuText)
	}
	return joinParts(parts)
}

func starterLlamaCppEngineURL(hostPlatform, backend string) string {
	var id string
	switch hostPlatform {
	case "windows/amd64":
		if backend == "cuda" {
			id = "engine-windows-cuda-x64"
		} else {
			id = "engine-windows-x64"
		}
	case "darwin/arm64":
		id = "engine-macos-arm64"
	case "darwin/amd64":
		id = "engine-macos-x64"
	default:
		return ""
	}
	if entry, ok := core.CatalogEntryByID(id); ok && entry.Downloadable() {
		return entry.URL
	}
	return ""
}

func starterLlamaCppDrawerTarget(reg *core.EngineCatalogRegistry, hostPlatform, backend string) (string, string) {
	if reg == nil {
		return "", ""
	}
	fam, ok := reg.FamilyByID("llama-cpp")
	if !ok {
		return "", ""
	}
	releases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(releases) == 0 {
		return "", ""
	}
	wantBackend := strings.ToLower(strings.TrimSpace(backend))
	if wantBackend != "" {
		for _, rel := range releases {
			for _, b := range rel.BuildsForPlatform(hostPlatform) {
				if strings.Contains(strings.ToLower(b.Variant), wantBackend) {
					return rel.ID, b.ID
				}
			}
		}
	}
	firstBuilds := releases[0].BuildsForPlatform(hostPlatform)
	if len(firstBuilds) > 0 {
		return releases[0].ID, firstBuilds[0].ID
	}
	return releases[0].ID, ""
}

func starterModelURLForOption(opt starterOption, modelSource string) string {
	if entry, ok := core.CatalogEntryByID(opt.ModelID); ok {
		if modelSource == "modelscope" && entry.MirrorURL != "" {
			return entry.MirrorURL
		}
		if entry.URL != "" {
			return entry.URL
		}
	}
	modelURL := core.DefaultModelURL()
	if modelSource == "modelscope" {
		if entry, ok := core.CatalogEntryByURL(modelURL); ok && entry.MirrorURL != "" {
			return entry.MirrorURL
		}
	}
	return modelURL
}

func (s *viewState) hasUnresolvedPolystrataTask() bool {
	for i, task := range s.downloads {
		if task.Kind != "polystrata" || (task.State != "failed" && task.State != "cancelled") {
			continue
		}
		resolved := false
		for j := 0; j < i; j++ {
			newer := s.downloads[j]
			if newer.Kind == "polystrata" && (newer.State == "downloading" || newer.State == "done") {
				resolved = true
				break
			}
		}
		if !resolved {
			return true
		}
	}
	return false
}

func (s *viewState) starterCardVisible() bool {
	assets := s.availableTaskAssets()
	if assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || s.downloadActive("polystrata") || s.hasUnresolvedPolystrataTask() {
		return false
	}
	rec := s.starterRecommendation()
	if s.device.Refreshing && !rec.FromSnapshot {
		return true
	}
	return len(starterOptions(rec)) > 0 || rec.Status == "blocked"
}

func (s *viewState) downloadsStarterCard(c *ui.Context) {
	st := s.ensureDownloadsUI()
	assets := s.availableTaskAssets()
	if assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || s.downloadActive("polystrata") || s.hasUnresolvedPolystrataTask() {
		return
	}
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

	applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		if rec.Status == "blocked" && len(opts) == 0 {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						heading(c, "推荐组合", 3)
						badge(c, "暂不可用", stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
					})
					reason := deviceLabel(rec.Reason)
					if reason == "" {
						reason = "当前硬件不满足默认推荐条件。"
					}
					ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if notes := localizedDeviceNotes(rec.Notes); len(notes) > 0 {
						ui.Text(c, strings.Join(notes, "；")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
				})
				button(c, "查看设备").OnClick(func() { s.selected = "device" })
			})
			return
		}

		chosen := opts[st.starterChoice]
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					heading(c, "推荐组合", 3)
					badge(c, starterEngineLabel(chosen.Engine), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
					if chosen.Backend != "" {
						badge(c, deviceLabel(chosen.Backend), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
					}
					if chosen.FitLevel != "" {
						badge(c, deviceLabel(chosen.FitLevel), starterFitState(chosen.FitLevel), badgeOptions{Style: badgeOutline, Size: sizeSM})
					}
				})
				ui.Text(c, starterOptionTitle(chosen)).Bold()
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					if ctx := starterContextLabel(chosen.RecommendedContext); ctx != "" {
						badge(c, ctx, stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
					}
					if chosen.EstimatedDownloadBytes > 0 {
						badge(c, "预计下载 "+formatDownloadBytes(chosen.EstimatedDownloadBytes), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
					}
					if gpuText := starterGPUChipText(rec); gpuText != "" {
						badge(c, gpuText, stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
					}
				})
				var summaryNotes []string
				if st.starterChoice == 0 && rec.Reason != "" && rec.Reason != "polystrata_hardware_fit" {
					summaryNotes = append(summaryNotes, deviceLabel(rec.Reason))
				}
				summaryNotes = append(summaryNotes, starterCardNotes(rec.Notes, chosen.Engine == "polystrata")...)
				if len(summaryNotes) > 0 {
					ui.Text(c, strings.Join(summaryNotes, "；")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
			})
			switch chosen.Engine {
			case "polystrata":
				if primaryButton(c, "一键安装 PolyStrata").Disabled(s.busy).Clicked() {
					s.acquirePolystrataSetup(polystrataRequestForOption(chosen, st.polystrataModelSource))
				}
			default:
				hostPlatform := s.downloadsHostPlatform()
				engineURL := starterLlamaCppEngineURL(hostPlatform, chosen.Backend)
				modelURL := starterModelURLForOption(chosen, st.modelSource)
				switch {
				case engineURL != "" && modelURL != "":
					if primaryButton(c, "下载推荐组合").Disabled(s.busy).Clicked() {
						s.acquireStarterBundle(engineURL, modelURL)
					}
				default:
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						primaryButton(c, "查看引擎获取方式").OnClick(func() {
							relID, buildID := starterLlamaCppDrawerTarget(s.engineCatalogRegistry(), hostPlatform, chosen.Backend)
							st.openEngineDrawer("llama-cpp", relID, hostPlatform, buildID)
						})
						if modelURL != "" {
							outlineButton(c, "下载推荐模型").Disabled(s.busy).OnClick(func() {
								s.acquireModelSource(modelURL, "")
							})
						}
					})
				}
			}
		})

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

		ui.Row(c.Key("starter-model-source-row")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, "模型下载源").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			if chosen.Engine == "polystrata" {
				ui.Box(c.Key("starter-source-polystrata")).Children(func() {
					prev := st.polystrataModelSource
					segmentedControl(c, "PolyStrata 模型源", &st.polystrataModelSource, []components.NativeSelectOption[string]{
						{Value: "auto", Label: "自动"},
						{Value: "modelscope", Label: "ModelScope"},
						{Value: "huggingface", Label: "HuggingFace"},
					})
					if st.polystrataModelSource != prev && (st.polystrataModelSource == "modelscope" || st.polystrataModelSource == "huggingface") {
						st.modelSource = st.polystrataModelSource
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
						st.polystrataModelSource = st.modelSource
					}
				})
			}
		})

		if len(opts) > 1 {
			ui.Column(c.Key("downloads-starter-alternatives")).FillWidth().Children(func() {
				components.Collapsible(c, fmt.Sprintf("切换备选方案（%d）", len(opts)-1), &st.starterAltOpen, components.CollapsibleOptions{}, func() {
					ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
						for i, opt := range opts {
							if i == st.starterChoice {
								continue
							}
							idx, option := i, opt
							ui.Row(c.Key(fmt.Sprintf("starter-alt-%d", idx))).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
								ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
									ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
										ui.Text(c, starterOptionTitle(option)).Bold()
										badge(c, starterEngineLabel(option.Engine), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
										if option.Backend != "" {
											badge(c, deviceLabel(option.Backend), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
										}
									})
									meta := []string{}
									if ctx := starterContextLabel(option.RecommendedContext); ctx != "" {
										meta = append(meta, ctx)
									}
									if option.EstimatedDownloadBytes > 0 {
										meta = append(meta, "预计下载 "+formatDownloadBytes(option.EstimatedDownloadBytes))
									}
									if len(meta) > 0 {
										ui.Text(c, joinParts(meta)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
									}
								})
								outlineButton(c, "使用此方案").Label("使用此方案 · " + option.ModelID).OnClick(func() {
									st.starterChoice = idx
									st.starterAltOpen = false
									if option.Engine == "polystrata" {
										st.polystrataDrawerModelID = option.ModelID
									}
								})
							})
						}
					})
				})
			})
		}
	})
}
