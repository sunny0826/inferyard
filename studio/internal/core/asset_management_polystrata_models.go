package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type polystrataModelDeletePlan struct {
	configPath        string
	dataDirs          []string
	files             []string
	dirs              []string
	reclaimableBytes  int64
	preservedShared   bool
	preservedExternal bool
}

func expandSplitGGUFShardsLenient(canonPath string) []assetFileSize {
	m := splitGGUFPattern.FindStringSubmatch(filepath.Base(canonPath))
	if m != nil {
		stem, idxStr, totalStr, ext := m[1], m[2], m[3], m[4]
		index, err1 := strconv.Atoi(idxStr)
		total, err2 := strconv.Atoi(totalStr)
		if err1 == nil && err2 == nil && total >= 2 && total <= 256 && index >= 1 && index <= total {
			dir := filepath.Dir(canonPath)
			shards := make([]assetFileSize, 0, total)
			for i := 1; i <= total; i++ {
				shardPath := filepath.Join(dir, fmt.Sprintf("%s-%05d-of-%05d%s", stem, i, total, ext))
				info, err := os.Lstat(shardPath)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					continue
				}
				canonShard, cerr := canonical(shardPath)
				if cerr != nil {
					canonShard = shardPath
				}
				shards = append(shards, assetFileSize{path: canonShard, size: info.Size()})
			}
			return shards
		}
	}
	info, err := os.Lstat(canonPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	return []assetFileSize{{path: canonPath, size: info.Size()}}
}

func resolvePolystrataConfigRefsLenient(cfgPath, declWorkDir, declDataDir string) ([]assetFileSize, []string) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, nil
	}
	var cfg struct {
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil, nil
	}
	var rawMap map[string]any
	_ = json.Unmarshal(raw, &rawMap)

	cfgDir := filepath.Dir(cfgPath)
	bases := polystrataConfigBases(cfg.Cwd, declWorkDir, cfgDir, declDataDir)
	refs := extractPolystrataAssetRefs(cfg.Args, rawMap)

	fileMap := map[string]int64{}
	dirMap := map[string]bool{}
	var dirs []string
	if len(refs) == 0 {
		if hasPackFallbackMarker(cfgDir) {
			if canonDir, cerr := canonical(cfgDir); cerr == nil {
				dirMap[canonDir] = true
				dirs = append(dirs, canonDir)
			}
		}
	} else {
		for _, ref := range refs {
			canonPath, isDir, ok := resolvePolystrataAssetRef(ref, bases)
			if !ok {
				continue
			}
			if isDir {
				if !dirMap[canonPath] {
					dirMap[canonPath] = true
					dirs = append(dirs, canonPath)
				}
				continue
			}
			for _, f := range expandSplitGGUFShardsLenient(canonPath) {
				fileMap[f.path] = f.size
			}
		}
	}
	files := make([]assetFileSize, 0, len(fileMap))
	for p, sz := range fileMap {
		files = append(files, assetFileSize{path: p, size: sz})
	}
	slices.SortFunc(files, func(a, b assetFileSize) int { return strings.Compare(a.path, b.path) })
	slices.Sort(dirs)
	return files, dirs
}

func findOwningPolystrataDecl(u UserSettings, canonCfgPath string) (string, string) {
	cfgDir := filepath.Dir(canonCfgPath)
	for _, rawInst := range u.EngineInstallations {
		rawBytes, err := installationBytes(rawInst)
		if err != nil {
			continue
		}
		var decl struct {
			Family           string `json:"family"`
			WorkingDirectory string `json:"working_directory"`
			DataDir          string `json:"data_dir"`
		}
		if json.Unmarshal(rawBytes, &decl) != nil || decl.Family != "polystrata" {
			continue
		}
		for _, dir := range polystrataConfigSearchDirs(decl.WorkingDirectory, decl.DataDir) {
			if canonDir, cerr := canonical(dir); cerr == nil && canonDir == cfgDir {
				return decl.WorkingDirectory, decl.DataDir
			}
		}
	}
	return "", ""
}

