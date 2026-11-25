package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	polystrataPinnedTag          = "v0.2.20-poly.5"
	polystrataLegacyPinnedTag    = "v0.1.45-poly.10"
	polystrataLegacyPinnedCommit = "3e1642161198c50ff1abed4046f4e4f476099dbb"
)

var polystrataArchiveTagPattern = regexp.MustCompile(`(?i)/archive/(?:refs/tags/)?([^/]+?)(?:\.tar\.gz|\.tgz|\.zip)$`)

type polystrataFamilyPrefix struct {
	prefix string
	family string
}

// Ordered longest-prefix first so e.g. "qwen36s-" and "ornith122-" match before shorter prefixes.
var polystrataTagPrefixes = []polystrataFamilyPrefix{
	{"ornith122-", "ornith122"},
	{"minicpmv-", "minicpmv"},
	{"inkling-", "inkling"},
	{"kolibri1-", "kolibri"},
	{"unsloth-", "unsloth"},
	{"qwen36s-", "qwen36-strata"},
	{"qwen36-", "qwen36"},
	{"clover-", "clover"},
	{"ornith-", "ornith"},
	{"gemma4-", "gemma4"},
	{"swift-", "swift"},
	{"coder-", "coder"},
	{"nvfp4-", "nvfp4"},
	{"lfm25-", "lfm25"},
	{"flint-", "flint"},
	{"groot-", "groot"},
	{"gyro-", "gyro"},
	{"dsv4-", "deepseek"},
	{"tiel-", "tiel"},
	{"glm-", "glm"},
}

var polystrataCanonicalQuants = map[string]string{
	"q2_0":       "Q2_0",
	"iq1_m":      "IQ1_M",
	"iq2_xs":     "IQ2_XS",
	"iq3_xxs":    "IQ3_XXS",
	"iq3_s":      "IQ3_S",
	"iq4_nl":     "IQ4_NL",
	"q4_k_m":     "Q4_K_M",
	"q5_k_m":     "Q5_K_M",
	"q6_k":       "Q6_K",
	"q8_0":       "Q8_0",
	"ud-iq1_m":   "UD-IQ1_M",
	"ud-iq2_xxs": "UD-IQ2_XXS",
	"ud-iq3_xxs": "UD-IQ3_XXS",
	"ud-iq4_xs":  "UD-IQ4_XS",
	"ud-q2_k_xl": "UD-Q2_K_XL",
	"ud-q3_k_m":  "UD-Q3_K_M",
	"ud-q3_k_xl": "UD-Q3_K_XL",
	"ud-q4_k_m":  "UD-Q4_K_M",
	"ud-q4_k_xl": "UD-Q4_K_XL",
	"ud-q5_k_xl": "UD-Q5_K_XL",
	"ud-q6_k_xl": "UD-Q6_K_XL",
}

