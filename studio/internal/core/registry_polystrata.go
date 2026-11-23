package core

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
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

type polystrataDedupeScope struct {
	dataDirs     []string
	coveredFiles map[string]bool
	coveredDirs  []string
}

func (s polystrataDedupeScope) covers(canonPath string) bool {
	if canonPath == "" || len(s.dataDirs) == 0 {
		return false
	}
	inDataDir := false
	for _, dir := range s.dataDirs {
		if pathStrictlyInsideDir(canonPath, dir) {
			inDataDir = true
			break
		}
	}
	if !inDataDir {
		return false
	}
	if s.coveredFiles[canonPath] {
		return true
	}
	for _, dir := range s.coveredDirs {
		if pathWithinDir(canonPath, dir) {
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
	subpaths := []string{
		filepath.Join("engine-poly", "BUILD.json"),
		filepath.Join("engine", "BUILD.json"),
		filepath.Join("build-metal", "BUILD.json"),
		"BUILD.json",
	}
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

func scanPolystrataModelConfigs(installPaths []string) ([]registryEntry, polystrataDedupeScope) {
	entries := []registryEntry{}
	seen := map[string]bool{}
	seenDataDirs := map[string]bool{}
	scope := polystrataDedupeScope{coveredFiles: map[string]bool{}}

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
		if dd := strings.TrimSpace(decl.DataDir); dd != "" {
			if info, err := os.Lstat(dd); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
				if canonDD, err := canonical(dd); err == nil && !seenDataDirs[canonDD] {
					seenDataDirs[canonDD] = true
					scope.dataDirs = append(scope.dataDirs, canonDD)
				}
			}
		}
		dirs := []string{}
		if decl.WorkingDirectory != "" {
			dirs = append(dirs, decl.WorkingDirectory)
		}
		if decl.DataDir != "" {
			modelsDir := filepath.Join(decl.DataDir, "models")
			dirs = append(dirs, decl.DataDir, modelsDir)
			if subEntries, err := os.ReadDir(modelsDir); err == nil {
				for _, sub := range subEntries {
					if sub.Type()&os.ModeSymlink == 0 && sub.IsDir() {
						dirs = append(dirs, filepath.Join(modelsDir, sub.Name()))
					}
				}
			}
		}
		for _, dir := range dirs {
			dirEntries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, item := range dirEntries {
				name := item.Name()
				if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
					continue
				}
				full := filepath.Join(dir, name)
				info, err := os.Lstat(full)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
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
				if err != nil || seen[resolved] {
					continue
				}
				seen[resolved] = true

				bases := polystrataConfigBases(cfg.Cwd, decl.WorkingDirectory, filepath.Dir(full), decl.DataDir)
				refRaw, explicitRef := extractPolystrataPrimaryRef(cfg.Args, rawMap)
				var assetPath string
				var assetIsDir, assetOK bool
				if explicitRef {
					assetPath, assetIsDir, assetOK = resolvePolystrataAssetRef(refRaw, bases)
				} else if hasPackFallbackMarker(filepath.Dir(full)) {
					if canonDir, err := canonical(filepath.Dir(full)); err == nil {
						assetPath, assetIsDir, assetOK = canonDir, true, true
					}
				}

				available := false
				reason := "polystrata_config_model_missing"
				var sizeBytes *int64
				var assetPaths []string
				if assetOK {
					available = true
					reason = ""
					var totalSize int64
					if assetIsDir {
						totalSize = sumNonSymlinkDirBytes(assetPath, 6)
						assetPaths = append(assetPaths, assetPath)
						scope.coveredDirs = append(scope.coveredDirs, assetPath)
					} else if fi, err := os.Stat(assetPath); err == nil {
						totalSize = fi.Size()
						assetPaths = append(assetPaths, assetPath)
						scope.coveredFiles[assetPath] = true
					}
					countedAux := map[string]bool{}
					for _, auxFlag := range []string{"--draft-model", "--mmproj"} {
						if auxRaw, ok := extractStrataArgValue(cfg.Args, auxFlag); ok {
							if auxCanon, auxIsDir, auxOK := resolvePolystrataAssetRef(auxRaw, bases); auxOK && !auxIsDir {
								already := auxCanon == assetPath || countedAux[auxCanon] || (assetIsDir && pathWithinDir(auxCanon, assetPath))
								if !already {
									if fi, err := os.Stat(auxCanon); err == nil {
										totalSize += fi.Size()
										countedAux[auxCanon] = true
										assetPaths = append(assetPaths, auxCanon)
										scope.coveredFiles[auxCanon] = true
									}
								}
							}
						}
					}
					if totalSize > 0 {
						sizeBytes = ptr(totalSize)
					}
				}

				ctxLen := parsePolystrataContextLength(cfg.Args, cfg.Context)
				displayName := polystrataConfigDisplayName(cfg.DisplayName, cfg.ModelName, resolved)
				entries = append(entries, registryEntry{
					path:        resolved,
					rawPath:     full,
					source:      "PolyStrata 配置",
					displayName: displayName,
					assetPaths:  assetPaths,
					model: ModelAsset{
						Name:          displayName,
						Format:        "strata_pack",
						SizeBytes:     sizeBytes,
						ContextLength: ctxLen,
						Available:     available,
						Reason:        reason,
					},
				})
			}
		}
	}
	return entries, scope
}

