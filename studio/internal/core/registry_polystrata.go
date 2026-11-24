package core

import (
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/l10n"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	inDataDir := false
	for _, dir := range s.dataDirs {
		if pathStrictlyInsideDirSameFS(canonPath, dir) {
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
			var sizeBytes *int64
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

func validatePolystrataAssets(refs []string, cfgDir string, bases []string) ([]assetFileSize, []string, bool) {
	fileSizes := map[string]int64{}
	dirSeen := map[string]bool{}
	resolvedDirs := []string{}
	if len(refs) == 0 {
		if !hasPackFallbackMarker(cfgDir) {
			return nil, nil, false
		}
		canonDir, err := canonical(cfgDir)
		if err != nil {
			return nil, nil, false
		}
		resolvedDirs = append(resolvedDirs, canonDir)
		for _, f := range collectNonSymlinkDirFiles(canonDir, 6) {
			fileSizes[f.path] = f.size
		}
	} else {
		for _, ref := range refs {
			canonPath, isDir, ok := resolvePolystrataAssetRef(ref, bases)
			if !ok {
				return nil, nil, false
			}
			if isDir {
				if !dirSeen[canonPath] {
					dirSeen[canonPath] = true
					resolvedDirs = append(resolvedDirs, canonPath)
					for _, f := range collectNonSymlinkDirFiles(canonPath, 6) {
						fileSizes[f.path] = f.size
					}
				}
				continue
			}
			shards, ok := expandSplitGGUFShards(canonPath)
			if !ok {
				return nil, nil, false
			}
			for _, sh := range shards {
				fileSizes[sh.path] = sh.size
			}
		}
	}
	if len(fileSizes) == 0 && len(resolvedDirs) == 0 {
		return nil, nil, false
	}
	paths := make([]string, 0, len(fileSizes))
	for p := range fileSizes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	assetFiles := make([]assetFileSize, 0, len(paths))
	for _, p := range paths {
		assetFiles = append(assetFiles, assetFileSize{path: p, size: fileSizes[p]})
	}
	return assetFiles, resolvedDirs, true
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

func extractPolystrataAssetRefs(args []string, rawMap map[string]any) []string {
	refs := []string{}
	for _, flag := range []string{"--model", "--native", "--pack", "--ple-gguf", "--embd-gguf", "--draft-model", "--mmproj"} {
		refs = append(refs, extractAllStrataArgValues(args, flag)...)
	}
	for _, mtpVal := range extractAllStrataArgValues(args, "--mtp") {
		if _, err := strconv.Atoi(mtpVal); err != nil {
			refs = append(refs, mtpVal)
		}
	}
	for _, key := range append(append([]string(nil), polystrataLegacyModelKeys...), "tokenizer", "tokenizer_path", "tokenizer_dir") {
		if val, ok := rawMap[key].(string); ok && strings.TrimSpace(val) != "" {
			refs = append(refs, strings.TrimSpace(val))
		}
	}
	if vision, ok := rawMap["vision"].(map[string]any); ok {
		if mmproj, ok := vision["mmproj"].(string); ok && strings.TrimSpace(mmproj) != "" {
			refs = append(refs, strings.TrimSpace(mmproj))
		}
	}
	return refs
}

func extractAllStrataArgValues(args []string, flag string) []string {
	prefix := flag + "="
	var out []string
	for i, item := range args {
		if item == flag && i+1 < len(args) {
			if val := strings.TrimSpace(args[i+1]); val != "" {
				out = append(out, val)
			}
		}
		if strings.HasPrefix(item, prefix) {
			if val := strings.TrimSpace(strings.TrimPrefix(item, prefix)); val != "" {
				out = append(out, val)
			}
		}
	}
	return out
}

func extractStrataArgValue(args []string, flag string) (string, bool) {
	vals := extractAllStrataArgValues(args, flag)
	if len(vals) == 0 {
		return "", false
	}
	return vals[0], true
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

func expandSplitGGUFShards(canonPath string) ([]assetFileSize, bool) {
	m := splitGGUFPattern.FindStringSubmatch(filepath.Base(canonPath))
	if m == nil {
		fi, err := os.Stat(canonPath)
		if err != nil {
			return nil, false
		}
		return []assetFileSize{{path: canonPath, size: fi.Size()}}, true
	}
	stem, idxStr, totalStr, ext := m[1], m[2], m[3], m[4]
	index, err1 := strconv.Atoi(idxStr)
	total, err2 := strconv.Atoi(totalStr)
	if err1 != nil || err2 != nil || total < 2 || index < 1 || index > total {
		fi, err := os.Stat(canonPath)
		if err != nil {
			return nil, false
		}
		return []assetFileSize{{path: canonPath, size: fi.Size()}}, true
	}
	if total > 256 {
		return nil, false
	}
	dir := filepath.Dir(canonPath)
	shards := make([]assetFileSize, 0, total)
	for i := 1; i <= total; i++ {
		shardPath := filepath.Join(dir, fmt.Sprintf("%s-%05d-of-%05d%s", stem, i, total, ext))
		info, err := os.Lstat(shardPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, false
		}
		canonShard, err := canonical(shardPath)
		if err != nil {
			canonShard = shardPath
		}
		shards = append(shards, assetFileSize{path: canonShard, size: info.Size()})
	}
	return shards, true
}

func collectNonSymlinkDirFiles(root string, maxDepth int) []assetFileSize {
	files := []assetFileSize{}
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
				canonFile, cerr := canonical(path)
				if cerr != nil {
					canonFile = filepath.Clean(path)
				}
				files = append(files, assetFileSize{path: canonFile, size: info.Size()})
			}
		}
		return nil
	})
	return files
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