func collectOtherPolystrataProtectedRefs(u UserSettings, targetCanonCfg string) (map[string]bool, []string, []string) {
	protFiles := map[string]bool{}
	protDirSet := map[string]bool{}
	var protDirs []string
	var otherCfgs []string
	seenCfgs := map[string]bool{}

	for _, rawInst := range u.EngineInstallations {
		rawBytes, err := installationBytes(rawInst)
		if err != nil {
			continue
		}
		var decl struct {
			Family           string `json:"family"`
			WorkingDirectory string `json:"working_directory"`
			DataDir          string `json:"data_dir"`
		}
		if json.Unmarshal(rawBytes, &decl) != nil || decl.Family != "polystrata" {
			continue
		}
		for _, dir := range polystrataConfigSearchDirs(decl.WorkingDirectory, decl.DataDir) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, item := range entries {
				name := item.Name()
				if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
					continue
				}
				full := filepath.Join(dir, name)
				info, err := os.Lstat(full)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
					continue
				}
				resolved, err := canonical(full)
				if err != nil || resolved == targetCanonCfg || seenCfgs[resolved] {
					continue
				}
				seenCfgs[resolved] = true
				otherCfgs = append(otherCfgs, resolved)
				files, dirs := resolvePolystrataConfigRefsLenient(resolved, decl.WorkingDirectory, decl.DataDir)
				for _, f := range files {
					protFiles[f.path] = true
				}
				for _, d := range dirs {
					if !protDirSet[d] {
						protDirSet[d] = true
						protDirs = append(protDirs, d)
					}
					for _, f := range collectNonSymlinkDirFiles(d, 6) {
						protFiles[f.path] = true
					}
				}
			}
		}
	}
	return protFiles, protDirs, otherCfgs
}

func isProtectedPolystrataContainer(canonDir string, managedDataDirs []string) bool {
	for _, dd := range managedDataDirs {
		if canonDir == dd || canonDir == filepath.Join(dd, "models") || canonDir == filepath.Join(dd, "packs") {
			return true
		}
	}
	return false
}

func planPolystrataModelDelete(u UserSettings, wsEnginesRoot, rawCfgPath, canonCfgPath string) (polystrataModelDeletePlan, *ApiError) {
	scope := polystrataManagedScopeFor(u, wsEnginesRoot)
	if !isManagedPolystrataModelDeletable(rawCfgPath, canonCfgPath, scope, u.ResultsRoot) {
		return polystrataModelDeletePlan{}, errAssetNotDeletable()
	}
	cfgInfo, err := os.Lstat(canonCfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return polystrataModelDeletePlan{}, Err("not_found")
		}
		return polystrataModelDeletePlan{}, errAssetNotDeletable()
	}
	if cfgInfo.Mode()&os.ModeSymlink != 0 || !cfgInfo.Mode().IsRegular() || isSealedEvidenceMarker(filepath.Base(canonCfgPath)) {
		return polystrataModelDeletePlan{}, errAssetNotDeletable()
	}

	workDir, dataDir := findOwningPolystrataDecl(u, canonCfgPath)
	targetFiles, targetDirs := resolvePolystrataConfigRefsLenient(canonCfgPath, workDir, dataDir)
	protFiles, protDirs, otherCfgs := collectOtherPolystrataProtectedRefs(u, canonCfgPath)

	isProtectedFile := func(p string) bool {
		if protFiles[p] {
			return true
		}
		for _, pd := range protDirs {
			if withinCanonicalDir(pd, p) {
				return true
			}
		}
		return false
	}
	dirTouchesProtected := func(d string) bool {
		for _, otherCfg := range otherCfgs {
			if withinCanonicalDir(d, otherCfg) {
				return true
			}
		}
		for _, pd := range protDirs {
			if d == pd || withinCanonicalDir(d, pd) || withinCanonicalDir(pd, d) {
				return true
			}
		}
		for pf := range protFiles {
			if withinCanonicalDir(d, pf) {
				return true
			}
		}
		return false
	}

	plan := polystrataModelDeletePlan{
		configPath: canonCfgPath,
		dataDirs:   scope.dataDirs,
	}
	inManagedInstall := pathStrictlyInsideAny(filepath.Dir(canonCfgPath), scope.installDirs) ||
		slices.Contains(scope.installDirs, filepath.Dir(canonCfgPath))

	for _, d := range targetDirs {
		coveredByPlannedDir := false
		for _, prev := range plan.dirs {
			if withinCanonicalDir(prev, d) {
				coveredByPlannedDir = true
				break
			}
		}
		if coveredByPlannedDir {
			continue
		}
		if !pathStrictlyInsideAny(d, scope.dataDirs) {
			if !(len(targetFiles) == 0 && len(targetDirs) == 1 && d == filepath.Dir(canonCfgPath) && inManagedInstall) {
				plan.preservedExternal = true
			}
			continue
		}
		if isProtectedPolystrataContainer(d, scope.dataDirs) {
			continue
		}
		if dirTouchesProtected(d) {
			plan.preservedShared = true
			continue
		}
		if isSymlinkPath(d) || overlapsResultsRoot(d, u.ResultsRoot) || verifyAssetTreeSafe(d, true) != nil {
			return polystrataModelDeletePlan{}, errAssetNotDeletable()
		}
		plan.dirs = append(plan.dirs, d)
		plan.reclaimableBytes += dirDiskUsageNoSymlinks(d)
	}

	cfgInsideDeletedDir := false
	for _, d := range plan.dirs {
		if withinCanonicalDir(d, canonCfgPath) {
			cfgInsideDeletedDir = true
			break
		}
	}

	for _, f := range targetFiles {
		coveredByPlannedDir := false
		for _, d := range plan.dirs {
			if withinCanonicalDir(d, f.path) {
				coveredByPlannedDir = true
				break
			}
		}
		if coveredByPlannedDir {
			continue
		}
		if !pathStrictlyInsideAny(f.path, scope.dataDirs) {
			plan.preservedExternal = true
			continue
		}
		if isProtectedFile(f.path) {
			plan.preservedShared = true
			continue
		}
		if isSymlinkPath(f.path) || overlapsResultsRoot(f.path, u.ResultsRoot) || isSealedEvidenceMarker(filepath.Base(f.path)) {
			return polystrataModelDeletePlan{}, errAssetNotDeletable()
		}
		plan.files = append(plan.files, f.path)
		plan.reclaimableBytes += f.size
	}

	if !cfgInsideDeletedDir {
		plan.reclaimableBytes += cfgInfo.Size()
	}
	return plan, nil
}

