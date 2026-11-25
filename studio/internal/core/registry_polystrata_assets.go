package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func expandPolystrataAssetDir(canonDir string) string {
	if !strings.EqualFold(filepath.Base(canonDir), "rt") {
		return canonDir
	}
	parent := filepath.Dir(canonDir)
	if !strings.EqualFold(filepath.Base(parent), "mtp") || isSymlinkPath(parent) {
		return canonDir
	}
	if isRegularNonSymlink(filepath.Join(parent, "mtp-manifest.json")) {
		return parent
	}
	if info, err := os.Lstat(filepath.Join(parent, "tensors")); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
		return parent
	}
	return canonDir
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
				canonPath = expandPolystrataAssetDir(canonPath)
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

func estimatePolystrataResidentBytes(args, bases []string, assetFiles []assetFileSize, totalSize int64) int64 {
	if len(assetFiles) == 0 || totalSize <= 0 {
		return 0
	}
	pleFiles := map[string]bool{}
	for _, rawRef := range extractAllStrataArgValues(args, "--ple-gguf") {
		if canonPath, isDir, ok := resolvePolystrataAssetRef(rawRef, bases); ok && !isDir {
			pleFiles[canonPath] = true
		}
	}
	var packDirs, mtpDirs []string
	for _, rawRef := range extractAllStrataArgValues(args, "--pack") {
		if canonPath, isDir, ok := resolvePolystrataAssetRef(rawRef, bases); ok && isDir {
			packDirs = append(packDirs, canonPath)
		}
	}
	for _, rawRef := range extractAllStrataArgValues(args, "--mtp") {
		if _, err := strconv.Atoi(rawRef); err == nil {
			continue
		}
		if canonPath, isDir, ok := resolvePolystrataAssetRef(rawRef, bases); ok && isDir {
			mtpDirs = append(mtpDirs, expandPolystrataAssetDir(canonPath))
		}
	}
	mmapExperts := slices.Contains(args, "--mmap-experts")
	var resident int64
	for _, f := range assetFiles {
		if f.size <= 0 || pleFiles[f.path] {
			continue
		}
		lowerName := strings.ToLower(filepath.Base(f.path))
		inMtp := slices.ContainsFunc(mtpDirs, func(d string) bool { return pathWithinDirSameFS(d, f.path) })
		if inMtp {
			if strings.HasSuffix(lowerName, ".gguf") {
				continue
			}
			if mmapExperts && strings.Contains(lowerName, ".experts.") {
				resident += (f.size + 4) / 5
			} else {
				resident += f.size
			}
			continue
		}
		inPack := slices.ContainsFunc(packDirs, func(d string) bool { return pathWithinDirSameFS(d, f.path) })
		if inPack {
			resident += f.size
			continue
		}
		if mmapExperts && strings.HasSuffix(lowerName, ".gguf") {
			resident += (f.size + 4) / 5
			continue
		}
		resident += f.size
	}
	return resident
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
