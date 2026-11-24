package pages

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

type polystrataCatalogPreset struct {
	id            string
	family        string
	size          string
	title         string
	modelFormat   string
	downloadBytes int64
	context       int64
	metal         bool
	cuda          bool
	hip           bool
}

// Mirrors cli/src/inferyard/data/starter_catalog.json (lighter GGUF presets first
// so fallback defaults avoid starting multi-tens-of-GB packs without selection).
var polystrataCatalogPresets = []polystrataCatalogPreset{
	{
		id: "polystrata-qwen36-ud-q4-k-m", family: "qwen36", size: "UD-Q4_K_M",
		title: "Qwen3.6-35B-A3B (UD-Q4_K_M)", modelFormat: "gguf",
		downloadBytes: 22663387424, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-qwen36-ud-q3-k-m", family: "qwen36", size: "UD-Q3_K_M",
		title: "Qwen3.6-35B-A3B (UD-Q3_K_M)", modelFormat: "gguf",
		downloadBytes: 17104402720, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-qwen36-ud-q2-k-xl", family: "qwen36", size: "UD-Q2_K_XL",
		title: "Qwen3.6-35B-A3B (UD-Q2_K_XL)", modelFormat: "gguf",
		downloadBytes: 12574128416, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-lfm25-ud-q3-k-xl", family: "lfm25", size: "UD-Q3_K_XL",
		title: "LFM2.5-24B-A2B (UD-Q3_K_XL)", modelFormat: "gguf",
		downloadBytes: 10947100928, context: 32768, metal: true, cuda: true,
	},
	{
		id: "polystrata-lfm25-ud-q4-k-xl", family: "lfm25", size: "UD-Q4_K_XL",
		title: "LFM2.5-24B-A2B (UD-Q4_K_XL)", modelFormat: "gguf",
		downloadBytes: 14876901632, context: 32768, metal: true, cuda: true,
	},
	{
		id: "polystrata-gemma4-ud-q3-k-m", family: "gemma4", size: "UD-Q3_K_M",
		title: "Gemma 4 26B-A4B (UD-Q3_K_M)", modelFormat: "gguf",
		downloadBytes: 13190264704, context: 32768, cuda: true, hip: true,
	},
	{
		id: "polystrata-flint-ud-q3-k-xl", family: "flint", size: "UD-Q3_K_XL",
		title: "Flint 40B-A2.5B (UD-Q3_K_XL)", modelFormat: "gguf",
		downloadBytes: 18494977472, context: 32768, cuda: true,
	},
	{
		id: "polystrata-flint-ud-q4-k-xl", family: "flint", size: "UD-Q4_K_XL",
		title: "Flint 40B-A2.5B (UD-Q4_K_XL)", modelFormat: "gguf",
		downloadBytes: 25054688704, context: 32768, cuda: true,
	},
	{
		id: "polystrata-qwen38-coder-iq1-m", family: "coder", size: "IQ1_M",
		title: "Qwen3.8-Flash-Next Coder (IQ1_M)", modelFormat: "strata_pack",
		downloadBytes: 58400000000, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-qwen38-q2-0", family: "qwen", size: "Q2_0",
		title: "Qwen3.8-Flash-Next (Q2_0)", modelFormat: "strata_pack",
		downloadBytes: 66400000000, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-qwen38-iq3-xxs", family: "qwen", size: "IQ3_XXS",
		title: "Qwen3.8-Flash-Next (IQ3_XXS)", modelFormat: "strata_pack",
		downloadBytes: 75800000000, context: 32768, metal: true, cuda: true, hip: true,
	},
	{
		id: "polystrata-qwen38-iq3-s", family: "qwen", size: "IQ3_S",
		title: "Qwen3.8-Flash-Next (IQ3_S)", modelFormat: "strata_pack",
		downloadBytes: 83600000000, context: 32768, metal: true, cuda: true, hip: true,
	},
}

func (p polystrataCatalogPreset) supports(hostPlatform, backend string) bool {
	if strings.HasPrefix(hostPlatform, "darwin/") {
		return p.metal
	}
	switch backend {
	case "cuda":
		return p.cuda
	case "hip":
		return p.hip
	default:
		return p.cuda || p.hip
	}
}

func polystrataDrawerBackend(rec core.StarterRecommendation, hostPlatform string) string {
	if strings.HasPrefix(hostPlatform, "darwin/") {
		return "metal"
	}
	if rec.Engine == "polystrata" && (rec.Backend == "cuda" || rec.Backend == "hip") {
		return rec.Backend
	}
	if rec.GPU != nil {
		switch strings.ToLower(strings.TrimSpace(rec.GPU.Vendor)) {
		case "nvidia":
			return "cuda"
		case "amd":
			return "hip"
		}
	}
	if rec.Backend == "cuda" || rec.Backend == "hip" {
		return rec.Backend
	}
	return "auto"
}

