package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const maxBuildJSONBytes = 64 << 10

var polystrataLegacyModelKeys = []string{
	"gguf",
	"gguf_path",
	"model_path",
	"weights_path",
	"model_dir",
	"pack_dir",
	"weights_dir",
}

var splitGGUFPattern = regexp.MustCompile(`(?i)^(.+)-(\d{5})-of-(\d{5})(\.gguf)$`)

type assetFileSize struct {
	path string
	size int64
}

type polystrataDedupeScope struct {
	dataDirs     []string
	coveredFiles map[string]bool
	coveredDirs  []string
}

func (s polystrataDedupeScope) covers(canonPath string) bool {
	if canonPath == "" || len(s.dataDirs) == 0 {
		return false
	}
	if strings.EqualFold(filepath.Base(filepath.Dir(canonPath)), "mtp") &&
		strings.HasPrefix(strings.ToLower(filepath.Base(canonPath)), "mtp-") {
		return true
	}
	inDataDir := false
	for _, dir := range s.dataDirs {
		if pathStrictlyInsideDirSameFS(canonPath, dir) {
			inDataDir = true
			if rel, err := filepath.Rel(dir, canonPath); err == nil {
				firstSeg := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
				if strings.EqualFold(firstSeg, "mtp") || strings.EqualFold(firstSeg, "packs") {
					return true
				}
			}
			break
		}
	}
	if !inDataDir {
		return false
	}
	if s.coveredFiles[canonPath] {
		return true
	}
	for cf := range s.coveredFiles {
		if sameFileOrPath(canonPath, cf) {
			return true
		}
	}
	for _, dir := range s.coveredDirs {
		if pathWithinDirSameFS(dir, canonPath) {
			return true
		}
	}
	return false
}

func pathWithinDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathStrictlyInsideDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func deduplicatePolystrataModels(entries []registryEntry, scope polystrataDedupeScope) []registryEntry {
	if len(scope.dataDirs) == 0 {
		return entries
	}
	filtered := make([]registryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.model.Format == "gguf" && scope.covers(entry.path) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func polystrataInstallationDisplayName(instPath, workingDir string) string {
	roots := uniqueCleanPaths([]string{strings.TrimSpace(workingDir), filepath.Dir(instPath)})
	for _, root := range roots {
		if ver := readPolystrataReceiptVersion(root); ver != "" {
			return "PolyStrata · " + ver
		}
	}
	subpaths := polystrataBuildJSONSubpaths()
	for _, root := range roots {
		for _, sub := range subpaths {
			if ver := readBuildJSONVersion(filepath.Join(root, sub)); ver != "" {
				return "PolyStrata · " + ver
			}
		}
	}
	dir := strings.TrimSpace(workingDir)
	if dir == "" {
		dir = filepath.Dir(instPath)
	}
	slug := filepath.Base(filepath.Clean(dir))
	if slug != "" && slug != "." && slug != string(filepath.Separator) && !strings.EqualFold(slug, "polystrata") {
		return "PolyStrata · " + slug
	}
	return "PolyStrata"
}

func readBuildJSONVersion(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxBuildJSONBytes {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	for _, key := range []string{"version", "release", "tag", "commit"} {
		val, ok := doc[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.Join(strings.Fields(val), " ")
		if trimmed == "" {
			continue
		}
		if key == "commit" && len(trimmed) > 12 {
			trimmed = trimmed[:8]
		}
		return trimmed
	}
	return ""
}

func uniqueCleanPaths(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if !seen[cleaned] {
			seen[cleaned] = true
			out = append(out, cleaned)
		}
	}
	return out
}

func scanPolystrataModelConfigs(installPaths, extraDataDirs []string) ([]registryEntry, polystrataDedupeScope) {
	entries := []registryEntry{}
	var seen []string
	scope := polystrataDedupeScope{coveredFiles: map[string]bool{}}

	addDataDir := func(dd string) string {
		dd = strings.TrimSpace(dd)
		if dd == "" {
			return ""
		}
		info, err := os.Lstat(dd)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ""
		}
		canonDD, err := canonical(dd)
		if err != nil {
			return ""
		}
		for _, prev := range scope.dataDirs {
			if sameFileOrPath(prev, canonDD) {
				return prev
			}
		}
		scope.dataDirs = append(scope.dataDirs, canonDD)
		return canonDD
	}

	scanConfigDir := func(dir, declWorkDir, declDataDir string) {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, item := range dirEntries {
			name := item.Name()
			if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			full := filepath.Join(dir, name)
			info, err := os.Lstat(full)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxStrataConfigBytes {
				continue
			}
			cfgRaw, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			var cfg struct {
				Exe         string   `json:"exe"`
				Args        []string `json:"args"`
				Cwd         string   `json:"cwd"`
				ModelName   string   `json:"model_name"`
				DisplayName string   `json:"display_name"`
				Context     *int64   `json:"context"`
			}
			if json.Unmarshal(cfgRaw, &cfg) != nil || strings.TrimSpace(cfg.Exe) == "" || cfg.Args == nil {
				continue
			}
			var rawMap map[string]any
			_ = json.Unmarshal(cfgRaw, &rawMap)
			resolved, err := canonical(full)
			if err != nil || containsSameDirEntry(seen, resolved) {
				continue
			}
			seen = append(seen, resolved)

			bases := polystrataConfigBases(cfg.Cwd, declWorkDir, filepath.Dir(full), declDataDir)
			refs := extractPolystrataAssetRefs(cfg.Args, rawMap)
			assetFiles, resolvedDirs, assetOK := validatePolystrataAssets(refs, filepath.Dir(full), bases)

			available := false
			reason := "polystrata_config_model_missing"
			var sizeBytes, residentBytes *int64
			var coveredPaths []string
			if assetOK {
				available = true
				reason = ""
				var totalSize int64
				for _, f := range assetFiles {
					totalSize += f.size
					scope.coveredFiles[f.path] = true
					coveredPaths = append(coveredPaths, f.path)
				}
				for _, d := range resolvedDirs {
					scope.coveredDirs = append(scope.coveredDirs, d)
				}
				if totalSize > 0 {
					sizeBytes = ptr(totalSize)
					if res := estimatePolystrataResidentBytes(cfg.Args, bases, assetFiles, totalSize); res > 0 && res < totalSize {
						residentBytes = ptr(res)
					}
				}
			} else {
				assetFiles = nil
			}

			ctxLen := parsePolystrataContextLength(cfg.Args, cfg.Context)
			displayName := polystrataConfigDisplayName(cfg.DisplayName, cfg.ModelName, resolved)
			entries = append(entries, registryEntry{
				path:        resolved,
				rawPath:     full,
				source:      l10n.TR("core.registry.source.polystrata_config"),
				displayName: displayName,
				assetFiles:  assetFiles,
				model: ModelAsset{
					Name:          displayName,
					Format:        "strata_pack",
					SizeBytes:     sizeBytes,
					ResidentBytes: residentBytes,
					ContextLength: ctxLen,
					Available:     available,
					Reason:        reason,
					CoveredPaths:  coveredPaths,
				},
			})
		}
	}

	for _, instPath := range installPaths {
		raw, err := installationBytes(instPath)
		if err != nil {
			continue
		}
		var decl struct {
			Family           string `json:"family"`
			WorkingDirectory string `json:"working_directory"`
			DataDir          string `json:"data_dir"`
		}
		if json.Unmarshal(raw, &decl) != nil || decl.Family != "polystrata" {
			continue
		}
		addDataDir(decl.DataDir)
		for _, dir := range polystrataConfigSearchDirs(decl.WorkingDirectory, decl.DataDir) {
			scanConfigDir(dir, decl.WorkingDirectory, decl.DataDir)
		}
	}
	for _, dd := range extraDataDirs {
		canonDD := addDataDir(dd)
		if canonDD == "" {
			continue
		}
		for _, dir := range polystrataConfigSearchDirs("", canonDD) {
			scanConfigDir(dir, "", canonDD)
		}
	}
	return entries, scope
}

func polystrataConfigSearchDirs(workingDir, dataDir string) []string {
	dirs := []string{}
	if workingDir != "" {
		dirs = append(dirs, workingDir)
		termDir := filepath.Join(workingDir, "terminal")
		if info, err := os.Lstat(termDir); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			dirs = append(dirs, termDir)
		}
	}
	if dataDir != "" {
		modelsDir := filepath.Join(dataDir, "models")
		dirs = append(dirs, dataDir, modelsDir)
		if subEntries, err := os.ReadDir(modelsDir); err == nil {
			for _, sub := range subEntries {
				if sub.Type()&os.ModeSymlink == 0 && sub.IsDir() {
					dirs = append(dirs, filepath.Join(modelsDir, sub.Name()))
				}
			}
		}
	}
	return dirs
}

func parsePolystrataContextLength(args []string, cfgContext *int64) *int64 {
	if rawCtx, ok := extractStrataArgValue(args, "--max-context"); ok {
		if parsed, err := strconv.ParseInt(rawCtx, 10, 64); err == nil && parsed > 0 {
			return ptr(parsed)
		}
	}
	if cfgContext != nil && *cfgContext > 0 {
		return ptr(*cfgContext)
	}
	return nil
}

func polystrataConfigDisplayName(displayName, modelName, resolvedPath string) string {
	for _, raw := range []string{displayName, modelName} {
		cleaned := strings.Join(strings.Fields(raw), " ")
		if cleaned != "" {
			return cleaned
		}
	}
	base := filepath.Base(resolvedPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem != "" {
		return stem
	}
	return base
}