func pruneEmptyPolystrataDataDirs(startDirs, managedDataDirs []string, resultsRoot *string) {
	for _, start := range startDirs {
		cur := filepath.Clean(start)
		for pathStrictlyInsideAny(cur, managedDataDirs) && !isProtectedPolystrataContainer(cur, managedDataDirs) {
			if isSymlinkPath(cur) || overlapsResultsRoot(cur, resultsRoot) {
				break
			}
			entries, err := os.ReadDir(cur)
			if err != nil || len(entries) != 0 {
				break
			}
			if os.Remove(cur) != nil {
				break
			}
			cur = filepath.Dir(cur)
		}
	}
}

func (s *Service) deleteManagedPolystrataModel(u UserSettings, paths *BoundPaths, rawCfgPath, canonCfgPath string) *ApiError {
	plan, err := planPolystrataModelDelete(u, workspaceEnginesRoot(u, paths), rawCfgPath, canonCfgPath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		return workspaceBusyError()
	}

	var pruneDirs []string
	for _, dir := range plan.dirs {
		if rmErr := os.RemoveAll(dir); rmErr != nil && !os.IsNotExist(rmErr) {
			s.mu.Unlock()
			return Err("internal_error")
		}
		pruneDirs = append(pruneDirs, filepath.Dir(dir))
		s.pruneAssetSnapshotLocked("model", dir)
	}
	for _, file := range plan.files {
		if rmErr := os.Remove(file); rmErr != nil && !os.IsNotExist(rmErr) {
			s.mu.Unlock()
			return Err("internal_error")
		}
		pruneDirs = append(pruneDirs, filepath.Dir(file))
		s.pruneAssetSnapshotLocked("model", file)
	}
	if rmErr := os.Remove(plan.configPath); rmErr != nil && !os.IsNotExist(rmErr) {
		s.mu.Unlock()
		return Err("internal_error")
	}
	if pathStrictlyInsideAny(plan.configPath, plan.dataDirs) {
		pruneDirs = append(pruneDirs, filepath.Dir(plan.configPath))
	}
	pruneEmptyPolystrataDataDirs(pruneDirs, plan.dataDirs, u.ResultsRoot)
	s.pruneAssetSnapshotLocked("model", plan.configPath)
	s.mu.Unlock()

	_ = s.RefreshAssets()
	return nil
}
