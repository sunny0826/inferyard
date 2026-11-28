package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type EngineUpdateAction string

const (
	EngineUpdateActionNone     EngineUpdateAction = ""
	EngineUpdateActionDownload EngineUpdateAction = "download"
	EngineUpdateActionManaged  EngineUpdateAction = "managed"
	EngineUpdateActionVenv     EngineUpdateAction = "venv"
	EngineUpdateActionDrawer   EngineUpdateAction = "drawer"
	EngineUpdateActionExternal EngineUpdateAction = "external"
)

type EngineUpdateInfo struct {
	Engine           string             `json:"engine"`
	Family           string             `json:"family,omitempty"`
	InstalledVersion string             `json:"installed_version"`
	LatestVersion    string             `json:"latest_version"`
	ReleaseID        string             `json:"release_id,omitempty"`
	ReleaseVersion   string             `json:"release_version,omitempty"`
	ReleaseURL       string             `json:"release_url,omitempty"`
	DownloadURL      string             `json:"download_url,omitempty"`
	WheelURLs        []string           `json:"wheel_urls,omitempty"`
	AssetName        string             `json:"asset_name,omitempty"`
	AssetBytes       int64              `json:"asset_bytes,omitempty"`
	Distribution     string             `json:"distribution,omitempty"`
	Source           string             `json:"source,omitempty"`
	Action           EngineUpdateAction `json:"action,omitempty"`
	HintCommand      string             `json:"hint_command,omitempty"`
	LatestInstalled  bool               `json:"latest_installed,omitempty"`
	HasUpdate        bool               `json:"has_update"`
}

func (u EngineUpdateInfo) CanOneClickUpdate() bool {
	return u.HasUpdate && !u.LatestInstalled && (u.Action == EngineUpdateActionDownload ||
		u.Action == EngineUpdateActionManaged ||
		u.Action == EngineUpdateActionVenv)
}

func latestComparableCatalogVersion(family EngineFamilyCatalog, installed, dist, platform string) (latest, releaseID string, hasUpdate, comparable bool) {
	releases := catalogReleasesForUpdate(family, dist, platform)
	var bestComparable, bestReleaseID string
	for _, rel := range releases {
		ver := strings.TrimSpace(rel.Version)
		if ver == "" {
			continue
		}
		if installed != "" {
			if _, ok := compareEngineVersions(installed, ver); ok {
				if bestComparable == "" {
					bestComparable = ver
					bestReleaseID = rel.ID
				} else if cmp, ok2 := compareEngineVersions(bestComparable, ver); ok2 && cmp < 0 {
					bestComparable = ver
					bestReleaseID = rel.ID
				}
			}
		}
	}
	if bestComparable != "" {
		cmp, ok := compareEngineVersions(installed, bestComparable)
		return bestComparable, bestReleaseID, ok && cmp < 0, true
	}
	for _, rel := range releases {
		ver := strings.TrimSpace(rel.Version)
		if ver != "" {
			return ver, rel.ID, false, false
		}
	}
	return "", "", false, false
}

func installedVersionFromRegistryEntry(entry registryEntry) string {
	if entry.installation != "" {
		if ver := readInstallationVersion(entry.installation); ver != "" {
			return ver
		}
	} else if entry.path != "" {
		for _, dir := range uniqueCleanPaths([]string{filepath.Dir(entry.path), filepath.Dir(filepath.Dir(entry.path))}) {
			if receipt, ok := readManagedEngineReceipt(dir); ok && strings.TrimSpace(receipt.Version) != "" {
				return strings.TrimSpace(receipt.Version)
			}
		}
	}
	if entry.receipt != "" {
		if raw, err := os.ReadFile(entry.receipt); err == nil {
			var doc struct {
				EngineRelease string `json:"engine_release"`
			}
			if json.Unmarshal(raw, &doc) == nil && strings.TrimSpace(doc.EngineRelease) != "" {
				return strings.TrimSpace(doc.EngineRelease)
			}
		}
	}
	for _, candidate := range []string{entry.displayName, entry.engine.Name} {
		for _, part := range strings.Split(candidate, "·") {
			seg := strings.TrimSpace(part)
			if seg == "" {
				continue
			}
			if _, ok := parseSemverVersion(seg); ok {
				return seg
			}
			if _, ok := parseBuildVersion(seg); ok {
				return seg
			}
		}
	}
	return ""
}

