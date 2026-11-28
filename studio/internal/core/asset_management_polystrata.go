package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const polystrataRuntimeDefinition = "polystrata-runtime.v1"

var (
	polystrataVenvPythonName   = regexp.MustCompile(`(?i)^python(3(\.\d+)?)?(\.exe)?$`)
	polystrataPythonTargetName = regexp.MustCompile(`(?i)^python(\d+(\.\d+)*)?(\.exe)?$`)
)

type polystrataManagedScope struct {
	installDirs []string
	dataDirs    []string
}

func sameFileOrPath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	ia, errA := os.Lstat(ca)
	ib, errB := os.Lstat(cb)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

func pathWithinDirSameFS(dir, target string) bool {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	if withinCanonicalDir(dir, target) {
		return true
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() {
		return false
	}
	cur := filepath.Clean(target)
	for {
		if info, lerr := os.Lstat(cur); lerr == nil && os.SameFile(dirInfo, info) {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return false
}

func pathStrictlyInsideDirSameFS(path, dir string) bool {
	return !sameFileOrPath(path, dir) && pathWithinDirSameFS(dir, path)
}

func pathStrictlyInsideAnySameFS(path string, dirs []string) bool {
	for _, dir := range dirs {
		if pathStrictlyInsideDirSameFS(path, dir) {
			return true
		}
	}
	return false
}

func readPolystrataRuntimeReceiptSHA256(dir string) (string, bool) {
	receiptPath := filepath.Join(dir, ".inferyard-runtime.json")
	info, err := os.Lstat(receiptPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
		return "", false
	}
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		return "", false
	}
	var receipt struct {
		Definition    string `json:"definition"`
		ArchiveSHA256 string `json:"archive_sha256"`
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Definition != polystrataRuntimeDefinition {
		return "", false
	}
	return strings.TrimSpace(receipt.ArchiveSHA256), true
}

func hasPolystrataRuntimeReceipt(dir string) bool {
	_, ok := readPolystrataRuntimeReceiptSHA256(dir)
	return ok
}

func managedPolystrataInstallDir(rawInstPath, canonInstPath, wsEnginesRoot string, resultsRoot *string) (string, bool) {
	if strings.TrimSpace(wsEnginesRoot) == "" || canonInstPath == "" || !filepath.IsAbs(canonInstPath) {
		return "", false
	}
	if isSymlinkPath(rawInstPath) || isSymlinkPath(canonInstPath) {
		return "", false
	}
	if rawDir := filepath.Dir(strings.TrimSpace(rawInstPath)); rawDir != "" && isSymlinkPath(rawDir) {
		return "", false
	}
	canonWS, err := canonical(wsEnginesRoot)
	if err != nil || !filepath.IsAbs(canonWS) {
		return "", false
	}
	installDir := filepath.Dir(canonInstPath)
	if sameFileOrPath(installDir, canonWS) || !sameFileOrPath(filepath.Dir(installDir), canonWS) {
		return "", false
	}
	dirInfo, err := os.Lstat(installDir)
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return "", false
	}
	if overlapsResultsRoot(installDir, resultsRoot) || overlapsResultsRoot(canonInstPath, resultsRoot) {
		return "", false
	}
	if !hasPolystrataRuntimeReceipt(installDir) {
		return "", false
	}
	return installDir, true
}

func defaultManagedPolystrataDataDirs(u UserSettings, paths *BoundPaths, wsEnginesRoot string) []string {
	var out []string
	resultsRoot := derivedResultsRoot(u, paths)
	tryAdd := func(parent, name string) {
		if strings.TrimSpace(parent) == "" {
			return
		}
		canonParent, err := canonical(parent)
		if err != nil || !filepath.IsAbs(canonParent) {
			return
		}
		cand := filepath.Join(canonParent, name)
		info, err := os.Lstat(cand)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return
		}
		canonCand, err := canonical(cand)
		if err != nil || !pathStrictlyInsideDirSameFS(canonCand, canonParent) || overlapsResultsRoot(canonCand, resultsRoot) {
			return
		}
		for _, prev := range out {
			if sameFileOrPath(prev, canonCand) {
				return
			}
		}
		out = append(out, canonCand)
	}
	if models := derivedModelsRoot(u, paths); models != nil {
		tryAdd(*models, "polystrata-data")
	}
	tryAdd(wsEnginesRoot, "PolyStrata-data")
	tryAdd(wsEnginesRoot, "polystrata-data")
	return out
}

func polystrataManagedScopeFor(u UserSettings, paths *BoundPaths, wsEnginesRoot string) polystrataManagedScope {
	resultsRoot := derivedResultsRoot(u, paths)
	scope := polystrataManagedScope{
		dataDirs: defaultManagedPolystrataDataDirs(u, paths, wsEnginesRoot),
	}
	appendUnique := func(list *[]string, cand string) {
		for _, prev := range *list {
			if sameFileOrPath(prev, cand) {
				return
			}
		}
		*list = append(*list, cand)
	}
	for _, rawInst := range u.EngineInstallations {
		if !filepath.IsAbs(rawInst) || strings.ContainsRune(rawInst, 0) {
			continue
		}
		rawBytes, err := installationBytes(rawInst)
		if err != nil {
			continue
		}
		var decl struct {
			Family  string `json:"family"`
			DataDir string `json:"data_dir"`
		}
		if json.Unmarshal(rawBytes, &decl) != nil || decl.Family != "polystrata" {
			continue
		}
		canonInst, err := canonical(rawInst)
		if err != nil {
			continue
		}
		installDir, ok := managedPolystrataInstallDir(rawInst, canonInst, wsEnginesRoot, resultsRoot)
		if !ok {
			continue
		}
		appendUnique(&scope.installDirs, installDir)
		dd := strings.TrimSpace(decl.DataDir)
		if dd == "" || isSymlinkPath(dd) {
			continue
		}
		ddInfo, err := os.Lstat(dd)
		if err != nil || ddInfo.Mode()&os.ModeSymlink != 0 || !ddInfo.IsDir() {
			continue
		}
		canonDD, err := canonical(dd)
		if err != nil || !filepath.IsAbs(canonDD) || overlapsResultsRoot(canonDD, resultsRoot) {
			continue
		}
		if pathStrictlyInsideDirSameFS(canonDD, installDir) {
			appendUnique(&scope.dataDirs, canonDD)
		}
	}
	return scope
}

func isManagedPolystrataModelDeletable(rawPath, canonPath string, scope polystrataManagedScope, resultsRoot *string) bool {
	if isSymlinkPath(rawPath) || isSymlinkPath(canonPath) || overlapsResultsRoot(canonPath, resultsRoot) {
		return false
	}
	return pathStrictlyInsideAnySameFS(canonPath, scope.installDirs) || pathStrictlyInsideAnySameFS(canonPath, scope.dataDirs)
}

func isEngineAssetDeletable(u UserSettings, paths *BoundPaths, rawPath, canonPath, kind, family, wsEnginesRoot string, resultsRoot *string, engines []registryEntry) bool {
	if kind != "local_installation" {
		return isEngineDeletable(rawPath, canonPath, kind, wsEnginesRoot, resultsRoot)
	}
	if family != "polystrata" {
		installDir, _, ok := managedVenvInstallDir(rawPath, canonPath, wsEnginesRoot, resultsRoot)
		return ok && !managedVenvHasExternalDependents(installDir, canonPath, engines)
	}
	installDir, ok := managedPolystrataInstallDir(rawPath, canonPath, wsEnginesRoot, resultsRoot)
	if !ok {
		return false
	}
	return !polystrataInstallHasExternalDependents(u, paths, wsEnginesRoot, canonPath, installDir, engines)
}

func otherEngineSharesInstallDir(installDir, selfCanonPath string, engines []registryEntry) bool {
	for _, other := range engines {
		if other.path == "" || sameDirectoryEntry(other.path, selfCanonPath) {
			continue
		}
		if otherCanon, err := canonical(other.path); err == nil && pathWithinDirSameFS(installDir, otherCanon) {
			return true
		}
	}
	return false
}

func (s *Service) findAssetRawPath(kind, name, canonPath string) string {
	data := s.registry()
	entries := data.models
	if kind == "engine" {
		entries = data.engines
	}
	for _, e := range entries {
		if (kind == "engine" && e.engine.Name == name) || (kind == "model" && e.model.Name == name) {
			if e.rawPath != "" {
				return e.rawPath
			}
			return e.path
		}
	}
	return canonPath
}

func workspaceBusyError() *ApiError {
	err := Conflict("workspace_busy")
	err.Message = l10n.TR("core.asset.err.workspace_busy")
	return err
}

var removeStagedPolystrataDir = os.RemoveAll

func (s *Service) deleteManagedPolystrataEngine(name string, eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	unregistered, err := s.runDeleteManagedPolystrataEngine(name, eng, expectedTarget, expectedFP)
	if unregistered || err == nil {
		s.VerifyEngines()
		_ = s.RefreshAssets()
	}
	return err
}

func (s *Service) runDeleteManagedPolystrataEngine(name string, eng EngineAsset, expectedTarget, expectedFP string) (bool, *ApiError) {
	return s.deleteManagedPolystrataInstall(name, eng.Path, "", expectedTarget, expectedFP, nil)
}

func (s *Service) deleteManagedPolystrataInstall(name, engPath, rawFallback, expectedTarget, expectedFP string, replaced *replacedEngineTarget) (bool, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	s.mu.Lock()
	if replaced != nil {
		if s.deletingAssets > 0 || s.isEngineActiveLocked(replaced.name, replaced.entry) || s.isEngineDirInUseLocked(replaced.dir) {
			s.mu.Unlock()
			return false, workspaceBusyError()
		}
	} else if s.assetWorkspaceBusyLocked() {
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

	rawPath := ""
	for _, rawInst := range u.EngineInstallations {
		if canonInst, err := canonical(rawInst); err == nil && sameDirectoryEntry(canonInst, engPath) {
			rawPath = rawInst
			break
		}
	}
	if rawPath == "" {
		rawPath = rawFallback
	}
	if rawPath == "" {
		rawPath = s.findAssetRawPath("engine", name, engPath)
	}
	if rawPath == "" {
		return false, Err("not_found")
	}
	wsEngines := workspaceEnginesRoot(u, paths)
	resultsRoot := derivedResultsRoot(u, paths)
	installDir, ok := managedPolystrataInstallDir(rawPath, engPath, wsEngines, resultsRoot)
	if !ok {
		return false, errAssetNotDeletable()
	}
	data := s.registry()
	if polystrataInstallHasExternalDependents(u, paths, wsEngines, engPath, installDir, data.engines) {
		return false, errAssetNotDeletable()
	}
	if err := verifyPolystrataRuntimeTreeSafe(installDir, resultsRoot); err != nil {
		return false, errAssetNotDeletable()
	}
	declWorkDir, declDataDir := readPolystrataInstallDirs(engPath)
	surviving := survivingInstallsExcluding(u, wsEngines, engPath, installDir)
	migPlans, migErr := snapshotPolystrataRootConfigs(installDir, declWorkDir, declDataDir, defaultManagedPolystrataDataDirs(u, paths, wsEngines), surviving)
	if migErr != nil {
		if apiErr, ok := polystrataCredentialApiError(migErr); ok {
			return false, apiErr
		}
		return false, Err("internal_error")
	}
	if expectedTarget != "" {
		if !sameFileOrPath(installDir, expectedTarget) {
			return false, deleteTargetChangedError()
		}
		if expectedFP != "" && polystrataEngineDeleteFingerprint(installDir, engPath, migPlans) != expectedFP {
			return false, deleteTargetChangedError()
		}
	}

	removeRaw := map[string]bool{}
	for _, rawInst := range u.EngineInstallations {
		if canonInst, err := canonical(rawInst); err == nil {
			if sameDirectoryEntry(canonInst, engPath) || pathWithinDirSameFS(installDir, canonInst) {
				removeRaw[rawInst] = true
			}
		} else if sameDirectoryEntry(rawInst, rawPath) || sameDirectoryEntry(rawInst, engPath) {
			removeRaw[rawInst] = true
		}
	}

	stagedDir := filepath.Join(filepath.Dir(installDir), ".inferyard-uninstall-"+filepath.Base(installDir)+"-"+uuid())
	if err := os.Rename(installDir, stagedDir); err != nil {
		return false, Err("internal_error")
	}
	if err := checkDirTreeWritable(stagedDir); err != nil {
		_ = os.Rename(stagedDir, installDir)
		return false, Err("internal_error")
	}

	migratedPaths, migErr := writeMigratedPolystrataConfigs(migPlans)
	if migErr != nil {
		_ = os.Rename(stagedDir, installDir)
		return false, Err("internal_error")
	}

	if len(removeRaw) > 0 {
		nextU := clone(u)
		nextU.EngineInstallations = slices.DeleteFunc(slices.Clone(nextU.EngineInstallations), func(rawInst string) bool {
			return removeRaw[rawInst]
		})
		if _, saveErr := s.saveSettings(nextU); saveErr != nil {
			for i := len(migratedPaths) - 1; i >= 0; i-- {
				_ = os.Remove(migratedPaths[i])
			}
			_ = os.Rename(stagedDir, installDir)
			return false, saveErr
		}
	}

	rmErr := removeStagedPolystrataDir(stagedDir)

	s.mu.Lock()
	s.pruneAssetSnapshotLocked("engine", installDir)
	s.pruneAssetSnapshotLocked("model", installDir)
	s.mu.Unlock()

	s.checkMu.Lock()
	for path := range s.checks {
		if sameDirectoryEntry(path, engPath) || pathWithinDirSameFS(installDir, path) {
			delete(s.checks, path)
		}
	}
	for path := range s.checkInflight {
		if sameDirectoryEntry(path, engPath) || pathWithinDirSameFS(installDir, path) {
			delete(s.checkInflight, path)
		}
	}
	s.checkNames = slices.DeleteFunc(s.checkNames, func(t engineCheckTarget) bool {
		return sameDirectoryEntry(t.path, engPath) || pathWithinDirSameFS(installDir, t.path)
	})
	s.checkMu.Unlock()

	if rmErr != nil && !os.IsNotExist(rmErr) {
		apiErr := Err("internal_error")
		apiErr.Message = l10n.TR("core.asset.err.polystrata_staged_cleanup_failed")
		return true, apiErr
	}
	return true, nil
}
