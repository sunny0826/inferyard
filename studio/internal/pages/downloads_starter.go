package pages

import (
	"fmt"
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

func starterSummaryMeta(rec core.StarterRecommendation, opt starterOption) string {
	title := strings.TrimSpace(opt.ModelTitle)
	if title == "" {
		title = opt.ModelID
	}
	parts := []string{starterEngineLabel(opt.Engine), title}
	if ctx := starterContextLabel(opt.RecommendedContext); ctx != "" {
		parts = append(parts, ctx)
	}
	if opt.EstimatedDownloadBytes > 0 {
		parts = append(parts, "预计下载 "+formatDownloadBytes(opt.EstimatedDownloadBytes))
	}
	if rec.GPU != nil && strings.TrimSpace(rec.GPU.Name) != "" {
		gpuText := strings.TrimSpace(rec.GPU.Name)
		if rec.GPU.VRAMBytes != nil && *rec.GPU.VRAMBytes > 0 {
			gpuText += " · " + formatDownloadBytes(*rec.GPU.VRAMBytes)
		} else if rec.GPU.UnifiedMemoryBytes != nil && *rec.GPU.UnifiedMemoryBytes > 0 {
			gpuText += " · " + formatDownloadBytes(*rec.GPU.UnifiedMemoryBytes)
		}
		parts = append(parts, gpuText)
	}
	return joinParts(parts)
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

func (s *viewState) downloadsStarterCard(c *ui.Context) {
	st := s.ensureDownloadsUI()
	assets := s.availableTaskAssets()
	if assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || s.downloadActive("polystrata") {
		return
	}
	rec := s.starterRecommendation()
	opts := starterOptions(rec)
	if len(opts) == 0 && rec.Status != "blocked" {
		return
	}
	if st.starterChoice < 0 || st.starterChoice >= len(opts) {
		st.starterChoice = 0
	}
	t := tokens(c)

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
				})
				button(c, "查看设备").OnClick(func() { s.selected = "device" })
			})
			return
		}

		chosen := opts[st.starterChoice]
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					heading(c, "推荐组合", 3)
					badge(c, starterEngineLabel(chosen.Engine), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
					if chosen.Backend != "" {
						badge(c, deviceLabel(chosen.Backend), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
					}
					if chosen.FitLevel != "" {
						badge(c, deviceLabel(chosen.FitLevel), stateSuccess, badgeOptions{Style: badgeOutline, Size: sizeSM})
					}
				})
				ui.Text(c, starterSummaryMeta(rec, chosen)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			switch chosen.Engine {
			case "polystrata":
				if button(c, "一键安装 PolyStrata").Disabled(s.busy).Clicked() {
					s.acquirePolystrataSetup(polystrataRequestForOption(chosen, st.polystrataModelSource))
				}
			default:
				engineURL := core.DefaultEngineURL()
				modelURL := starterModelURLForOption(chosen, st.modelSource)
				if engineURL != "" && modelURL != "" {
					if button(c, "下载推荐组合").Disabled(s.busy).Clicked() {
						s.acquireStarterBundle(engineURL, modelURL)
					}
				}
			}
		})

		if chosen.Engine == "polystrata" {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, "模型下载源").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				segmentedControl(c, "PolyStrata 模型源", &st.polystrataModelSource, []components.NativeSelectOption[string]{
					{Value: "auto", Label: "自动"},
					{Value: "modelscope", Label: "ModelScope"},
					{Value: "huggingface", Label: "HuggingFace"},
				})
			})
		}

		if len(opts) > 1 {
			componentFoldKeyed(c, "downloads-starter-alternatives", fmt.Sprintf("切换备选方案（%d）", len(opts)-1), func() {
				ui.Column(c).FillWidth().Gap(t.Density.Tight).Children(func() {
					for i, opt := range opts {
						if i == st.starterChoice {
							continue
						}
						idx, option := i, opt
						ui.Row(c.Key(fmt.Sprintf("starter-alt-%d", idx))).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
							ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
								ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
									ui.Text(c, option.ModelTitle).Bold()
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
							})
						})
					}
				})
			})
		}
	})
}