func readInstallationVersion(instPath string) string {
	raw, err := installationBytes(instPath)
	if err != nil {
		return ""
	}
	var decl struct {
		Family           string `json:"family"`
		Version          string `json:"version"`
		WorkingDirectory string `json:"working_directory"`
	}
	if json.Unmarshal(raw, &decl) != nil {
		return ""
	}
	if v := strings.TrimSpace(decl.Version); v != "" {
		return v
	}
	roots := uniqueCleanPaths([]string{strings.TrimSpace(decl.WorkingDirectory), filepath.Dir(instPath)})
	if decl.Family == "polystrata" {
		for _, root := range roots {
			if ver := readPolystrataReceiptVersion(root); ver != "" {
				return ver
			}
		}
	}
	subpaths := polystrataBuildJSONSubpaths()
	for _, root := range roots {
		for _, sub := range subpaths {
			if ver := readBuildJSONVersion(filepath.Join(root, sub)); ver != "" {
				return ver
			}
		}
	}
	return ""
}

func venvUpgradeSupported(familyID, platform string) bool {
	switch familyID {
	case "mlx-lm":
		return true
	case "vllm":
		return platform != "darwin/arm64" && platform != "darwin/amd64"
	default:
		return false
	}
}

func catalogDirectArtifactFor(family EngineFamilyCatalog, releaseID, platform string, wantCUDA bool) (string, string, int64, string) {
	for _, rel := range family.Releases {
		if releaseID != "" && rel.ID != releaseID {
			continue
		}
		var fallback *EnginePlatformBuild
		for i := range rel.Builds {
			b := &rel.Builds[i]
			if !b.CanDirectDownloadOn(platform) && b.AcquireMode != EngineAcquireManagedSetup {
				continue
			}
			if b.Platform != platform && b.Platform != "any" {
				continue
			}
			isCUDA := strings.Contains(strings.ToLower(b.Variant), "cuda") || strings.Contains(strings.ToLower(b.Profile), "cuda")
			if wantCUDA == isCUDA && len(b.Artifacts) > 0 {
				a := b.Artifacts[0]
				return a.URL, a.Filename, b.TotalBytes(), rel.ReleaseURL
			}
			if fallback == nil && len(b.Artifacts) > 0 {
				fallback = b
			}
		}
		if fallback != nil {
			a := fallback.Artifacts[0]
			return a.URL, a.Filename, fallback.TotalBytes(), rel.ReleaseURL
		}
		return "", "", 0, rel.ReleaseURL
	}
	return "", "", 0, family.ReleasesURL
}

func countUpdatableEngineTargets(updates map[string]EngineUpdateInfo) int {
	if len(updates) == 0 {
		return 0
	}
	type groupState struct {
		hasUpdate  bool
		hasCurrent bool
	}
	groups := map[string]groupState{}
	for _, info := range updates {
		key := info.Family
		if key == "" {
			key = info.Engine
		}
		if info.Distribution != "" {
			key += ":" + info.Distribution
		}
		st := groups[key]
		if info.HasUpdate {
			st.hasUpdate = true
		} else if strings.TrimSpace(info.InstalledVersion) != "" && strings.TrimSpace(info.LatestVersion) != "" {
			if cmp, ok := compareEngineVersions(info.InstalledVersion, info.LatestVersion); ok && cmp >= 0 {
				st.hasCurrent = true
			}
		}
		groups[key] = st
	}
	count := 0
	for _, st := range groups {
		if st.hasUpdate && !st.hasCurrent {
			count++
		}
	}
	return count
}

