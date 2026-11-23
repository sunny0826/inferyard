package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func migratePolystrataRootConfigsToDataDir(stagedDir, origInstallDir string, managedDataDirs []string) ([]string, error) {
	if len(managedDataDirs) == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(stagedDir)
	if err != nil {
		return nil, err
	}
	var written []string
	rollback := func() {
		for i := len(written) - 1; i >= 0; i-- {
			_ = os.Remove(written[i])
		}
	}

	for _, item := range entries {
		name := item.Name()
		if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		srcPath := filepath.Join(stagedDir, name)
		info, err := os.Lstat(srcPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
			continue
		}
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			rollback()
			return nil, err
		}
		var cfg struct {
			Exe  string   `json:"exe"`
			Args []string `json:"args"`
			Cwd  string   `json:"cwd"`
		}
		if json.Unmarshal(raw, &cfg) != nil || strings.TrimSpace(cfg.Exe) == "" || cfg.Args == nil {
			continue
		}
		var rawMap map[string]any
		if json.Unmarshal(raw, &rawMap) != nil || rawMap == nil {
			continue
		}

		for _, dd := range managedDataDirs {
			if isSymlinkPath(dd) {
				continue
			}
			origBases := migrateConfigOrigBases(cfg.Cwd, stagedDir, origInstallDir, dd)
			origLenFiles, origLenDirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, stagedDir, origBases)
			if !polystrataRefsScopedToDataDir(origLenFiles, origLenDirs, stagedDir, origInstallDir, dd) {
				continue
			}

			clonedMap, rewrittenArgs := rewritePolystrataConfigMapForDataDir(rawMap, cfg.Args, origBases, stagedDir, origInstallDir)
			rewrittenBytes, err := json.MarshalIndent(clonedMap, "", "  ")
			if err != nil {
				rollback()
				return nil, err
			}
			rewrittenBytes = append(rewrittenBytes, '\n')

			if err := verifyMigratedPolystrataConfig(dd, stagedDir, origBases, cfg.Args, rawMap, origLenFiles, origLenDirs, rewrittenArgs, clonedMap); err != nil {
				rollback()
				return nil, err
			}

			destPath, skipExisting, err := selectMigratedConfigPath(dd, name, rewrittenBytes)
			if err != nil {
				rollback()
				return nil, err
			}
			if skipExisting {
				break
			}
			if err := writeFileAtomic(dd, destPath, rewrittenBytes, 0o600); err != nil {
				rollback()
				return nil, err
			}
			written = append(written, destPath)
			break
		}
	}
	return written, nil
}

func polystrataRefsScopedToDataDir(files []assetFileSize, dirs []string, stagedDir, origInstallDir, dd string) bool {
	inDD := false
	for _, f := range files {
		if pathWithinDirSameFS(stagedDir, f.path) || pathWithinDirSameFS(origInstallDir, f.path) {
			return false
		}
		if pathStrictlyInsideDirSameFS(f.path, dd) {
			inDD = true
		}
	}
	for _, d := range dirs {
		if pathWithinDirSameFS(stagedDir, d) || pathWithinDirSameFS(origInstallDir, d) {
			return false
		}
		if pathStrictlyInsideDirSameFS(d, dd) {
			inDD = true
		}
	}
	return inDD
}

func migrateConfigOrigBases(cwd, stagedDir, origInstallDir, dd string) []string {
	candidates := []string{}
	trimmedCwd := strings.TrimSpace(cwd)
	if trimmedCwd != "" {
		mapped := trimmedCwd
		if !filepath.IsAbs(mapped) {
			mapped = filepath.Join(stagedDir, mapped)
		} else if relocated, ok := rebaseInstallSubpath(mapped, origInstallDir, stagedDir); ok {
			mapped = relocated
		}
		if info, err := os.Stat(mapped); err == nil && info.IsDir() {
			candidates = append(candidates, mapped)
		}
	}
	for _, dir := range []string{stagedDir, dd} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			candidates = append(candidates, dir)
		}
	}
	return uniqueCleanPaths(candidates)
}

func rebaseInstallSubpath(path, origInstallDir, stagedDir string) (string, bool) {
	cleanOrig := filepath.Clean(origInstallDir)
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanOrig {
		return stagedDir, true
	}
	if withinCanonicalDir(cleanOrig, cleanPath) {
		rel, err := filepath.Rel(cleanOrig, cleanPath)
		if err == nil {
			return filepath.Join(stagedDir, rel), true
		}
	}
	return "", false
}

