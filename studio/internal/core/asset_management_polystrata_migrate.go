package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func readPolystrataInstallDirs(canonInstPath string) (workDir, dataDir string) {
	rawBytes, err := installationBytes(canonInstPath)
	if err != nil {
		return "", ""
	}
	var decl struct {
		WorkingDirectory string `json:"working_directory"`
		DataDir          string `json:"data_dir"`
	}
	if json.Unmarshal(rawBytes, &decl) != nil {
		return "", ""
	}
	return strings.TrimSpace(decl.WorkingDirectory), strings.TrimSpace(decl.DataDir)
}

func registeredInstallationProtectedPaths(u UserSettings, paths *BoundPaths, wsEnginesRoot string) []string {
	out := protectedPolystrataDataDirs(u, paths, wsEnginesRoot)
	addPath := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.ContainsRune(trimmed, 0) {
			return
		}
		resolved := filepath.Clean(trimmed)
		if c, err := canonical(trimmed); err == nil {
			resolved = c
		}
		if !slices.ContainsFunc(out, func(prev string) bool { return sameDirectoryEntry(prev, resolved) }) {
			out = append(out, resolved)
		}
	}
	for _, rawInst := range u.EngineInstallations {
		addPath(rawInst)
		rawBytes, err := installationBytes(rawInst)
		if err != nil {
			continue
		}
		var decl struct {
			Executable       string   `json:"executable"`
			WorkingDirectory string   `json:"working_directory"`
			ModelsRoot       string   `json:"models_root"`
			DataDir          string   `json:"data_dir"`
			RuntimeRoots     []string `json:"runtime_roots"`
		}
		if json.Unmarshal(rawBytes, &decl) != nil {
			continue
		}
		for _, p := range append([]string{decl.Executable, decl.WorkingDirectory, decl.ModelsRoot, decl.DataDir}, decl.RuntimeRoots...) {
			addPath(p)
		}
	}
	return out
}

type migratedPolystrataConfig struct {
	dd      string
	name    string
	payload []byte
}

func polystrataMigPlansDigest(plans []migratedPolystrataConfig) string {
	ordered := slices.Clone(plans)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].dd != ordered[j].dd {
			return ordered[i].dd < ordered[j].dd
		}
		return ordered[i].name < ordered[j].name
	})
	h := sha256.New()
	for _, p := range ordered {
		sum := sha256.Sum256(p.payload)
		_, _ = fmt.Fprintf(h, "%s|%s|%x\n", p.dd, p.name, sum)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func snapshotPolystrataRootConfigs(installDir, declWorkDir, declDataDir string, managedDataDirs, survivingInstalls []string) ([]migratedPolystrataConfig, error) {
	if len(managedDataDirs) == 0 {
		return nil, nil
	}
	configDirs := []string{installDir}
	termDir := filepath.Join(installDir, "terminal")
	if tInfo, tErr := os.Lstat(termDir); tErr == nil && tInfo.Mode()&os.ModeSymlink == 0 && tInfo.IsDir() {
		configDirs = append(configDirs, termDir)
	}
	var plans []migratedPolystrataConfig
	for _, cfgDir := range configDirs {
		entries, err := os.ReadDir(cfgDir)
		if err != nil {
			if cfgDir == installDir {
				return nil, err
			}
			continue
		}
		for _, item := range entries {
			name := item.Name()
			if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			srcPath := filepath.Join(cfgDir, name)
			info, err := os.Lstat(srcPath)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			if info.Size() > maxStrataConfigBytes {
				return nil, fmt.Errorf("%s exceeds max config size", name)
			}
			raw, err := os.ReadFile(srcPath)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			var cfg struct {
				Exe         string   `json:"exe"`
				Args        []string `json:"args"`
				Cwd         string   `json:"cwd"`
				ModelName   string   `json:"model_name"`
				DisplayName string   `json:"display_name"`
			}
			if json.Unmarshal(raw, &cfg) != nil || strings.TrimSpace(cfg.Exe) == "" || cfg.Args == nil {
				continue
			}
			var rawMap map[string]any
			if json.Unmarshal(raw, &rawMap) != nil || rawMap == nil {
				continue
			}

			origBases := polystrataConfigBases(cfg.Cwd, declWorkDir, cfgDir, declDataDir)
			origLenFiles, origLenDirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, origBases)
			origRefs := extractPolystrataAssetRefs(cfg.Args, rawMap)
			origValidFiles, origValidDirs, origValidOK := validatePolystrataAssets(origRefs, cfgDir, origBases)
			resolvedSrc := srcPath
			if c, cerr := canonical(srcPath); cerr == nil {
				resolvedSrc = c
			}
			origDisplayName := polystrataConfigDisplayName(cfg.DisplayName, cfg.ModelName, resolvedSrc)

			for _, dd := range managedDataDirs {
				if isSymlinkPath(dd) {
					continue
				}
				if !polystrataRefsScopedToDataDir(origLenFiles, origLenDirs, installDir, dd) {
					continue
				}
				if field, hasCred := findPolystrataPlaintextCredentialField(rawMap, cfg.Args); hasCred {
					return nil, &polystrataCredentialError{configName: name, field: field}
				}
				if origValidOK && survivingModelAlreadyCovered(survivingInstalls, managedDataDirs, installDir, origDisplayName, origValidFiles) {
					break
				}

				clonedMap, rewrittenArgs := rewritePolystrataConfigMapForDataDir(rawMap, cfg.Args, origBases, installDir)
				rewrittenBytes, err := json.MarshalIndent(clonedMap, "", "  ")
				if err != nil {
					return nil, err
				}
				rewrittenBytes = append(rewrittenBytes, '\n')

				if err := verifyMigratedPolystrataConfig(dd, origLenFiles, origLenDirs, origValidFiles, origValidDirs, origValidOK, rewrittenArgs, clonedMap); err != nil {
					return nil, err
				}
				plans = append(plans, migratedPolystrataConfig{dd: dd, name: name, payload: rewrittenBytes})
				break
			}
		}
	}
	return plans, nil
}