func polystrataConfigBases(cwd, workDir, cfgDir, dataDir string) []string {
	candidates := []string{}
	if trimmedCwd := strings.TrimSpace(cwd); trimmedCwd != "" {
		cwdCandidate := trimmedCwd
		if !filepath.IsAbs(cwdCandidate) {
			base := strings.TrimSpace(workDir)
			if base == "" {
				base = cfgDir
			}
			cwdCandidate = filepath.Join(base, cwdCandidate)
		}
		if info, err := os.Stat(cwdCandidate); err == nil && info.IsDir() {
			candidates = append(candidates, cwdCandidate)
		}
	}
	for _, dir := range []string{workDir, cfgDir, dataDir} {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		if info, err := os.Stat(trimmed); err == nil && info.IsDir() {
			candidates = append(candidates, trimmed)
		}
	}
	return uniqueCleanPaths(candidates)
}

func extractPolystrataPrimaryRef(args []string, rawMap map[string]any) (string, bool) {
	for _, flag := range []string{"--model", "--native", "--pack"} {
		if val, ok := extractStrataArgValue(args, flag); ok {
			return val, true
		}
	}
	for _, key := range polystrataLegacyModelKeys {
		if val, ok := rawMap[key].(string); ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val), true
		}
	}
	return "", false
}

func extractStrataArgValue(args []string, flag string) (string, bool) {
	prefix := flag + "="
	for i, item := range args {
		if item == flag && i+1 < len(args) {
			if val := strings.TrimSpace(args[i+1]); val != "" {
				return val, true
			}
		}
		if strings.HasPrefix(item, prefix) {
			if val := strings.TrimSpace(strings.TrimPrefix(item, prefix)); val != "" {
				return val, true
			}
		}
	}
	return "", false
}

func hasPackFallbackMarker(dir string) bool {
	for _, name := range []string{"tokenizer_config.json", "chat_template.jinja"} {
		p := filepath.Join(dir, name)
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func resolvePolystrataAssetRef(raw string, bases []string) (string, bool, bool) {
	ref := strings.TrimSpace(raw)
	if ref == "" || strings.ContainsRune(ref, 0) {
		return "", false, false
	}
	check := func(candidate string) (string, bool, bool) {
		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", false, false
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return "", false, false
		}
		canon, err := canonical(candidate)
		if err != nil {
			return "", false, false
		}
		return canon, info.IsDir(), true
	}
	if filepath.IsAbs(ref) {
		return check(ref)
	}
	for _, base := range bases {
		if canon, isDir, ok := check(filepath.Join(base, ref)); ok {
			return canon, isDir, true
		}
	}
	return "", false, false
}

func sumNonSymlinkDirBytes(root string, maxDepth int) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if d.IsDir() {
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
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
