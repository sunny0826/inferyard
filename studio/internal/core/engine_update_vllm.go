package core

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

var pythonSiteVersionPattern = regexp.MustCompile(`(?i)python3\.(\d+)`)

func catalogManagedWheelURLs(family EngineFamilyCatalog, releaseID, platform string) []string {
	for _, rel := range family.Releases {
		if releaseID != "" && rel.ID != releaseID {
			continue
		}
		for _, b := range rel.Builds {
			if b.CanManagedVenvInstallOn(platform) && b.ManagedSpec != nil && len(b.ManagedSpec.WheelURLs) > 0 {
				return append([]string(nil), b.ManagedSpec.WheelURLs...)
			}
		}
	}
	return nil
}

func (s *Service) detectInstalledVenvPythonMinor(familyID string) string {
	reg := s.registry()
	for _, entry := range reg.engines {
		fam := normalizeCatalogFamilyID(entry.engine.Family, entry.family, entry.engine.Name, entry.path)
		if fam != familyID {
			continue
		}
		venvRoot := venvRootFromEntry(entry)
		if venvRoot == "" {
			continue
		}
		for _, siteDir := range venvSitePackagesDirs(venvRoot) {
			if m := pythonSiteVersionPattern.FindStringSubmatch(filepath.ToSlash(siteDir)); len(m) >= 2 && m[1] != "" {
				return m[1]
			}
		}
	}
	return "12"
}

func matchMacOSVLLMWheel(assets []remoteReleaseAsset, wantMetal bool, pyMinor string) (remoteReleaseAsset, bool) {
	if pyMinor == "" {
		pyMinor = "12"
	}
	prefix := "vllm-"
	if wantMetal {
		prefix = "vllm_metal-"
	}
	cpTag := "cp3" + pyMinor + "-"
	var exactCP, abi3 []remoteReleaseAsset
	for _, a := range assets {
		lower := strings.ToLower(strings.TrimSpace(a.Name))
		if !strings.HasPrefix(lower, prefix) || !strings.Contains(lower, "macosx_") || !strings.HasSuffix(lower, "_arm64.whl") {
			continue
		}
		switch {
		case strings.Contains(lower, cpTag):
			exactCP = append(exactCP, a)
		case strings.Contains(lower, "-abi3-"):
			abi3 = append(abi3, a)
		}
	}
	if len(exactCP) > 0 {
		return exactCP[0], true
	}
	if len(abi3) > 0 {
		return abi3[0], true
	}
	return remoteReleaseAsset{}, false
}

func (m *engineUpdateManager) fetchDarwinVLLMRelease(ctx context.Context, client *http.Client, key string) (remoteEngineRelease, error) {
	pyMinor := "12"
	if m.service != nil {
		pyMinor = m.service.detectInstalledVenvPythonMinor("vllm")
	}
	vllmRel, err := m.fetchGitHubRelease(ctx, client, key, "vllm", "GitHub", "vllm-project/vllm", false)
	if err != nil {
		return remoteEngineRelease{}, err
	}
	metalRel, err := m.fetchGitHubRelease(ctx, client, key+":metal", "vllm", "GitHub", "vllm-project/vllm-metal", false)
	if err != nil {
		return remoteEngineRelease{}, err
	}
	vVer := strings.TrimPrefix(strings.TrimSpace(vllmRel.Version), "v")
	mVer := strings.TrimPrefix(strings.TrimSpace(metalRel.Version), "v")
	if vVer == "" || !strings.EqualFold(vVer, mVer) {
		return remoteEngineRelease{}, fmt.Errorf("vllm (%s) and vllm-metal (%s) tag mismatch", vllmRel.Version, metalRel.Version)
	}
	coreWheel, okCore := matchMacOSVLLMWheel(vllmRel.Assets, false, pyMinor)
	metalWheel, okMetal := matchMacOSVLLMWheel(metalRel.Assets, true, pyMinor)
	if !okCore || !okMetal {
		return remoteEngineRelease{}, fmt.Errorf("missing macOS arm64 wheels for vllm %s (cp3%s)", vllmRel.Version, pyMinor)
	}
	vllmRel.Distribution = "GitHub"
	vllmRel.WheelURLs = []string{coreWheel.URL, metalWheel.URL}
	return vllmRel, nil
}