func (s *viewState) polystrataDrawerOptions(hostPlatform string) []starterOption {
	rec := s.starterRecommendation()
	out := make([]starterOption, 0, len(polystrataCatalogPresets))
	seen := make(map[string]bool, len(polystrataCatalogPresets))

	for _, alt := range rec.PolystrataModels {
		if alt.Engine != "polystrata" || seen[alt.ModelID] {
			continue
		}
		seen[alt.ModelID] = true
		out = append(out, starterOption{
			Engine:                 alt.Engine,
			Backend:                alt.Backend,
			ModelID:                alt.ModelID,
			ModelTitle:             alt.ModelTitle,
			Family:                 emptyText(alt.Family),
			Size:                   emptyText(alt.Size),
			EngineCore:             emptyText(alt.EngineCore),
			ModelFormat:            alt.ModelFormat,
			FitLevel:               alt.FitLevel,
			Tier:                   alt.Tier,
			RecommendedContext:     alt.RecommendedContext,
			EstimatedDownloadBytes: alt.EstimatedDownloadBytes,
			EstimatedRAMBytes:      alt.EstimatedRAMBytes,
		})
	}

	for _, opt := range starterOptions(rec) {
		if opt.Engine != "polystrata" || seen[opt.ModelID] {
			continue
		}
		seen[opt.ModelID] = true
		out = append(out, opt)
	}
	if rec.FromSnapshot {
		return out
	}

	backend := polystrataDrawerBackend(rec, hostPlatform)
	for _, preset := range polystrataCatalogPresets {
		if seen[preset.id] || !preset.supports(hostPlatform, backend) {
			continue
		}
		seen[preset.id] = true
		out = append(out, starterOption{
			Engine:                 "polystrata",
			Backend:                backend,
			ModelID:                preset.id,
			ModelTitle:             preset.title,
			Family:                 preset.family,
			Size:                   preset.size,
			ModelFormat:            preset.modelFormat,
			RecommendedContext:     preset.context,
			EstimatedDownloadBytes: preset.downloadBytes,
		})
	}
	return out
}

func (s *viewState) resolvePolystrataDrawerOption(hostPlatform string) (starterOption, []starterOption) {
	st := s.ensureDownloadsUI()
	options := s.polystrataDrawerOptions(hostPlatform)
	if len(options) == 0 {
		return starterOption{}, nil
	}
	for _, opt := range options {
		if opt.ModelID == st.polystrataDrawerModelID {
			return opt, options
		}
	}
	st.polystrataDrawerModelID = options[0].ModelID
	return options[0], options
}

func (s *viewState) openPolystrataConfigDrawer(familyID, releaseID, platform, buildID string) {
	st := s.ensureDownloadsUI()
	hostPlatform := s.downloadsHostPlatform()
	if strings.TrimSpace(st.polystrataDrawerModelID) == "" {
		rec := s.starterRecommendation()
		opts := starterOptions(rec)
		if st.starterChoice >= 0 && st.starterChoice < len(opts) && opts[st.starterChoice].Engine == "polystrata" {
			st.polystrataDrawerModelID = opts[st.starterChoice].ModelID
		}
	}
	s.resolvePolystrataDrawerOption(hostPlatform)
	st.openEngineDrawer(familyID, releaseID, platform, buildID)
}

func polystrataDrawerOptionSummary(opt starterOption) string {
	parts := []string{starterOptionTitle(opt)}
	if opt.EstimatedDownloadBytes > 0 {
		parts = append(parts, formatDownloadBytes(opt.EstimatedDownloadBytes))
	}
	if ctx := starterContextLabel(opt.RecommendedContext); ctx != "" {
		parts = append(parts, ctx)
	}
	return joinParts(parts)
}

func polystrataCLICommand(build core.EnginePlatformBuild, req core.PolystrataSetupRequest) string {
	base := strings.TrimSpace(build.InstallCommand)
	if base == "" {
		return ""
	}
	parts := strings.Fields(base)
	if backend := strings.TrimSpace(req.Backend); backend != "" {
		replaced := false
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "--backend" {
				parts[i+1] = backend
				replaced = true
				break
			}
		}
		if !replaced {
			parts = append(parts, "--backend", backend)
		}
	}
	if family := strings.TrimSpace(req.Family); family != "" {
		parts = append(parts, "--family", family)
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		parts = append(parts, "--model", model)
	}
	if req.Context > 0 {
		parts = append(parts, "--context", strconv.Itoa(req.Context))
	}
	if source := strings.TrimSpace(req.ModelSource); source != "" {
		parts = append(parts, "--model-source", source)
	}
	return strings.Join(parts, " ")
}