// EngineUpdates compares locally checked/registered engine versions against cached upstream
// releases and the built-in engine catalog, reporting update availability and actionable modes.
func (s *Service) EngineUpdates() map[string]EngineUpdateInfo {
	out := map[string]EngineUpdateInfo{}
	checks := s.EngineChecks()
	reg := s.registry()

	entryByName := make(map[string]registryEntry, len(reg.engines))
	familyByName := make(map[string]string, len(reg.engines))
	versionByName := make(map[string]string, len(reg.engines))
	for _, entry := range reg.engines {
		entryByName[entry.engine.Name] = entry
		fam := normalizeCatalogFamilyID(entry.engine.Family, entry.family, entry.engine.Name, entry.path)
		if fam != "" {
			familyByName[entry.engine.Name] = fam
		}
		if ver := s.extractInstalledVersion(context.Background(), entry, fam, false); ver != "" {
			versionByName[entry.engine.Name] = ver
		}
	}

	var remote map[string]remoteEngineRelease
	platform := CurrentPlatform()
	if s != nil && s.engineUpdater != nil {
		remote = s.engineUpdater.remoteSnapshot()
		if strings.TrimSpace(s.engineUpdater.opts.Platform) != "" {
			platform = s.engineUpdater.opts.Platform
		}
	}

	catalog := DefaultEngineCatalog()
	evaluate := func(name string, elig EngineEligibility) {
		familyID := normalizeCatalogFamilyID(elig.Family, familyByName[name], name)
		if familyID == "" {
			return
		}
		family, ok := catalog.FamilyByID(familyID)
		if !ok {
			return
		}
		entry, hasEntry := entryByName[name]
		// Only trust elig.Release as an installed release when verification succeeded;
		// for ineligible checks, elig.Release holds the expected target rather than the binary's actual version.
		verifiedRelease := ""
		if elig.State == EngineCheckEligible {
			verifiedRelease = strings.TrimSpace(elig.Release)
		}
		candidates := []string{
			strings.TrimSpace(elig.Version),
			strings.TrimSpace(versionByName[name]),
			verifiedRelease,
		}

		dist := ""
		remoteKey := familyID
		if familyID == "llama-cpp" {
			dist = detectLlamaDistribution(entry, versionByName[name], verifiedRelease)
			remoteKey = "llama-cpp:" + dist
		}

		// 1. Evaluate catalog baseline.
		var installed, latest, releaseID string
		var hasUpdate, matched bool
		for _, cand := range candidates {
			if cand == "" {
				continue
			}
			if installed == "" {
				installed = cand
			}
			l, rID, upd, comparable := latestComparableCatalogVersion(family, cand, dist, platform)
			if comparable {
				installed, latest, releaseID, hasUpdate, matched = cand, l, rID, upd, true
				break
			}
		}
		if !matched {
			latest, releaseID, hasUpdate, _ = latestComparableCatalogVersion(family, installed, dist, platform)
		}

		source := "catalog"
		wantCUDA := hasEntry && isCUDAEngineEntry(entry)
		downloadURL, assetName, assetBytes, releaseURL := catalogDirectArtifactFor(family, releaseID, platform, wantCUDA)
		var wheelURLs []string
		if hasUpdate && familyID == "vllm" {
			wheelURLs = catalogManagedWheelURLs(family, releaseID, platform)
		}

		// 2. Overlay remote upstream release when available and comparable.
		if rem, ok := remote[remoteKey]; ok && strings.TrimSpace(rem.Version) != "" && !(familyID == "vllm" && platform == "darwin/arm64" && len(rem.WheelURLs) == 0) {
			remVer := strings.TrimSpace(rem.Version)
			for _, cand := range candidates {
				if cand == "" {
					continue
				}
				cmp, comparable := compareEngineVersions(cand, remVer)
				if !comparable {
					continue
				}
				preferRemote := !matched
				if !preferRemote && latest != "" {
					if rCmp, ok2 := compareEngineVersions(latest, remVer); ok2 && rCmp <= 0 {
						preferRemote = true
					}
				}
				if preferRemote {
					installed = cand
					latest = remVer
					hasUpdate = cmp < 0
					source = "remote"
					releaseID = matchingCatalogReleaseID(family, remVer)
					if strings.TrimSpace(rem.ReleaseURL) != "" {
						releaseURL = strings.TrimSpace(rem.ReleaseURL)
					}
					if strings.TrimSpace(rem.Distribution) != "" {
						dist = strings.TrimSpace(rem.Distribution)
					}
					if len(rem.WheelURLs) > 0 {
						wheelURLs = append([]string(nil), rem.WheelURLs...)
					}
					if familyID == "llama-cpp" {
						if asset, okAsset := matchLlamaReleaseAsset(rem.Assets, platform, wantCUDA); okAsset {
							downloadURL = asset.URL
							assetName = asset.Name
							assetBytes = asset.Bytes
						} else {
							downloadURL, assetName, assetBytes = "", "", 0
						}
					} else if familyID == "polystrata" && releaseID == "" && strings.TrimSpace(rem.DownloadURL) != "" {
						downloadURL = strings.TrimSpace(rem.DownloadURL)
						assetName = filepath.Base(downloadURL)
						assetBytes = 0
					}
				}
				break
			}
		}

		info := EngineUpdateInfo{
			Engine:           name,
			Family:           familyID,
			InstalledVersion: installed,
			LatestVersion:    latest,
			ReleaseID:        releaseID,
			ReleaseVersion:   latest,
			ReleaseURL:       releaseURL,
			DownloadURL:      downloadURL,
			WheelURLs:        wheelURLs,
			AssetName:        assetName,
			AssetBytes:       assetBytes,
			Distribution:     dist,
			Source:           source,
			HasUpdate:        hasUpdate,
		}
		info.Action, info.HintCommand = resolveEngineUpdateAction(info, entry, hasEntry, platform)
		out[name] = info
	}

	for name, elig := range checks.Engines {
		evaluate(name, elig)
	}
	for _, entry := range reg.engines {
		name := entry.engine.Name
		if name == "" {
			continue
		}
		if _, exists := out[name]; exists {
			continue
		}
		evaluate(name, EngineEligibility{Family: entry.engine.Family})
	}
	markSideBySideLatestInstalled(out)
	return out
}

