package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (s *Service) deleteManagedLocalInstallationEngine(name string, eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	if eng.Family == "polystrata" {
		return s.deleteManagedPolystrataEngine(name, eng, expectedTarget, expectedFP)
	}
	if eng.InstallMode == managedEngineVenvInstallMode {
		return s.deleteManagedVenvEngine(eng, expectedTarget, expectedFP)
	}
	return errAssetNotDeletable()
}

func managedVenvHasExternalDependents(installDir, selfCanonPath string, engines []registryEntry) bool {
	if otherEngineSharesInstallDir(installDir, selfCanonPath, engines) {
		return true
	}
	touchesInstallDir := func(raw string) bool {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return false
		}
		if pathWithinDirSameFS(installDir, trimmed) {
			return true
		}
		if canon, err := canonical(trimmed); err == nil && pathWithinDirSameFS(installDir, canon) {
			return true
		}
		return false
	}
	for _, other := range engines {
		if other.installation == "" || sameDirectoryEntry(other.path, selfCanonPath) {
			continue
		}
		raw, err := installationBytes(other.installation)
		if err != nil {
			continue
		}
		var decl struct {
			Executable       string   `json:"executable"`
			WorkingDirectory string   `json:"working_directory"`
			RuntimeRoots     []string `json:"runtime_roots"`
		}
		if json.Unmarshal(raw, &decl) != nil {
			continue
		}
		if touchesInstallDir(decl.Executable) || touchesInstallDir(decl.WorkingDirectory) {
			return true
		}
		for _, root := range decl.RuntimeRoots {
			if touchesInstallDir(root) {
				return true
			}
		}
	}
	return false
}

func managedVenvInstallDir(rawInstPath, canonInstPath, wsEnginesRoot string, resultsRoot *string) (string, managedEngineReceipt, bool) {
	empty := managedEngineReceipt{}
	if wsEnginesRoot == "" || filepath.Base(canonInstPath) != "installation.json" ||
		isSymlinkPath(rawInstPath) || isSymlinkPath(canonInstPath) || isSymlinkPath(filepath.Dir(rawInstPath)) {
		return "", empty, false
	}
	root, err := canonical(wsEnginesRoot)
	if err != nil {
		return "", empty, false
	}
	installDir := filepath.Dir(canonInstPath)
	if sameFileOrPath(installDir, root) || !sameFileOrPath(filepath.Dir(installDir), root) {
		return "", empty, false
	}
	info, err := os.Lstat(installDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		overlapsResultsRoot(installDir, resultsRoot) {
		return "", empty, false
	}
	receipt, ok := readManagedEngineReceipt(installDir)
	if !ok || receipt.InstallMode != managedEngineVenvInstallMode || receipt.Version == "" {
		return "", empty, false
	}
	raw, err := installationBytes(canonInstPath)
	if err != nil {
		return "", empty, false
	}
	var declaration struct {
		Family string `json:"family"`
	}
	if json.Unmarshal(raw, &declaration) != nil || declaration.Family != receipt.Family {
		return "", empty, false
	}
	return installDir, receipt, true
}

func verifyManagedVenvTreeSafe(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && isSealedEvidenceMarker(entry.Name()) {
			return errUnsafeAssetTree
		}
		if entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func (s *Service) inspectManagedVenvEngineDelete(u UserSettings, paths *BoundPaths, eng EngineAsset) (AssetDeleteInfo, *ApiError) {
	rawPath := s.findAssetRawPath("engine", eng.Name, eng.Path)
	installDir, _, ok := managedVenvInstallDir(rawPath, eng.Path, workspaceEnginesRoot(u, paths), derivedResultsRoot(u, paths))
	if !ok || verifyManagedVenvTreeSafe(installDir) != nil {
		return AssetDeleteInfo{}, errAssetNotDeletable()
	}
	if otherEngineSharesInstallDir(installDir, eng.Path, s.registry().engines) ||
		managedVenvHasExternalDependents(installDir, eng.Path, s.registry().engines) {
		return AssetDeleteInfo{}, errAssetNotDeletable()
	}
	return AssetDeleteInfo{
		Kind: "engine", Name: eng.Name, TargetPath: installDir,
		Fingerprint:      standardAssetDeleteFingerprint("managed_venv_engine", installDir),
		ReclaimableBytes: ptr(reclaimableDiskBytes([]string{installDir}, nil)), Scope: "managed_venv_engine",
	}, nil
}

var removeStagedManagedEngineDir = os.RemoveAll

func (s *Service) deleteManagedVenvEngine(eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	failure := s.runDeleteManagedVenvEngine(eng, expectedTarget, expectedFP)
	if failure == nil {
		s.VerifyEngines()
		_ = s.RefreshAssets()
		s.publishEngineUpdatesEvent(false)
	}
	return failure
}

func (s *Service) runDeleteManagedVenvEngine(eng EngineAsset, expectedTarget, expectedFP string) *ApiError {
	rawPath := s.findAssetRawPath("engine", eng.Name, eng.Path)
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		return workspaceBusyError()
	}
	s.deletingAssets++
	u, paths := clone(s.settings), s.paths
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.deletingAssets--
		s.mu.Unlock()
	}()

	wsEngines := workspaceEnginesRoot(u, paths)
	installDir, _, ok := managedVenvInstallDir(rawPath, eng.Path, wsEngines, derivedResultsRoot(u, paths))
	if !ok || managedVenvHasExternalDependents(installDir, eng.Path, s.registry().engines) || verifyManagedVenvTreeSafe(installDir) != nil {
		return errAssetNotDeletable()
	}
	if expectedTarget != "" && (!sameFileOrPath(installDir, expectedTarget) ||
		expectedFP != "" && standardAssetDeleteFingerprint("managed_venv_engine", installDir) != expectedFP) {
		return deleteTargetChangedError()
	}
	removeRaw := map[string]bool{}
	for _, rawInst := range u.EngineInstallations {
		if canon, err := canonical(rawInst); err == nil && pathWithinDirSameFS(installDir, canon) {
			removeRaw[rawInst] = true
		}
	}
	stagedDir := filepath.Join(filepath.Dir(installDir), ".inferyard-uninstall-"+filepath.Base(installDir)+"-"+uuid())
	if err := os.Rename(installDir, stagedDir); err != nil {
		return Err("internal_error")
	}
	nextU := clone(u)
	nextU.EngineInstallations = slices.DeleteFunc(slices.Clone(nextU.EngineInstallations), func(raw string) bool { return removeRaw[raw] })
	if len(removeRaw) > 0 {
		if _, saveErr := s.saveSettings(nextU); saveErr != nil {
			_ = os.Rename(stagedDir, installDir)
			return saveErr
		}
	}
	removeErr := removeStagedManagedEngineDir(stagedDir)
	s.mu.Lock()
	s.pruneAssetSnapshotLocked("engine", installDir)
	s.mu.Unlock()
	s.checkMu.Lock()
	for path := range s.checks {
		if pathWithinDirSameFS(installDir, path) {
			delete(s.checks, path)
		}
	}
	for path := range s.checkInflight {
		if pathWithinDirSameFS(installDir, path) {
			delete(s.checkInflight, path)
		}
	}
	s.checkMu.Unlock()
	if removeErr != nil && !os.IsNotExist(removeErr) {
		apiErr := Err("internal_error")
		apiErr.Message = l10n.TR("core.asset.err.polystrata_staged_cleanup_failed")
		return apiErr
	}
	return nil
}