func rewritePolystrataConfigMapForDataDir(rawMap map[string]any, origArgs, origBases []string, stagedDir, origInstallDir string) (map[string]any, []string) {
	cloned := maps.Clone(rawMap)
	rewriteVal := func(rawVal string) string {
		trimmed := strings.TrimSpace(rawVal)
		if trimmed == "" || filepath.IsAbs(trimmed) {
			return rawVal
		}
		if canonPath, _, ok := resolvePolystrataAssetRef(trimmed, origBases); ok {
			if !pathWithinDirSameFS(stagedDir, canonPath) && !pathWithinDirSameFS(origInstallDir, canonPath) {
				return canonPath
			}
			return rawVal
		}
		if shards := collectBrokenSplitGGUFShards(trimmed, origBases); len(shards) > 0 {
			dir := filepath.Dir(shards[0].path)
			if !pathWithinDirSameFS(stagedDir, dir) && !pathWithinDirSameFS(origInstallDir, dir) {
				return filepath.Join(dir, filepath.Base(trimmed))
			}
		}
		return rawVal
	}

	pathFlags := map[string]bool{
		"--model": true, "--native": true, "--pack": true,
		"--ple-gguf": true, "--embd-gguf": true, "--draft-model": true, "--mmproj": true,
	}
	isNonInt := func(v string) bool {
		_, err := strconv.Atoi(strings.TrimSpace(v))
		return err != nil
	}
	outArgs := slices.Clone(origArgs)
	for i := 0; i < len(outArgs); i++ {
		item := outArgs[i]
		if pathFlags[item] || item == "--mtp" {
			if i+1 < len(outArgs) {
				if item != "--mtp" || isNonInt(outArgs[i+1]) {
					outArgs[i+1] = rewriteVal(outArgs[i+1])
				}
				i++
			}
			continue
		}
		if eq := strings.IndexByte(item, '='); eq > 0 {
			flag, val := item[:eq], item[eq+1:]
			if pathFlags[flag] || (flag == "--mtp" && isNonInt(val)) {
				outArgs[i] = flag + "=" + rewriteVal(val)
			}
		}
	}
	cloned["args"] = outArgs

	for _, key := range append(append([]string(nil), polystrataLegacyModelKeys...), "tokenizer", "tokenizer_path", "tokenizer_dir") {
		if val, ok := cloned[key].(string); ok && strings.TrimSpace(val) != "" {
			cloned[key] = rewriteVal(val)
		}
	}
	if vision, ok := cloned["vision"].(map[string]any); ok {
		visionClone := maps.Clone(vision)
		if mmproj, ok := visionClone["mmproj"].(string); ok && strings.TrimSpace(mmproj) != "" {
			visionClone["mmproj"] = rewriteVal(mmproj)
		}
		cloned["vision"] = visionClone
	}
	if cwdRaw, ok := cloned["cwd"].(string); ok {
		trimmedCwd := strings.TrimSpace(cwdRaw)
		if trimmedCwd != "" && (!filepath.IsAbs(trimmedCwd) ||
			pathWithinDirSameFS(origInstallDir, filepath.Clean(trimmedCwd)) ||
			pathWithinDirSameFS(stagedDir, filepath.Clean(trimmedCwd)) ||
			withinCanonicalDir(filepath.Clean(origInstallDir), filepath.Clean(trimmedCwd)) ||
			filepath.Clean(trimmedCwd) == filepath.Clean(origInstallDir)) {
			delete(cloned, "cwd")
		}
	}
	return cloned, outArgs
}

func verifyMigratedPolystrataConfig(
	dd, stagedDir string,
	origBases, origArgs []string,
	origMap map[string]any,
	origLenFiles []assetFileSize,
	origLenDirs []string,
	newArgs []string,
	newMap map[string]any,
) error {
	destCwd, _ := newMap["cwd"].(string)
	destBases := polystrataConfigBases(destCwd, "", dd, dd)

	origRefs := extractPolystrataAssetRefs(origArgs, origMap)
	newRefs := extractPolystrataAssetRefs(newArgs, newMap)
	origValidFiles, origValidDirs, origOK := validatePolystrataAssets(origRefs, stagedDir, origBases)
	if origOK {
		newValidFiles, newValidDirs, newOK := validatePolystrataAssets(newRefs, dd, destBases)
		if !newOK || !sameAssetFileSets(origValidFiles, newValidFiles) || !samePathSets(origValidDirs, newValidDirs) {
			return fmt.Errorf("migrated config assets differ after relocating to %s", dd)
		}
	}

	newLenFiles, newLenDirs := resolvePolystrataConfigRefsWithBases(newArgs, newMap, dd, destBases)
	if !sameAssetFileSets(origLenFiles, newLenFiles) || !samePathSets(origLenDirs, newLenDirs) {
		return fmt.Errorf("migrated config lenient refs differ after relocating to %s", dd)
	}
	return nil
}

func sameAssetFileSets(a, b []assetFileSize) bool {
	if len(a) != len(b) {
		return false
	}
	for _, af := range a {
		if !slices.ContainsFunc(b, func(bf assetFileSize) bool { return sameFileOrPath(af.path, bf.path) }) {
			return false
		}
	}
	return true
}

func samePathSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, ap := range a {
		if !slices.ContainsFunc(b, func(bp string) bool { return sameFileOrPath(ap, bp) }) {
			return false
		}
	}
	return true
}

func selectMigratedConfigPath(dd, name string, payload []byte) (string, bool, error) {
	destPath := filepath.Join(dd, name)
	existing, err := os.ReadFile(destPath)
	if err == nil {
		if bytes.Equal(existing, payload) {
			return "", true, nil
		}
		stem := strings.TrimSuffix(name, ".json")
		for i := 1; i <= 100; i++ {
			cand := filepath.Join(dd, fmt.Sprintf("%s-retained-%d.json", stem, i))
			if _, lerr := os.Lstat(cand); os.IsNotExist(lerr) {
				return cand, false, nil
			}
		}
		return "", false, errors.New("no available retained strata config slot")
	}
	if !os.IsNotExist(err) {
		return "", false, err
	}
	return destPath, false, nil
}

func writeFileAtomic(dir, destPath string, payload []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(dir, ".inferyard-migrate-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(payload); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
