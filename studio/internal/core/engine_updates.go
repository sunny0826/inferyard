package core

import (
	"regexp"
	"strconv"
	"strings"
)

type EngineUpdateInfo struct {
	Engine           string `json:"engine"`
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	HasUpdate        bool   `json:"has_update"`
}

var (
	buildVersionPattern  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])b(\d{3,})(?:[^0-9]|$)`)
	semverVersionPattern = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)(?:\.(\d+))?(?:-([0-9a-zA-Z.-]+))?(?:\+[0-9a-zA-Z.-]+)?$`)
)

type parsedSemver struct {
	major, minor, patch int
	pre                 string
}

func parseBuildVersion(raw string) (int, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	// Do not misclassify semver strings as build numbers.
	if _, ok := parseSemverVersion(s); ok {
		return 0, false
	}
	m := buildVersionPattern.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func parseSemverVersion(raw string) (parsedSemver, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return parsedSemver{}, false
	}
	m := semverVersionPattern.FindStringSubmatch(s)
	if len(m) < 3 {
		return parsedSemver{}, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	patch := 0
	var err3 error
	if m[3] != "" {
		patch, err3 = strconv.Atoi(m[3])
	}
	if err1 != nil || err2 != nil || err3 != nil || major < 0 || minor < 0 || patch < 0 {
		return parsedSemver{}, false
	}
	return parsedSemver{major: major, minor: minor, patch: patch, pre: m[4]}, true
}

// compareEngineVersions compares two engine version strings.
// Returns (-1, true) if installed < latest, (0, true) if equal, (1, true) if installed > latest,
// and (0, false) if either version cannot be parsed or the two versions use incompatible schemes.
func compareEngineVersions(installed, latest string) (int, bool) {
	if a, okA := parseSemverVersion(installed); okA {
		b, okB := parseSemverVersion(latest)
		if !okB {
			return 0, false
		}
		return compareEngineSemver(a, b)
	}
	if a, okA := parseBuildVersion(installed); okA {
		b, okB := parseBuildVersion(latest)
		if !okB {
			return 0, false
		}
		switch {
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		default:
			return 0, true
		}
	}
	return 0, false
}

func compareEngineSemver(a, b parsedSemver) (int, bool) {
	if a.major != b.major {
		if a.major < b.major {
			return -1, true
		}
		return 1, true
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1, true
		}
		return 1, true
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1, true
		}
		return 1, true
	}
	if a.pre == b.pre {
		return 0, true
	}
	if a.pre == "" {
		return 1, true
	}
	if b.pre == "" {
		return -1, true
	}
	return comparePrerelease(a.pre, b.pre)
}

func comparePrerelease(a, b string) (int, bool) {
	partsA := strings.Split(strings.ToLower(a), ".")
	partsB := strings.Split(strings.ToLower(b), ".")
	n := len(partsA)
	if len(partsB) < n {
		n = len(partsB)
	}
	for i := 0; i < n; i++ {
		if partsA[i] == partsB[i] {
			continue
		}
		numA, errA := strconv.Atoi(partsA[i])
		numB, errB := strconv.Atoi(partsB[i])
		if errA == nil && errB == nil {
			if numA < numB {
				return -1, true
			}
			return 1, true
		}
		// Different non-numeric prerelease labels (e.g. "alpha" vs "poly") are not safely ordered.
		return 0, false
	}
	if len(partsA) != len(partsB) {
		if len(partsA) < len(partsB) {
			return -1, true
		}
		return 1, true
	}
	return 0, true
}

func normalizeCatalogFamilyID(candidates ...string) string {
	for _, raw := range candidates {
		lower := strings.ToLower(strings.TrimSpace(raw))
		if lower == "" {
			continue
		}
		switch {
		case lower == "llama-cpp" || lower == "llama.cpp" || lower == "llama_cpp" || strings.Contains(lower, "llama-server") || strings.Contains(lower, "llama"):
			return "llama-cpp"
		case lower == "polystrata" || strings.Contains(lower, "polystrata"):
			return "polystrata"
		case lower == "mlx-lm" || lower == "mlx_lm" || strings.Contains(lower, "mlx"):
			return "mlx-lm"
		case lower == "vllm" || strings.Contains(lower, "vllm"):
			return "vllm"
		case lower == "sglang" || strings.Contains(lower, "sglang"):
			return "sglang"
		case lower == "lmstudio" || lower == "lm studio" || lower == "lm-studio" || strings.Contains(lower, "lmstudio"):
			return "lmstudio"
		}
	}
	return ""
}

func latestComparableCatalogVersion(family EngineFamilyCatalog, installed string) (latest string, hasUpdate bool) {
	var bestComparable string
	for _, rel := range family.Releases {
		ver := strings.TrimSpace(rel.Version)
		if ver == "" {
			continue
		}
		if installed != "" {
			if _, ok := compareEngineVersions(installed, ver); ok {
				if bestComparable == "" {
					bestComparable = ver
				} else if cmp, ok2 := compareEngineVersions(bestComparable, ver); ok2 && cmp < 0 {
					bestComparable = ver
				}
			}
		}
	}
	if bestComparable != "" {
		cmp, ok := compareEngineVersions(installed, bestComparable)
		return bestComparable, ok && cmp < 0
	}
	// No comparable version in catalog: surface primary release version without false-positive update.
	for _, rel := range family.Releases {
		ver := strings.TrimSpace(rel.Version)
		if ver != "" {
			return ver, false
		}
	}
	return "", false
}

// EngineUpdates compares locally checked/registered engine versions against the built-in
// engine catalog and reports whether a newer comparable version is available.
func (s *Service) EngineUpdates() map[string]EngineUpdateInfo {
	out := map[string]EngineUpdateInfo{}
	checks := s.EngineChecks()
	reg := s.registry()

	familyByName := make(map[string]string, len(reg.engines))
	for _, entry := range reg.engines {
		fam := normalizeCatalogFamilyID(entry.engine.Family, entry.family, entry.engine.Name, entry.path)
		if fam != "" {
			familyByName[entry.engine.Name] = fam
		}
	}

	catalog := DefaultEngineCatalog()
	for name, elig := range checks.Engines {
		familyID := normalizeCatalogFamilyID(elig.Family, familyByName[name], name)
		if familyID == "" {
			continue
		}
		family, ok := catalog.FamilyByID(familyID)
		if !ok {
			continue
		}
		installed := strings.TrimSpace(elig.Version)
		latest, hasUpdate := latestComparableCatalogVersion(family, installed)
		out[name] = EngineUpdateInfo{
			Engine:           name,
			InstalledVersion: installed,
			LatestVersion:    latest,
			HasUpdate:        hasUpdate,
		}
	}
	return out
}
