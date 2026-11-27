package pages

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"inferyard/studio/internal/core"
)

func normalizeMatchToken(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func strataAssetPrimaryText(asset core.ModelAsset) string {
	if len(asset.CoveredPaths) > 0 {
		return strings.ToLower(asset.Name + " " + strings.Join(asset.CoveredPaths, " "))
	}
	return strings.ToLower(asset.Name + " " + asset.Path)
}

func strataAssetMatches(hay, fam, size string) bool {
	if fam == "" || size == "" {
		return false
	}
	if strings.Contains(hay, fam) && strings.Contains(hay, size) {
		return true
	}
	normHay := normalizeMatchToken(hay)
	normFam := normalizeMatchToken(fam)
	normSize := normalizeMatchToken(size)
	return normFam != "" && normSize != "" && strings.Contains(normHay, normFam) && strings.Contains(normHay, normSize)
}

func strataAssetConflictsWithSize(asset core.ModelAsset, wantSize string) bool {
	hay := strataAssetPrimaryText(asset)
	normHay := normalizeMatchToken(hay)
	normWant := normalizeMatchToken(wantSize)
	if strings.Contains(hay, wantSize) || (normWant != "" && strings.Contains(normHay, normWant)) {
		return false
	}
	for _, preset := range polystrataCatalogPresets {
		other := strings.ToLower(strings.TrimSpace(preset.size))
		normOther := normalizeMatchToken(other)
		if other != "" && other != wantSize && (strings.Contains(hay, other) || (normOther != "" && strings.Contains(normHay, normOther))) {
			return true
		}
	}
	return false
}

func (s *viewState) isStarterEngineInstalled(opt starterOption) bool {
	if opt.Engine == "polystrata" {
		return s.isEngineFamilyInstalled("polystrata")
	}
	if opt.Engine == "mlx-lm" {
		return s.isEngineFamilyInstalled("mlx-lm")
	}
	wantBackend := strings.ToLower(strings.TrimSpace(opt.Backend))
	for _, eng := range s.registry.Engines {
		isLlama := eng.Family == "llama-cpp" || (eng.Family == "" && strings.Contains(strings.ToLower(eng.Name), "llama"))
		if !isLlama {
			continue
		}
		check, checked := s.checks.Engines[eng.Name]
		if checked && check.State == core.EngineCheckIneligible {
			continue
		}
		if wantBackend == "" || wantBackend == "cpu" {
			return true
		}
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
			if strataAssetMatches(strataAssetPrimaryText(m), fam, size) {
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
			if strataAssetMatches(strings.ToLower(task.DisplayName), fam, size) {
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
	if opt.Engine == "mlx-lm" {
		if u, err := url.Parse(strings.TrimSpace(entry.URL)); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 {
				repoToken := normalizeMatchToken(parts[1])
				for _, m := range s.registry.Models {
					if !m.Available {
						continue
					}
					if repoToken != "" && strings.Contains(normalizeMatchToken(m.Name+" "+m.Path), repoToken) {
						return true
					}
				}
			}
		}
		return false
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

func (s *viewState) starterManagedVenvTarget(familyID string) (core.EngineFamilyCatalog, core.EngineRelease, core.EnginePlatformBuild, bool) {
	fam, ok := s.engineCatalogRegistry().FamilyByID(familyID)
	if !ok {
		return core.EngineFamilyCatalog{}, core.EngineRelease{}, core.EnginePlatformBuild{}, false
	}
	hostPlatform := s.downloadsHostPlatform()
	releases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(releases) == 0 {
		return core.EngineFamilyCatalog{}, core.EngineRelease{}, core.EnginePlatformBuild{}, false
	}
	st := s.ensureDownloadsUI()
	rel := resolveEngineReleaseForInstall(st.selectedRelease[fam.ID], releases, hostPlatform)
	build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)
	if !build.CanManagedVenvInstallOn(hostPlatform) {
		return core.EngineFamilyCatalog{}, core.EngineRelease{}, core.EnginePlatformBuild{}, false
	}
	return fam, rel, build, true
}

func (s *viewState) startStarterMlxOption(opt starterOption, navigateToDownloads bool) {
	st := s.ensureDownloadsUI()
	engineInstalled := s.isStarterEngineInstalled(opt)
	modelInstalled := s.isStarterModelInstalled(opt)
	if engineInstalled && modelInstalled {
		return
	}
	if navigateToDownloads {
		s.setStarterPanelExpanded(false)
		s.selected = "library"
	}
	if !engineInstalled {
		if fam, rel, build, ok := s.starterManagedVenvTarget("mlx-lm"); ok {
			s.acquireManagedEngine(fam, rel, build)
		}
	}
	if !modelInstalled {
		if modelURL := starterModelURLForOption(opt, st.modelSource); modelURL != "" {
			s.acquireModelSource(modelURL, "")
		}
	}
}
