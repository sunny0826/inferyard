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
	protectedPaths    []string
	files             []string
	dirs              []string
	reclaimableBytes  int64
	preservedShared   bool
	preservedExternal bool
}

type protectedPolystrataRefs struct {
	fileInfos []os.FileInfo
	files     []string
	dirs      []string
	cfgs      []string
}

// sameDirectoryEntry returns true when two paths refer to the same file AND the same
// directory entry (case aliases on case-insensitive volumes return true, while hardlinks
// with distinct names or directories return false).
func sameDirectoryEntry(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	if !sameFileOrPath(ca, cb) {
		return false
	}
	dirA, dirB := filepath.Dir(ca), filepath.Dir(cb)
	if !sameFileOrPath(dirA, dirB) {
		return false
	}
	baseA, baseB := filepath.Base(ca), filepath.Base(cb)
	if baseA == baseB {
		return true
	}
	if !strings.EqualFold(baseA, baseB) {
		return false
	}
	entries, err := os.ReadDir(dirA)
	if err != nil {
		return false
	}
	distinct := 0
	for _, entry := range entries {
		if entry.Name() == baseA || entry.Name() == baseB {
			distinct++
		}
	}
	return distinct < 2
}

func containsSameDirEntry(list []string, target string) bool {
	return slices.ContainsFunc(list, func(prev string) bool {
		return sameDirectoryEntry(prev, target)
	})
}

func appendUniqueAssetFile(files *[]assetFileSize, candidate assetFileSize) {
	for _, existing := range *files {
		if sameDirectoryEntry(existing.path, candidate.path) {
			return
		}
	}
	*files = append(*files, candidate)
}

func expandSplitGGUFShardsLenient(canonPath string) []assetFileSize {
	m := splitGGUFPattern.FindStringSubmatch(filepath.Base(canonPath))
	if m != nil {
		stem, idxStr, totalStr, ext := m[1], m[2], m[3], m[4]
		index, err1 := strconv.Atoi(idxStr)
		total, err2 := strconv.Atoi(totalStr)
		if err1 == nil && err2 == nil && total >= 2 && total <= 256 && index >= 1 && index <= total {
			dir := filepath.Dir(canonPath)
			var shards []assetFileSize
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
				appendUniqueAssetFile(&shards, assetFileSize{path: canonShard, size: info.Size()})
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

func collectBrokenSplitGGUFShards(ref string, bases []string) []assetFileSize {
	baseName := filepath.Base(ref)
	if !splitGGUFPattern.MatchString(baseName) {
		return nil
	}
	var candidateDirs []string
	if filepath.IsAbs(ref) {
		candidateDirs = []string{filepath.Dir(ref)}
	} else {
		relDir := filepath.Dir(ref)
		for _, b := range bases {
			candidateDirs = append(candidateDirs, filepath.Join(b, relDir))
		}
	}
	var shards []assetFileSize
	for _, dir := range candidateDirs {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		canonDir, cerr := canonical(dir)
		if cerr != nil {
			continue
		}
		for _, sh := range expandSplitGGUFShardsLenient(filepath.Join(canonDir, baseName)) {
			appendUniqueAssetFile(&shards, sh)
		}
	}
	return shards
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
	return resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, bases)
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

func findOwningPolystrataDecl(u UserSettings, wsEnginesRoot, canonCfgPath string) (string, string) {
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
			if canonDir, cerr := canonical(dir); cerr == nil && sameFileOrPath(canonDir, cfgDir) {
				return decl.WorkingDirectory, decl.DataDir
			}
		}
	}
	for _, dd := range defaultManagedPolystrataDataDirs(u, wsEnginesRoot) {
		for _, dir := range polystrataConfigSearchDirs("", dd) {
			if canonDir, cerr := canonical(dir); cerr == nil && sameFileOrPath(canonDir, cfgDir) {
				return "", dd
			}
		}
	}
	return "", ""
}

func collectOtherPolystrataProtectedRefs(u UserSettings, wsEnginesRoot, targetCanonCfg string) protectedPolystrataRefs {
	prot := protectedPolystrataRefs{}
	var seenCfgs []string
	seenDirs := map[string]bool{}
	seenFiles := map[string]bool{}

	recordFile := func(p string) {
		if seenFiles[p] {
			return
		}
		seenFiles[p] = true
		prot.files = append(prot.files, p)
		if info, err := os.Lstat(p); err == nil {
			prot.fileInfos = append(prot.fileInfos, info)
		}
	}
	scanDir := func(dir, declWorkDir, declDataDir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
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
			if err != nil {
				continue
			}
			if (targetCanonCfg != "" && sameDirectoryEntry(resolved, targetCanonCfg)) || containsSameDirEntry(seenCfgs, resolved) {
				continue
			}
			seenCfgs = append(seenCfgs, resolved)
			prot.cfgs = append(prot.cfgs, resolved)
			recordFile(resolved)
			files, dirs := resolvePolystrataConfigRefsLenient(resolved, declWorkDir, declDataDir)
			for _, f := range files {
				recordFile(f.path)
			}
			for _, d := range dirs {
				if !seenDirs[d] {
					seenDirs[d] = true
					prot.dirs = append(prot.dirs, d)
				}
				for _, f := range collectNonSymlinkDirFiles(d, 6) {
					recordFile(f.path)
				}
			}
		}
	}

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
			scanDir(dir, decl.WorkingDirectory, decl.DataDir)
		}
	}
	for _, dd := range defaultManagedPolystrataDataDirs(u, wsEnginesRoot) {
		for _, dir := range polystrataConfigSearchDirs("", dd) {
			scanDir(dir, "", dd)
		}
	}
	return prot
}

