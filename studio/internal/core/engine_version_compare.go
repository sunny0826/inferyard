package core

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	buildVersionPattern  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])b(\d{3,})(?:[^0-9]|$)`)
	semverVersionPattern = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)(?:\.(\d+))?(?:-([0-9a-zA-Z.-]+))?(?:\.post(\d+))?(?:\+[0-9a-zA-Z.-]+)?$`)
)

type parsedSemver struct {
	major, minor, patch int
	pre                 string
	post                int
}

func parseBuildVersion(raw string) (int, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
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
	post := 0
	var err3, err4 error
	if m[3] != "" {
		patch, err3 = strconv.Atoi(m[3])
	}
	if len(m) > 5 && m[5] != "" {
		post, err4 = strconv.Atoi(m[5])
	}
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || major < 0 || minor < 0 || patch < 0 || post < 0 {
		return parsedSemver{}, false
	}
	return parsedSemver{major: major, minor: minor, patch: patch, pre: m[4], post: post}, true
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
		if a.post != b.post {
			if a.post < b.post {
				return -1, true
			}
			return 1, true
		}
		return 0, true
	}
	if a.pre == "" {
		return 1, true
	}
	if b.pre == "" {
		return -1, true
	}
	cmp, ok := comparePrerelease(a.pre, b.pre)
	if !ok || cmp != 0 {
		return cmp, ok
	}
	if a.post != b.post {
		if a.post < b.post {
			return -1, true
		}
		return 1, true
	}
	return 0, true
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
		case lower == "lmstudio" || lower == "lm studio" || lower == "lm-studio" || strings.Contains(lower, "lmstudio"):
			return "lmstudio"
		}
	}
	return ""
}

// catalogReleasesForUpdate narrows catalog releases to the target platform and
// distribution so multi-track families such as llama-cpp do not cross-compare.
func catalogReleasesForUpdate(family EngineFamilyCatalog, dist, platform string) []EngineRelease {
	releases := family.ReleasesForPlatform(platform, false)
	if len(releases) == 0 {
		releases = family.Releases
	}
	d := strings.TrimSpace(dist)
	if d == "" {
		return releases
	}
	var matched []EngineRelease
	for _, rel := range releases {
		if strings.EqualFold(strings.TrimSpace(rel.Distribution), d) {
			matched = append(matched, rel)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return releases
}
