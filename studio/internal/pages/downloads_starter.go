package pages

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"inferyard/studio/internal/core"
)

type starterOption struct {
	Engine                 string
	Backend                string
	ModelID                string
	ModelTitle             string
	Family                 string
	Size                   string
	EngineCore             string
	ModelFormat            string
	FitLevel               string
	Tier                   string
	RecommendedContext     int64
	EstimatedDownloadBytes int64
	EstimatedRAMBytes      int64
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
			EngineCore:             emptyText(rec.EngineCore),
			ModelFormat:            rec.ModelFormat,
			FitLevel:               rec.FitLevel,
			Tier:                   rec.Tier,
			RecommendedContext:     rec.RecommendedContext,
			EstimatedDownloadBytes: rec.EstimatedDownloadBytes,
			EstimatedRAMBytes:      rec.EstimatedRAMBytes,
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
			EngineCore:             emptyText(alt.EngineCore),
			ModelFormat:            alt.ModelFormat,
			FitLevel:               alt.FitLevel,
			Tier:                   alt.Tier,
			RecommendedContext:     alt.RecommendedContext,
			EstimatedDownloadBytes: alt.EstimatedDownloadBytes,
			EstimatedRAMBytes:      alt.EstimatedRAMBytes,
		})
	}
	for i := range opts {
		opts[i].Tier = resolveStarterTier(i, opts[i])
	}
	return opts
}

func resolveStarterTier(index int, opt starterOption) string {
	switch strings.TrimSpace(opt.Tier) {
	case "best", "balanced", "lightweight", "fallback":
		return strings.TrimSpace(opt.Tier)
	}
	if index == 0 {
		return "best"
	}
	if opt.Engine == "llama-cpp" {
		return "fallback"
	}
	if opt.EngineCore == "strata-poly" {
		return "lightweight"
	}
	return "balanced"
}

func starterTierLabel(tier string) string {
	if label := deviceLabel("tier_" + tier); label != "tier_"+tier {
		return label
	}
	return "备选方案"
}