func (p protectedPolystrataRefs) touchesFile(path string) bool {
	if slices.ContainsFunc(p.files, func(pf string) bool { return sameFileOrPath(pf, path) }) {
		return true
	}
	if info, err := os.Lstat(path); err == nil {
		for _, pfInfo := range p.fileInfos {
			if os.SameFile(info, pfInfo) {
				return true
			}
		}
	}
	for _, pd := range p.dirs {
		if pathWithinDirSameFS(pd, path) {
			return true
		}
	}
	return false
}

func (p protectedPolystrataRefs) touchesDir(dir string) bool {
	for _, otherCfg := range p.cfgs {
		if pathWithinDirSameFS(dir, otherCfg) {
			return true
		}
	}
	for _, pd := range p.dirs {
		if sameFileOrPath(dir, pd) || pathWithinDirSameFS(dir, pd) || pathWithinDirSameFS(pd, dir) {
			return true
		}
	}
	for _, pf := range p.files {
		if pathWithinDirSameFS(dir, pf) {
			return true
		}
	}
	return false
}

func isProtectedPolystrataContainer(canonDir string, managedDataDirs []string) bool {
	for _, dd := range managedDataDirs {
		if sameFileOrPath(canonDir, dd) ||
			sameFileOrPath(canonDir, filepath.Join(dd, "models")) ||
			sameFileOrPath(canonDir, filepath.Join(dd, "packs")) {
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

	workDir, dataDir := findOwningPolystrataDecl(u, wsEnginesRoot, canonCfgPath)
	targetFiles, targetDirs := resolvePolystrataConfigRefsLenient(canonCfgPath, workDir, dataDir)
	prot := collectOtherPolystrataProtectedRefs(u, wsEnginesRoot, canonCfgPath)
	protectedPaths := registeredInstallationProtectedPaths(u, wsEnginesRoot)

	plan := polystrataModelDeletePlan{
		configPath:     canonCfgPath,
		dataDirs:       scope.dataDirs,
		protectedPaths: append(slices.Clone(protectedPaths), prot.dirs...),
	}
	inManagedInstall := pathStrictlyInsideAnySameFS(canonCfgPath, scope.installDirs)

	for _, d := range targetDirs {
		if slices.ContainsFunc(plan.dirs, func(prev string) bool { return pathWithinDirSameFS(prev, d) }) {
			continue
		}
		if !pathStrictlyInsideAnySameFS(d, scope.dataDirs) {
			if !(len(targetFiles) == 0 && len(targetDirs) == 1 && sameFileOrPath(d, filepath.Dir(canonCfgPath)) && inManagedInstall) {
				plan.preservedExternal = true
			}
			continue
		}
		if isProtectedPolystrataContainer(d, scope.dataDirs) {
			continue
		}
		if prot.touchesDir(d) || slices.ContainsFunc(protectedPaths, func(p string) bool { return sameFileOrPath(d, p) || pathWithinDirSameFS(d, p) }) {
			plan.preservedShared = true
			continue
		}
		if isSymlinkPath(d) || overlapsResultsRoot(d, u.ResultsRoot) || verifyAssetTreeSafe(d, true) != nil {
			return polystrataModelDeletePlan{}, errAssetNotDeletable()
		}
		plan.dirs = append(plan.dirs, d)
	}

	cfgInsideDeletedDir := slices.ContainsFunc(plan.dirs, func(d string) bool {
		return pathWithinDirSameFS(d, canonCfgPath)
	})

	for _, f := range targetFiles {
		if slices.ContainsFunc(plan.dirs, func(d string) bool { return pathWithinDirSameFS(d, f.path) }) {
			continue
		}
		if !pathStrictlyInsideAnySameFS(f.path, scope.dataDirs) {
			plan.preservedExternal = true
			continue
		}
		if prot.touchesFile(f.path) || slices.ContainsFunc(protectedPaths, func(p string) bool { return sameFileOrPath(f.path, p) }) {
			plan.preservedShared = true
			continue
		}
		if isSymlinkPath(f.path) || overlapsResultsRoot(f.path, u.ResultsRoot) || isSealedEvidenceMarker(filepath.Base(f.path)) {
			return polystrataModelDeletePlan{}, errAssetNotDeletable()
		}
		plan.files = append(plan.files, f.path)
	}

	reclaimFiles := append([]string(nil), plan.files...)
	if !cfgInsideDeletedDir {
		reclaimFiles = append(reclaimFiles, plan.configPath)
	}
	plan.reclaimableBytes = reclaimableDiskBytes(plan.dirs, reclaimFiles)
	return plan, nil
}

func pruneEmptyPolystrataDataDirs(startDirs, managedDataDirs, protectedPaths []string, resultsRoot *string) {
	for _, start := range startDirs {
		cur := filepath.Clean(start)
		for pathStrictlyInsideAnySameFS(cur, managedDataDirs) && !isProtectedPolystrataContainer(cur, managedDataDirs) {
			if isSymlinkPath(cur) || overlapsResultsRoot(cur, resultsRoot) {
				break
			}
			if slices.ContainsFunc(protectedPaths, func(p string) bool {
				return sameFileOrPath(cur, p) || pathWithinDirSameFS(cur, p)
			}) {
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

func (s *Service) deleteManagedPolystrataModel(rawCfgPath, canonCfgPath, expectedTarget, expectedFP string) *ApiError {
	mutated, err := s.runDeleteManagedPolystrataModel(rawCfgPath, canonCfgPath, expectedTarget, expectedFP)
	if mutated || err == nil {
		_ = s.RefreshAssets()
	}
	return err
}

func (s *Service) runDeleteManagedPolystrataModel(rawCfgPath, canonCfgPath, expectedTarget, expectedFP string) (bool, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		return false, workspaceBusyError()
	}
	s.deletingAssets++
	u := clone(s.settings)
	paths := s.paths
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.deletingAssets--
		s.mu.Unlock()
	}()

	plan, err := planPolystrataModelDelete(u, workspaceEnginesRoot(u, paths), rawCfgPath, canonCfgPath)
	if err != nil {
		return false, err
	}
	if expectedTarget != "" {
		if !sameFileOrPath(plan.configPath, expectedTarget) {
			return false, deleteTargetChangedError()
		}
		if expectedFP != "" && polystrataModelDeleteFingerprint(plan) != expectedFP {
			return false, deleteTargetChangedError()
		}
	}

	var targets []stagedDeleteTarget
	cfgInsideDeletedDir := slices.ContainsFunc(plan.dirs, func(d string) bool {
		return pathWithinDirSameFS(d, plan.configPath)
	})
	if !cfgInsideDeletedDir {
		targets = append(targets, stagedDeleteTarget{orig: plan.configPath, isDir: false})
	}
	for _, dir := range plan.dirs {
		targets = append(targets, stagedDeleteTarget{orig: dir, isDir: true})
	}
	for _, file := range plan.files {
		targets = append(targets, stagedDeleteTarget{orig: file, isDir: false})
	}

	staged, stageErr := stagePolystrataDeleteTargets(targets)
	if stageErr != nil {
		if os.IsNotExist(stageErr) {
			return false, Err("not_found")
		}
		return false, Err("internal_error")
	}

	var removeErr error
	var pruneDirs []string
	var removedOrigs []string
	for _, entry := range staged {
		var rmErr error
		if entry.isDir {
			rmErr = os.RemoveAll(entry.staged)
		} else {
			rmErr = os.Remove(entry.staged)
		}
		if rmErr != nil && !os.IsNotExist(rmErr) {
			if removeErr == nil {
				removeErr = rmErr
			}
			continue
		}
		removedOrigs = append(removedOrigs, entry.orig)
		if !entry.isDir && sameFileOrPath(entry.orig, plan.configPath) {
			if pathStrictlyInsideAnySameFS(plan.configPath, plan.dataDirs) {
				pruneDirs = append(pruneDirs, filepath.Dir(plan.configPath))
			}
		} else {
			pruneDirs = append(pruneDirs, filepath.Dir(entry.orig))
		}
	}
	pruneEmptyPolystrataDataDirs(pruneDirs, plan.dataDirs, plan.protectedPaths, u.ResultsRoot)

	s.mu.Lock()
	for _, orig := range removedOrigs {
		s.pruneAssetSnapshotLocked("model", orig)
	}
	s.pruneAssetSnapshotLocked("model", plan.configPath)
	s.mu.Unlock()
	if removeErr != nil {
		return true, Err("internal_error")
	}
	return true, nil
}