func writeMigratedPolystrataConfigs(plans []migratedPolystrataConfig) ([]string, error) {
	var written []string
	rollback := func() {
		for i := len(written) - 1; i >= 0; i-- {
			_ = os.Remove(written[i])
		}
	}
	for _, p := range plans {
		destPath, skipExisting, err := selectMigratedConfigPath(p.dd, p.name, p.payload)
		if err != nil {
			rollback()
			return nil, err
		}
		if skipExisting {
			continue
		}
		if err := writeFileAtomic(p.dd, destPath, p.payload, 0o600); err != nil {
			rollback()
			return nil, err
		}
		written = append(written, destPath)
	}
	return written, nil
}

func polystrataRefsScopedToDataDir(files []assetFileSize, dirs []string, installDir, dd string) bool {
	inDD := false
	for _, f := range files {
		if pathWithinDirSameFS(installDir, f.path) {
			return false
		}
		if pathStrictlyInsideDirSameFS(f.path, dd) {
			inDD = true
		}
	}
	for _, d := range dirs {
		if pathWithinDirSameFS(installDir, d) {
			return false
		}
		if pathWithinDirSameFS(dd, d) {
			inDD = true
		}
	}
	return inDD
}

func rewritePolystrataConfigMapForDataDir(rawMap map[string]any, origArgs, origBases []string, installDir string) (map[string]any, []string) {
	cloned := maps.Clone(rawMap)
	rewriteVal := func(rawVal string) string {
		trimmed := strings.TrimSpace(rawVal)
		if trimmed == "" {
			return rawVal
		}
		if canonPath, _, ok := resolvePolystrataAssetRef(trimmed, origBases); ok {
			if !pathWithinDirSameFS(installDir, canonPath) {
				return canonPath
			}
			return rawVal
		}
		if shards := collectBrokenSplitGGUFShards(trimmed, origBases); len(shards) > 0 {
			dir := filepath.Dir(shards[0].path)
			if !pathWithinDirSameFS(installDir, dir) {
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
		if trimmedCwd != "" {
			canonCwd := ""
			if c, err := canonical(trimmedCwd); err == nil {
				canonCwd = c
			}
			if !filepath.IsAbs(trimmedCwd) ||
				pathWithinDirSameFS(installDir, filepath.Clean(trimmedCwd)) ||
				(canonCwd != "" && pathWithinDirSameFS(installDir, canonCwd)) {
				delete(cloned, "cwd")
			}
		}
	}
	return cloned, outArgs
}

func verifyMigratedPolystrataConfig(
	dd string,
	origLenFiles []assetFileSize,
	origLenDirs []string,
	origValidFiles []assetFileSize,
	origValidDirs []string,
	origValidOK bool,
	newArgs []string,
	newMap map[string]any,
) error {
	destCwd, _ := newMap["cwd"].(string)
	destBases := polystrataConfigBases(destCwd, "", dd, dd)

	newRefs := extractPolystrataAssetRefs(newArgs, newMap)
	if origValidOK {
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

func selectMigratedConfigPath(dd, name string, payload []byte) (string, bool, error) {
	destPath := filepath.Join(dd, name)
	linfo, lerr := os.Lstat(destPath)
	if os.IsNotExist(lerr) {
		return destPath, false, nil
	}
	if lerr != nil {
		return "", false, lerr
	}
	if linfo.Mode()&os.ModeSymlink == 0 && linfo.Mode().IsRegular() {
		existing, err := os.ReadFile(destPath)
		if err != nil {
			return "", false, err
		}
		if bytes.Equal(existing, payload) {
			return "", true, nil
		}
	}
	stem := strings.TrimSuffix(name, ".json")
	for i := 1; i <= 100; i++ {
		cand := filepath.Join(dd, fmt.Sprintf("%s-retained-%d.json", stem, i))
		if _, cerr := os.Lstat(cand); os.IsNotExist(cerr) {
			return cand, false, nil
		}
	}
	return "", false, errors.New("no available retained strata config slot")
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

func parseAndResolvePolystrataRefs(cfgPath, declWorkDir, declDataDir string, includeNonRelCwd bool) ([]assetFileSize, []string, bool) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, nil, false
	}
	var cfg struct {
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil, nil, true
	}
	var rawMap map[string]any
	_ = json.Unmarshal(raw, &rawMap)

	cfgDir := filepath.Dir(cfgPath)
	files, dirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, polystrataConfigBases(cfg.Cwd, declWorkDir, cfgDir, declDataDir))
	// CLI inspect_polystrata_payload ignores relative cwd; union non-cwd bases so CLI-resolved weights stay protected too.
	if includeNonRelCwd {
		if trimmedCwd := strings.TrimSpace(cfg.Cwd); trimmedCwd != "" && !filepath.IsAbs(trimmedCwd) {
			cliFiles, cliDirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, polystrataConfigBases("", declWorkDir, cfgDir, declDataDir))
			for _, f := range cliFiles {
				appendUniqueAssetFile(&files, f)
			}
			for _, d := range cliDirs {
				if !slices.ContainsFunc(dirs, func(prev string) bool { return sameFileOrPath(prev, d) }) {
					dirs = append(dirs, d)
				}
			}
		}
	}
	return files, dirs, true
}

func resolvePolystrataConfigRefsProtected(cfgPath, declWorkDir, declDataDir string) ([]assetFileSize, []string, bool) {
	return parseAndResolvePolystrataRefs(cfgPath, declWorkDir, declDataDir, true)
}

func resolvePolystrataConfigRefsLenient(cfgPath, declWorkDir, declDataDir string) ([]assetFileSize, []string) {
	files, dirs, _ := parseAndResolvePolystrataRefs(cfgPath, declWorkDir, declDataDir, false)
	return files, dirs
}

func resolvePolystrataConfigRefsWithBases(args []string, rawMap map[string]any, cfgDir string, bases []string) ([]assetFileSize, []string) {
	refs := extractPolystrataAssetRefs(args, rawMap)

	var files []assetFileSize
	var dirs []string
	if len(refs) == 0 {
		if hasPackFallbackMarker(cfgDir) {
			if canonDir, cerr := canonical(cfgDir); cerr == nil {
				dirs = append(dirs, canonDir)
			}
		}
	} else {
		for _, ref := range refs {
			canonPath, isDir, ok := resolvePolystrataAssetRef(ref, bases)
			if !ok {
				for _, f := range collectBrokenSplitGGUFShards(ref, bases) {
					appendUniqueAssetFile(&files, f)
				}
				continue
			}
			if isDir {
				canonPath = expandPolystrataAssetDir(canonPath)
				if !slices.ContainsFunc(dirs, func(prev string) bool { return sameFileOrPath(prev, canonPath) }) {
					dirs = append(dirs, canonPath)
				}
				continue
			}
			for _, f := range expandSplitGGUFShardsLenient(canonPath) {
				appendUniqueAssetFile(&files, f)
			}
		}
	}
	slices.SortFunc(files, func(a, b assetFileSize) int { return strings.Compare(a.path, b.path) })
	slices.Sort(dirs)
	return files, dirs
}