func (s *viewState) polystrataDrawerSetupSection(c *ui.Context, st *downloadsUIState, hostPlatform string) (starterOption, bool) {
	t := tokens(c)
	rec := s.starterRecommendation()
	installed := s.isEngineFamilyInstalled("polystrata")
	sectionHeading := tr(c, "downloads.polystrata.drawer.heading_install")
	if installed {
		sectionHeading = tr(c, "downloads.polystrata.drawer.heading_add_model")
	}
	heading(c, sectionHeading, 3)

	selected, options := s.resolvePolystrataDrawerOption(hostPlatform)
	if len(options) == 0 {
		msg := strings.TrimSpace(deviceLabel(rec.Reason))
		if msg == "" {
			msg = tr(c, "downloads.polystrata.drawer.hw_unmet")
		}
		if notes := localizedDeviceNotes(rec.Notes); len(notes) > 0 {
			msg = trf(c, "downloads.polystrata.drawer.msg_with_notes", "message", msg, "notes", strings.Join(notes, tr(c, "downloads.punctuation.semicolon")))
		}
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Size:    bannerCompact,
			Message: msg,
		})
		return starterOption{}, false
	}

	if !rec.FromSnapshot {
		ui.Text(c, trf(c, "downloads.polystrata.drawer.snapshot_fallback", "reason", deviceLabel("device_snapshot_unavailable"))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	}

	chosenID := selected.ModelID
	ui.Box(c.Key("polystrata-drawer-model-select")).Children(func() {
		sel := components.Select(c, &chosenID)
		sel.Trigger.Label(tr(c, "downloads.polystrata.drawer.select_model")).Value(polystrataDrawerOptionSummary(selected)).Children(func() {
			ui.Text(c, polystrataDrawerOptionSummary(selected)).SingleLine()
		})
		sel.Popup(func(ui.Element) {
			components.ScrollView(c, components.ScrollViewOptions{}).Label(tr(c, "downloads.polystrata.drawer.model_list")).MaxHeight(240).Children(func() {
				for _, candidate := range options {
					itemLabel := polystrataDrawerOptionSummary(candidate)
					sel.Item(candidate.ModelID).Label(itemLabel).Children(func() {
						ui.Text(c, itemLabel).SingleLine()
					})
				}
			})
		})
		if sel.Trigger.Changed() {
			st.polystrataDrawerModelID = chosenID
			selected, options = s.resolvePolystrataDrawerOption(hostPlatform)
		}
	})

	applyMaterial(c, ui.Column(c.Key("polystrata-drawer-selection-summary")).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
		ui.Text(c, starterOptionTitle(selected)).Bold()
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			if selected.Backend != "" && selected.Backend != "auto" {
				badge(c, deviceLabel(selected.Backend), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
			}
			if selected.FitLevel != "" {
				badge(c, deviceLabel(selected.FitLevel), starterFitState(selected.FitLevel), badgeOptions{Style: badgeOutline, Size: sizeSM})
			}
			if ctx := starterContextLabel(selected.RecommendedContext); ctx != "" {
				badge(c, ctx, stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			}
			if selected.EstimatedDownloadBytes > 0 {
				badge(c, trf(c, "downloads.starter.est_download_badge", "size", formatDownloadBytes(selected.EstimatedDownloadBytes)), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			}
			if selected.EstimatedRAMBytes > 0 {
				badge(c, trf(c, "downloads.starter.est_ram_badge", "size", formatDownloadBytes(selected.EstimatedRAMBytes)), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			}
		})
	})

	ui.Row(c.Key("polystrata-drawer-source-row")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Text(c, tr(c, "downloads.starter.model_source")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		prev := st.polystrataModelSource
		segmentedControl(c, tr(c, "downloads.polystrata.drawer.source_control_label"), &st.polystrataModelSource, []components.NativeSelectOption[string]{
			{Value: "auto", Label: tr(c, "downloads.source.auto")},
			{Value: "modelscope", Label: "ModelScope"},
			{Value: "huggingface", Label: "HuggingFace"},
		})
		if st.polystrataModelSource != prev {
			st.setPolystrataModelSource(st.polystrataModelSource)
		}
	})

	ui.Text(c, tr(c, "downloads.polystrata.drawer.gguf_reuse_hint")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	return selected, true
}
