package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
	fileInfos  []os.FileInfo
	files      []string
	dirs       []string
	cfgs       []string
	unreadable bool
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

func findOwningPolystrataDecl(u UserSettings, paths *BoundPaths, wsEnginesRoot, canonCfgPath string) (string, string) {
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
	for _, dd := range defaultManagedPolystrataDataDirs(u, paths, wsEnginesRoot) {
		for _, dir := range polystrataConfigSearchDirs("", dd) {
			if canonDir, cerr := canonical(dir); cerr == nil && sameFileOrPath(canonDir, cfgDir) {
				return "", dd
			}
		}
	}
	return "", ""
}

func collectOtherPolystrataProtectedRefs(u UserSettings, paths *BoundPaths, wsEnginesRoot, targetCanonCfg string) protectedPolystrataRefs {
	prot := protectedPolystrataRefs{}
	var seenContexts []polystrataCfgContext
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
			if targetCanonCfg != "" && sameDirectoryEntry(full, targetCanonCfg) {
				continue
			}
			linfo, lerr := os.Lstat(full)
			info, err := os.Stat(full)
			if err != nil {
				if lerr == nil && linfo.Mode()&os.ModeSymlink == 0 && linfo.Mode().IsRegular() {
					prot.unreadable = true
				}
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if info.Size() > maxStrataConfigBytes {
				prot.unreadable = true
				continue
			}
			resolved, err := canonical(full)
			if err != nil {
				prot.unreadable = true
				continue
			}
			if targetCanonCfg != "" && sameDirectoryEntry(resolved, targetCanonCfg) {
				continue
			}
			if seenCfgInContext(seenContexts, resolved, declWorkDir, declDataDir, false) {
				continue
			}
			seenContexts = append(seenContexts, polystrataCfgContext{
				cfg:     resolved,
				workDir: filepath.Clean(strings.TrimSpace(declWorkDir)),
				dataDir: filepath.Clean(strings.TrimSpace(declDataDir)),
			})
			for _, cfgEntry := range []string{resolved, filepath.Clean(full)} {
				if !containsSameDirEntry(prot.cfgs, cfgEntry) {
					prot.cfgs = append(prot.cfgs, cfgEntry)
				}
				recordFile(cfgEntry)
			}
			files, dirs, ok := resolvePolystrataConfigRefsProtected(resolved, declWorkDir, declDataDir)
			if !ok {
				prot.unreadable = true
				continue
			}
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
	for _, dd := range defaultManagedPolystrataDataDirs(u, paths, wsEnginesRoot) {
		for _, dir := range polystrataConfigSearchDirs("", dd) {
			scanDir(dir, "", dd)
		}
	}
	return prot
}

func (p protectedPolystrataRefs) touchesFile(path string) bool {
	if p.unreadable {
		return true
	}
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
	if p.unreadable {
		return true
	}
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

func planPolystrataModelDelete(u UserSettings, paths *BoundPaths, wsEnginesRoot, rawCfgPath, canonCfgPath string) (polystrataModelDeletePlan, *ApiError) {
	resultsRoot := derivedResultsRoot(u, paths)
	scope := polystrataManagedScopeFor(u, paths, wsEnginesRoot)
	if !isManagedPolystrataModelDeletable(rawCfgPath, canonCfgPath, scope, resultsRoot) {
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

	workDir, dataDir := findOwningPolystrataDecl(u, paths, wsEnginesRoot, canonCfgPath)
	targetFiles, targetDirs := resolvePolystrataConfigRefsLenient(canonCfgPath, workDir, dataDir)
	prot := collectOtherPolystrataProtectedRefs(u, paths, wsEnginesRoot, canonCfgPath)
	protectedPaths := registeredInstallationProtectedPaths(u, paths, wsEnginesRoot)

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
		if isSymlinkPath(d) || overlapsResultsRoot(d, resultsRoot) || verifyAssetTreeSafe(d, true) != nil {
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
		if isSymlinkPath(f.path) || overlapsResultsRoot(f.path, resultsRoot) || isSealedEvidenceMarker(filepath.Base(f.path)) {
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

type stagedDeleteTarget struct {
	orig   string
	staged string
	isDir  bool
}

func stagePolystrataDeleteTargets(targets []stagedDeleteTarget) ([]stagedDeleteTarget, error) {
	staged := make([]stagedDeleteTarget, 0, len(targets))
	rollback := func() {
		for i := len(staged) - 1; i >= 0; i-- {
			_ = os.Rename(staged[i].staged, staged[i].orig)
		}
	}
	for _, t := range targets {
		stagedPath := filepath.Join(filepath.Dir(t.orig), ".inferyard-del-"+uuid()+"-"+filepath.Base(t.orig))
		if err := os.Rename(t.orig, stagedPath); err != nil {
			rollback()
			return nil, err
		}
		entry := stagedDeleteTarget{orig: t.orig, staged: stagedPath, isDir: t.isDir}
		staged = append(staged, entry)
		if t.isDir {
			if err := checkDirTreeWritable(stagedPath); err != nil {
				rollback()
				return nil, err
			}
		}
	}
	return staged, nil
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

	plan, err := planPolystrataModelDelete(u, paths, workspaceEnginesRoot(u, paths), rawCfgPath, canonCfgPath)
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
	pruneEmptyPolystrataDataDirs(pruneDirs, plan.dataDirs, plan.protectedPaths, derivedResultsRoot(u, paths))

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