func hasExtractedPolystrataFiles(dir string) bool {
	if isRegularNonSymlink(filepath.Join(dir, "setup.py")) &&
		isRegularNonSymlink(filepath.Join(dir, "serve", "server.py")) {
		return true
	}
	termDir := filepath.Join(dir, "terminal")
	info, err := os.Lstat(termDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	return isRegularNonSymlink(filepath.Join(termDir, "setup.py")) &&
		isRegularNonSymlink(filepath.Join(termDir, "serve", "server.py"))
}

func polystrataBuildJSONSubpaths() []string {
	dirs := []string{
		filepath.Join("terminal", "engine-poly"),
		filepath.Join("terminal", "engine"),
		filepath.Join("terminal", "engine-cuda12"),
		filepath.Join("engine", "build-metal"),
		"engine-poly",
		"engine",
		"build-metal",
		"",
	}
	names := []string{"BUILD.json", "BUILD-poly.json", "BUILD-q36.json"}
	out := make([]string, 0, len(dirs)*len(names))
	for _, d := range dirs {
		for _, n := range names {
			if d == "" {
				out = append(out, n)
			} else {
				out = append(out, filepath.Join(d, n))
			}
		}
	}
	return out
}

func readPolystrataReceiptVersion(dir string) string {
	receiptPath := filepath.Join(dir, ".inferyard-runtime.json")
	if !isRegularNonSymlink(receiptPath) {
		return ""
	}
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		return ""
	}
	var doc struct {
		Definition string `json:"definition"`
		Source     string `json:"source"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Definition != polystrataRuntimeDefinition {
		return ""
	}
	src := strings.TrimSpace(doc.Source)
	switch {
	case src == "":
		return ""
	case src == polystrataDefaultSourceURL || strings.Contains(src, polystrataPinnedCommit):
		return polystrataPinnedTag
	case strings.Contains(src, polystrataLegacyPinnedCommit):
		return polystrataLegacyPinnedTag
	}
	m := polystrataArchiveTagPattern.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	tag := strings.TrimSpace(m[1])
	if _, ok := parseSemverVersion(tag); ok {
		return tag
	}
	return ""
}

func parsePolystrataTagDir(tagDir string) (string, string) {
	trimmed := strings.TrimSpace(tagDir)
	if trimmed == "" {
		return "", ""
	}
	lower := strings.ToLower(trimmed)
	for _, entry := range polystrataTagPrefixes {
		if strings.HasPrefix(lower, entry.prefix) {
			quant := strings.TrimSpace(trimmed[len(entry.prefix):])
			if quant != "" {
				return entry.family, quant
			}
			return "", ""
		}
	}
	if canon, ok := polystrataCanonicalQuants[lower]; ok {
		return "qwen", canon
	}
	return "", ""
}

func parsePolystrataConfigSlug(slug string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(slug))
	if lower == "" {
		return "", ""
	}
	for _, entry := range polystrataTagPrefixes {
		if strings.HasPrefix(lower, entry.prefix) {
			rest := lower[len(entry.prefix):]
			if canon, ok := polystrataCanonicalQuants[rest]; ok {
				return entry.family, canon
			}
			return "", ""
		}
	}
	if canon, ok := polystrataCanonicalQuants[lower]; ok {
		return "qwen", canon
	}
	return "", ""
}

func inferPolystrataUpdateSpec(instDir, dataDir string) (family, model, ggufPath string, contextLen int) {
	type candFile struct {
		path    string
		modTime time.Time
	}
	localDirs := polystrataConfigSearchDirs(instDir, "")
	var sharedDirs []string
	for _, dir := range polystrataConfigSearchDirs("", dataDir) {
		if !slices.Contains(localDirs, dir) {
			sharedDirs = append(sharedDirs, dir)
		}
	}
	var fallbackGGUF string
	var fallbackCtx int
	for _, tier := range [][]string{localDirs, sharedDirs} {
		var candidates []candFile
		for _, dir := range tier {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, item := range entries {
				name := item.Name()
				if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") || strings.Contains(name, "-retained-") {
					continue
				}
				full := filepath.Join(dir, name)
				info, err := os.Lstat(full)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxStrataConfigBytes {
					continue
				}
				candidates = append(candidates, candFile{path: full, modTime: info.ModTime()})
			}
		}
		slices.SortFunc(candidates, func(a, b candFile) int {
			return b.modTime.Compare(a.modTime)
		})
		for _, cand := range candidates {
			raw, err := os.ReadFile(cand.path)
			if err != nil {
				continue
			}
			var cfg struct {
				Args    []string `json:"args"`
				Cwd     string   `json:"cwd"`
				Context *int64   `json:"context"`
			}
			if json.Unmarshal(raw, &cfg) != nil {
				continue
			}
			var cfgCtx int
			if parsedCtx := parsePolystrataContextLength(cfg.Args, cfg.Context); parsedCtx != nil && *parsedCtx >= 1 && *parsedCtx <= 524288 {
				cfgCtx = int(*parsedCtx)
				if fallbackCtx == 0 {
					fallbackCtx = cfgCtx
				}
			}
			bases := polystrataConfigBases(cfg.Cwd, instDir, filepath.Dir(cand.path), dataDir)
			for _, flag := range []string{"--model", "--native"} {
				rawRef, ok := extractStrataArgValue(cfg.Args, flag)
				if !ok {
					continue
				}
				resolved := rawRef
				if canon, _, ok2 := resolvePolystrataAssetRef(rawRef, bases); ok2 {
					resolved = canon
				}
				parentDir := filepath.Dir(resolved)
				if strings.EqualFold(filepath.Base(filepath.Dir(parentDir)), "models") {
					if fam, mod := parsePolystrataTagDir(filepath.Base(parentDir)); fam != "" && mod != "" {
						return fam, mod, "", cfgCtx
					}
				}
				if flag == "--model" && runtime.GOOS != "darwin" && fallbackGGUF == "" &&
					strings.HasSuffix(strings.ToLower(resolved), ".gguf") && filepath.IsAbs(resolved) && isRegularNonSymlink(resolved) {
					fallbackGGUF = resolved
					fallbackCtx = cfgCtx
				}
			}
			slug := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(cand.path), "strata-"), ".json")
			if fam, mod := parsePolystrataConfigSlug(slug); fam != "" && mod != "" {
				return fam, mod, "", cfgCtx
			}
		}
		if fallbackGGUF != "" {
			return "", "", fallbackGGUF, fallbackCtx
		}
	}
	return "", "", fallbackGGUF, fallbackCtx
}