func starterTierState(tier string) stateKind {
	switch tier {
	case "best":
		return stateActive
	case "balanced":
		return stateInfo
	default:
		return stateNeutral
	}
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

func starterContextShort(ctx int64) string {
	if ctx <= 0 {
		return "—"
	}
	if ctx%1024 == 0 {
		return fmt.Sprintf("%dK", ctx/1024)
	}
	return fmt.Sprintf("%d", ctx)
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

func (s *viewState) starterHardwareSummary(rec core.StarterRecommendation) string {
	var parts []string
	hasUnified := rec.GPU != nil && rec.GPU.UnifiedMemoryBytes != nil && *rec.GPU.UnifiedMemoryBytes > 0
	if gpuText := starterGPUChipText(rec); gpuText != "" {
		if hasUnified {
			parts = append(parts, gpuText+" 统一内存")
		} else {
			parts = append(parts, gpuText)
		}
	}
	snapshot, err := parseDeviceSnapshot(s.device.Snapshot)
	if err == nil && snapshot != nil {
		hw := snapshot.object("hardware")
		if !hasUnified {
			if mem := deviceBytes(hw["memory_total_bytes"]); mem != "" && mem != "缺测" {
				parts = append(parts, "内存 "+mem)
			}
		}
		_, hasModelsDiskKey := hw["models_disk_free_bytes"]
		modelsDiskMissing := hw.object("missing")["models_disk_free_bytes"] != nil
		if rec.DiskFreeBytes == nil && !hasModelsDiskKey && !modelsDiskMissing {
			if disk := deviceBytes(hw["disk_free_bytes"]); disk != "" && disk != "缺测" {
				parts = append(parts, "可用磁盘 "+disk)
			}
		}
	}
	if rec.DiskFreeBytes != nil && *rec.DiskFreeBytes > 0 {
		parts = append(parts, "可用磁盘 "+formatDownloadBytes(*rec.DiskFreeBytes))
	}
	return joinParts(parts)
}

func (s *viewState) isStarterEngineInstalled(opt starterOption) bool {
	if opt.Engine == "polystrata" {
		return s.isEngineFamilyInstalled("polystrata")
	}
	wantBackend := strings.ToLower(strings.TrimSpace(opt.Backend))
	for _, eng := range s.registry.Engines {
		isLlama := eng.Family == "llama-cpp" || (eng.Family == "" && strings.Contains(strings.ToLower(eng.Name), "llama"))
		if !isLlama {
			continue
		}
		if wantBackend == "" || wantBackend == "cpu" {
			return true
		}
		check, checked := s.checks.Engines[eng.Name]
		if checked && check.State == core.EngineCheckEligible {
			if strings.EqualFold(strings.TrimSpace(check.Mode), wantBackend) || check.Modes[wantBackend] {
				return true
			}
			if strings.TrimSpace(check.Mode) != "" || len(check.Modes) > 0 {
				continue
			}
		}
		if wantBackend == "cuda" {
			hay := strings.ToLower(eng.Name + " " + eng.Path)
			if strings.Contains(hay, "cuda") {
				return true
			}
			continue
		}
		return true
	}
	return false
}

func strataAssetConflictsWithSize(asset core.ModelAsset, wantSize string) bool {
	hay := strings.ToLower(asset.Name + " " + asset.Path + " " + strings.Join(asset.CoveredPaths, " "))
	if strings.Contains(hay, wantSize) {
		return false
	}
	for _, preset := range polystrataCatalogPresets {
		other := strings.ToLower(strings.TrimSpace(preset.size))
		if other != "" && other != wantSize && strings.Contains(hay, other) {
			return true
		}
	}
	return false
}

func (s *viewState) isStarterOptionInstalled(opt starterOption) bool {
	if opt.Engine == "polystrata" {
		if !s.isEngineFamilyInstalled("polystrata") {
			return false
		}
		fam := strings.ToLower(strings.TrimSpace(opt.Family))
		size := strings.ToLower(strings.TrimSpace(opt.Size))
		availableStrata := map[string]core.ModelAsset{}
		for _, m := range s.registry.Models {
			if !m.Available || m.Format != "strata_pack" {
				continue
			}
			if cleaned := cleanMatchPath(m.Path); cleaned != "" {
				availableStrata[cleaned] = m
			}
			hay := strings.ToLower(m.Name + " " + m.Path + " " + strings.Join(m.CoveredPaths, " "))
			if fam != "" && size != "" && strings.Contains(hay, fam) && strings.Contains(hay, size) {
				return true
			}
		}
		if len(availableStrata) == 0 {
			return false
		}
		seenCfg := map[string]bool{}
		for _, task := range s.downloads {
			if task.Kind != "polystrata" || task.State != "done" || task.ModelConfigPath == nil {
				continue
			}
			cfgPath := cleanMatchPath(*task.ModelConfigPath)
			if cfgPath == "" || seenCfg[cfgPath] {
				continue
			}
			seenCfg[cfgPath] = true
			asset, ok := availableStrata[cfgPath]
			if !ok || (size != "" && strataAssetConflictsWithSize(asset, size)) {
				continue
			}
			hay := strings.ToLower(task.DisplayName)
			if fam != "" && size != "" && strings.Contains(hay, fam) && strings.Contains(hay, size) {
				return true
			}
		}
		return false
	}
	return s.isStarterEngineInstalled(opt) && s.isStarterModelInstalled(opt)
}

func (s *viewState) isStarterModelInstalled(opt starterOption) bool {
	if opt.Engine == "polystrata" {
		return s.isStarterOptionInstalled(opt)
	}
	entry, ok := core.CatalogEntryByID(opt.ModelID)
	if !ok {
		return false
	}
	if s.isModelEntryDownloaded(entry) {
		return true
	}
	wantFile := ""
	if u, err := url.Parse(strings.TrimSpace(entry.URL)); err == nil {
		base := strings.TrimSpace(path.Base(u.Path))
		if strings.HasSuffix(strings.ToLower(base), ".gguf") {
			wantFile = strings.ToLower(base)
		}
	}
	if wantFile == "" {
		return false
	}
	for _, m := range s.registry.Models {
		if !m.Available {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(m.Name), wantFile) ||
			strings.EqualFold(filepath.Base(strings.TrimSpace(m.Path)), wantFile) {
			return true
		}
	}
	return false
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

func (s *viewState) hasUnresolvedDownloadTask() bool {
	for i, task := range s.downloads {
		if task.State != "failed" && task.State != "cancelled" {
			continue
		}
		if !s.downloadResolved(task, i) {
			return true
		}
	}
	return false
}

func (s *viewState) hasStarterContent(rec core.StarterRecommendation) bool {
	if s.device.Refreshing && !rec.FromSnapshot {
		return true
	}
	return len(starterOptions(rec)) > 0 || rec.Status == "blocked"
}

func (s *viewState) starterDefaultCollapsed() bool {
	assets := s.availableTaskAssets()
	return assets.engine || assets.model || s.downloadActive("engine") || s.downloadActive("model") || s.downloadActive("polystrata") || s.hasUnresolvedDownloadTask()
}

func (s *viewState) starterPanelExpanded() bool {
	st := s.ensureDownloadsUI()
	if st.starterExpandedOverride != nil {
		return *st.starterExpandedOverride
	}
	return !s.starterDefaultCollapsed()
}

func (s *viewState) setStarterPanelExpanded(expanded bool) {
	st := s.ensureDownloadsUI()
	v := expanded
	st.starterExpandedOverride = &v
}

// starterCardVisible reports whether the expanded recommendation panel with
// primary install CTA is currently visible on the Downloads page.
func (s *viewState) starterCardVisible() bool {
	rec := s.starterRecommendation()
	if !s.hasStarterContent(rec) {
		return false
	}
	return s.starterPanelExpanded()
}

func (s *viewState) openDownloadsWithStarterExpanded() {
	s.setStarterPanelExpanded(true)
	s.selected = "downloads"
}

func (s *viewState) startStarterOption(opt starterOption, navigateToDownloads bool) {
	st := s.ensureDownloadsUI()
	switch opt.Engine {
	case "polystrata":
		if navigateToDownloads {
			s.setStarterPanelExpanded(false)
			s.selected = "downloads"
		}
		s.acquirePolystrataSetup(polystrataRequestForOption(opt, st.polystrataModelSource))
	default:
		engineInstalled := s.isStarterEngineInstalled(opt)
		modelInstalled := s.isStarterModelInstalled(opt)
		if engineInstalled && modelInstalled {
			return
		}
		hostPlatform := s.downloadsHostPlatform()
		engineURL := starterLlamaCppEngineURL(hostPlatform, opt.Backend)
		modelURL := starterModelURLForOption(opt, st.modelSource)
		switch {
		case engineInstalled && modelURL != "":
			if navigateToDownloads {
				s.setStarterPanelExpanded(false)
				s.selected = "downloads"
			}
			s.acquireModelSource(modelURL, "")
		case modelInstalled && engineURL != "":
			if navigateToDownloads {
				s.setStarterPanelExpanded(false)
				s.selected = "downloads"
			}
			s.acquireEngine(engineURL, "")
		case !engineInstalled && !modelInstalled && engineURL != "" && modelURL != "":
			if navigateToDownloads {
				s.setStarterPanelExpanded(false)
				s.selected = "downloads"
			}
			s.acquireStarterBundle(engineURL, modelURL)
		default:
			if navigateToDownloads {
				s.selected = "downloads"
			}
			relID, buildID := starterLlamaCppDrawerTarget(s.engineCatalogRegistry(), hostPlatform, opt.Backend)
			st.openEngineDrawer("llama-cpp", relID, hostPlatform, buildID)
		}
	}
}

func (s *viewState) openPolystrataDrawerForCurrentHost() {
	fam, ok := s.engineCatalogRegistry().FamilyByID("polystrata")
	if !ok || len(fam.Releases) == 0 {
		return
	}
	st := s.ensureDownloadsUI()
	hostPlatform := s.downloadsHostPlatform()
	releases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(releases) == 0 {
		releases = fam.Releases
	}
	rel := resolveEngineRelease(st.selectedRelease[fam.ID], releases)
	build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)
	s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
}