func matchingCatalogReleaseID(family EngineFamilyCatalog, version string) string {
	want := strings.TrimSpace(version)
	if want == "" {
		return ""
	}
	for _, rel := range family.Releases {
		if strings.EqualFold(strings.TrimSpace(rel.Version), want) {
			return rel.ID
		}
	}
	return ""
}

func markSideBySideLatestInstalled(updates map[string]EngineUpdateInfo) {
	covered := map[string]bool{}
	groupKey := func(info EngineUpdateInfo) string {
		k := info.Family
		if k == "" {
			k = info.Engine
		}
		if info.Distribution != "" {
			k += ":" + info.Distribution
		}
		return k
	}
	for _, info := range updates {
		if !info.HasUpdate && strings.TrimSpace(info.InstalledVersion) != "" && strings.TrimSpace(info.LatestVersion) != "" {
			if cmp, ok := compareEngineVersions(info.InstalledVersion, info.LatestVersion); ok && cmp >= 0 {
				covered[groupKey(info)] = true
			}
		}
	}
	for name, info := range updates {
		// Only suppress side-by-side install actions when a newer copy already exists;
		// independent Python venvs upgrade in-place and must keep their own update action.
		if info.HasUpdate && info.Action != EngineUpdateActionVenv && covered[groupKey(info)] {
			info.LatestInstalled = true
			info.Action = EngineUpdateActionNone
			updates[name] = info
		}
	}
}

func resolveEngineUpdateAction(info EngineUpdateInfo, entry registryEntry, hasEntry bool, platform string) (EngineUpdateAction, string) {
	if !info.HasUpdate || info.LatestInstalled {
		return EngineUpdateActionNone, ""
	}
	switch info.Family {
	case "llama-cpp":
		if hasEntry && isHomebrewEnginePath(resolveEngineExecutablePath(entry)) {
			return EngineUpdateActionDrawer, "brew upgrade llama.cpp"
		}
		if strings.TrimSpace(info.DownloadURL) != "" {
			// On Windows, legacy llama-server requires a CLI-generated runtime-receipt.json
			// produced only when the archive matches a known catalog runtime profile.
			if platform == "windows/amd64" {
				catEntry, ok := CatalogEntryByURL(info.DownloadURL)
				if !ok || strings.TrimSpace(catEntry.Profile) == "" {
					return EngineUpdateActionDrawer, ""
				}
			}
			return EngineUpdateActionDownload, ""
		}
		return EngineUpdateActionDrawer, ""
	case "polystrata":
		if strings.TrimSpace(info.DownloadURL) != "" {
			return EngineUpdateActionManaged, ""
		}
		return EngineUpdateActionDrawer, ""
	case "mlx-lm", "vllm":
		if hasEntry && venvPythonPath(entry) != "" {
			if venvUpgradeSupported(info.Family, platform) || (info.Family == "vllm" && platform == "darwin/arm64" && len(info.WheelURLs) > 0) {
				return EngineUpdateActionVenv, ""
			}
		}
		return EngineUpdateActionDrawer, ""
	case "lmstudio":
		return EngineUpdateActionExternal, ""
	default:
		return EngineUpdateActionDrawer, ""
	}
}
