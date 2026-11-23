package core

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const polystrataRuntimeDefinition = "polystrata-runtime.v1"

// AssetDeleteInfo describes the validated target and disk impact of deleting an asset.
type AssetDeleteInfo struct {
	Kind              string
	Name              string
	TargetPath        string
	ReclaimableBytes  *int64
	Scope             string
	PreservedShared   bool
	PreservedExternal bool
	KeepsModelData    bool
}

type polystrataManagedScope struct {
	installDirs []string
	dataDirs    []string
}

func hasPolystrataRuntimeReceipt(dir string) bool {
	receiptPath := filepath.Join(dir, ".inferyard-runtime.json")
	info, err := os.Lstat(receiptPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
		return false
	}
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		return false
	}
	var receipt struct {
		Definition string `json:"definition"`
	}
	return json.Unmarshal(raw, &receipt) == nil && receipt.Definition == polystrataRuntimeDefinition
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
	if installDir == canonWS || filepath.Dir(installDir) != canonWS {
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

func polystrataManagedScopeFor(u UserSettings, wsEnginesRoot string) polystrataManagedScope {
	scope := polystrataManagedScope{}
	modelRoots := activeModelRoots(u)
	canonWS := ""
	if strings.TrimSpace(wsEnginesRoot) != "" {
		if c, err := canonical(wsEnginesRoot); err == nil && filepath.IsAbs(c) {
			canonWS = c
		}
	}
	seenInstall := map[string]bool{}
	seenData := map[string]bool{}
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
		installDir, ok := managedPolystrataInstallDir(rawInst, canonInst, wsEnginesRoot, u.ResultsRoot)
		if !ok {
			continue
		}
		if !seenInstall[installDir] {
			seenInstall[installDir] = true
			scope.installDirs = append(scope.installDirs, installDir)
		}
		dd := strings.TrimSpace(decl.DataDir)
		if dd == "" {
			dd = existingPolystrataDataDir(installDir)
		}
		if dd == "" || isSymlinkPath(dd) {
			continue
		}
		ddInfo, err := os.Lstat(dd)
		if err != nil || ddInfo.Mode()&os.ModeSymlink != 0 || !ddInfo.IsDir() {
			continue
		}
		canonDD, err := canonical(dd)
		if err != nil || !filepath.IsAbs(canonDD) || overlapsResultsRoot(canonDD, u.ResultsRoot) {
			continue
		}
		_, inModelRoot := strictlyInsideAnyRoot(canonDD, modelRoots)
		inWSEngines := canonWS != "" && pathStrictlyInsideDir(canonDD, canonWS)
		if (inModelRoot || inWSEngines) && !seenData[canonDD] {
			seenData[canonDD] = true
			scope.dataDirs = append(scope.dataDirs, canonDD)
		}
	}
	return scope
}

func pathStrictlyInsideAny(canonPath string, dirs []string) bool {
	for _, dir := range dirs {
		if pathStrictlyInsideDir(canonPath, dir) {
			return true
		}
	}
	return false
}

func isManagedPolystrataModelDeletable(rawPath, canonPath string, scope polystrataManagedScope, resultsRoot *string) bool {
	if isSymlinkPath(rawPath) || isSymlinkPath(canonPath) {
		return false
	}
	if overlapsResultsRoot(canonPath, resultsRoot) {
		return false
	}
	return pathStrictlyInsideAny(canonPath, scope.installDirs) || pathStrictlyInsideAny(canonPath, scope.dataDirs)
}

func isEngineAssetDeletable(rawPath, canonPath, kind, family, wsEnginesRoot string, resultsRoot *string, engines []registryEntry) bool {
	if kind != "local_installation" {
		return isEngineDeletable(rawPath, canonPath, kind, wsEnginesRoot, resultsRoot)
	}
	if family != "polystrata" {
		return false
	}
	installDir, ok := managedPolystrataInstallDir(rawPath, canonPath, wsEnginesRoot, resultsRoot)
	if !ok {
		return false
	}
	return !otherEngineSharesInstallDir(installDir, canonPath, engines)
}

func otherEngineSharesInstallDir(installDir, selfCanonPath string, engines []registryEntry) bool {
	for _, other := range engines {
		if other.path == "" || other.path == selfCanonPath {
			continue
		}
		if otherCanon, err := canonical(other.path); err == nil && withinCanonicalDir(installDir, otherCanon) {
			return true
		}
	}
	return false
}

// isAllowedPolystrataVenvSymlink allows python* interpreter symlinks created inside a genuine
// non-symlink .venv/bin or .venv/Scripts directory of a managed PolyStrata runtime, while keeping
// all other escaping symlinks rejected.
func isAllowedPolystrataVenvSymlink(canonInstallDir, linkPath string, resultsRoot *string) bool {
	venvDir := filepath.Join(canonInstallDir, ".venv")
	venvInfo, err := os.Lstat(venvDir)
	if err != nil || venvInfo.Mode()&os.ModeSymlink != 0 || !venvInfo.IsDir() {
		return false
	}
	if !isRegularNonSymlink(filepath.Join(venvDir, "pyvenv.cfg")) {
		return false
	}
	parent := filepath.Dir(linkPath)
	if parent != filepath.Join(venvDir, "bin") && parent != filepath.Join(venvDir, "Scripts") {
		return false
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Base(linkPath)), "python") {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(linkPath); err == nil {
		if isSealedEvidenceMarker(filepath.Base(resolved)) || overlapsResultsRoot(resolved, resultsRoot) {
			return false
		}
		if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
			return false
		}
	}
	return true
}

func verifyPolystrataRuntimeTreeSafe(canonInstallDir string, resultsRoot *string) error {
	canonRoot, err := canonical(canonInstallDir)
	if err != nil {
		return errUnsafeAssetTree
	}
	return filepath.WalkDir(canonRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if isAllowedPolystrataVenvSymlink(canonRoot, path, resultsRoot) {
				return nil
			}
			resolved, linkErr := filepath.EvalSymlinks(path)
			if linkErr != nil || !withinCanonicalDir(canonRoot, resolved) || isSealedEvidenceMarker(filepath.Base(resolved)) || overlapsResultsRoot(resolved, resultsRoot) {
				return errUnsafeAssetTree
			}
			return nil
		}
		if !d.IsDir() && isSealedEvidenceMarker(d.Name()) {
			return errUnsafeAssetTree
		}
		return nil
	})
}

func dirDiskUsageNoSymlinks(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.Type()&os.ModeSymlink != 0 {
			if d != nil && d.Type()&os.ModeSymlink != 0 && d.IsDir() {
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

func polystrataInstallKeepsExternalData(canonInstPath, installDir string) bool {
	dd := ""
	if rawBytes, err := installationBytes(canonInstPath); err == nil {
		var decl struct {
			DataDir string `json:"data_dir"`
		}
		if json.Unmarshal(rawBytes, &decl) == nil {
			dd = strings.TrimSpace(decl.DataDir)
		}
	}
	if dd == "" {
		dd = existingPolystrataDataDir(installDir)
	}
	if dd == "" {
		return false
	}
	canonDD, err := canonical(dd)
	if err != nil {
		return false
	}
	info, err := os.Lstat(canonDD)
	return err == nil && info.IsDir() && !withinCanonicalDir(installDir, canonDD)
}

// InspectDeleteAsset previews the deletion scope and reclaimable disk size without mutating state.
func (s *Service) InspectDeleteAsset(kind, name string) (AssetDeleteInfo, *ApiError) {
	if (kind != "model" && kind != "engine") || strings.TrimSpace(name) == "" {
		return AssetDeleteInfo{}, Err("invalid_request")
	}
	s.mu.Lock()
	u := clone(s.settings)
	paths := s.paths
	s.mu.Unlock()

	reg := s.Registry()
	if kind == "engine" {
		return s.inspectDeleteEngineAsset(u, paths, reg, name)
	}
	return s.inspectDeleteModelAsset(u, paths, reg, name)
}

func (s *Service) inspectDeleteEngineAsset(u UserSettings, paths *BoundPaths, reg Registry, name string) (AssetDeleteInfo, *ApiError) {
	for _, eng := range reg.Engines {
		if eng.Name != name {
			continue
		}
		if !eng.Deletable {
			return AssetDeleteInfo{}, errAssetNotDeletable()
		}
		if eng.Kind == "local_installation" && eng.Family == "polystrata" {
			rawPath := s.findAssetRawPath("engine", name, eng.Path)
			wsEngines := workspaceEnginesRoot(u, paths)
			installDir, ok := managedPolystrataInstallDir(rawPath, eng.Path, wsEngines, u.ResultsRoot)
			if !ok {
				return AssetDeleteInfo{}, errAssetNotDeletable()
			}
			if data := s.registry(); otherEngineSharesInstallDir(installDir, eng.Path, data.engines) {
				return AssetDeleteInfo{}, errAssetNotDeletable()
			}
			return AssetDeleteInfo{
				Kind:             "engine",
				Name:             name,
				TargetPath:       installDir,
				ReclaimableBytes: ptr(dirDiskUsageNoSymlinks(installDir)),
				Scope:            "polystrata_engine",
				KeepsModelData:   polystrataInstallKeepsExternalData(eng.Path, installDir),
			}, nil
		}
		target := eng.Path
		if wsEngines := workspaceEnginesRoot(u, paths); wsEngines != "" {
			if canonWS, err := canonical(wsEngines); err == nil {
				target = resolveEngineDeleteTarget(eng.Path, canonWS, name, reg.Engines)
			}
		}
		var sizePtr *int64
		if info, err := os.Lstat(target); err == nil {
			if info.IsDir() {
				sizePtr = ptr(dirDiskUsageNoSymlinks(target))
			} else if info.Mode().IsRegular() {
				sizePtr = ptr(info.Size())
			}
		}
		return AssetDeleteInfo{
			Kind:             "engine",
			Name:             name,
			TargetPath:       target,
			ReclaimableBytes: sizePtr,
			Scope:            "standard",
		}, nil
	}
	return AssetDeleteInfo{}, Err("not_found")
}

func (s *Service) inspectDeleteModelAsset(u UserSettings, paths *BoundPaths, reg Registry, name string) (AssetDeleteInfo, *ApiError) {
	for _, m := range reg.Models {
		if m.Name != name {
			continue
		}
		if !m.Deletable {
			return AssetDeleteInfo{}, errAssetNotDeletable()
		}
		if m.Format == "strata_pack" {
			rawPath := s.findAssetRawPath("model", name, m.Path)
			plan, err := planPolystrataModelDelete(u, workspaceEnginesRoot(u, paths), rawPath, m.Path)
			if err != nil {
				return AssetDeleteInfo{}, err
			}
			scope := "polystrata_model_config_only"
			if len(plan.files) > 0 || len(plan.dirs) > 0 {
				scope = "polystrata_model"
			}
			return AssetDeleteInfo{
				Kind:              "model",
				Name:              name,
				TargetPath:        plan.configPath,
				ReclaimableBytes:  ptr(plan.reclaimableBytes),
				Scope:             scope,
				PreservedShared:   plan.preservedShared,
				PreservedExternal: plan.preservedExternal,
			}, nil
		}
		return AssetDeleteInfo{
			Kind:             "model",
			Name:             name,
			TargetPath:       m.Path,
			ReclaimableBytes: clone(m.SizeBytes),
			Scope:            "standard",
		}, nil
	}
	return AssetDeleteInfo{}, Err("not_found")
}

func workspaceBusyError() *ApiError {
	err := Conflict("workspace_busy")
	err.Message = "当前有任务、下载或模型服务正在运行，请停止后再删除。"
	return err
}

func (s *Service) deleteManagedPolystrataEngine(u UserSettings, paths *BoundPaths, name string, eng EngineAsset) *ApiError {
	rawPath := s.findAssetRawPath("engine", name, eng.Path)
	wsEngines := workspaceEnginesRoot(u, paths)
	installDir, ok := managedPolystrataInstallDir(rawPath, eng.Path, wsEngines, u.ResultsRoot)
	if !ok {
		return errAssetNotDeletable()
	}
	data := s.registry()
	if otherEngineSharesInstallDir(installDir, eng.Path, data.engines) {
		return errAssetNotDeletable()
	}
	for _, rawInst := range u.EngineInstallations {
		canonInst, err := canonical(rawInst)
		if err != nil {
			continue
		}
		if canonInst != eng.Path && withinCanonicalDir(installDir, canonInst) {
			return errAssetNotDeletable()
		}
	}
	if err := verifyPolystrataRuntimeTreeSafe(installDir, u.ResultsRoot); err != nil {
		return errAssetNotDeletable()
	}

	s.settingsMu.Lock()
	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		s.settingsMu.Unlock()
		return workspaceBusyError()
	}
	prevU := clone(s.settings)
	nextU := clone(s.settings)
	s.mu.Unlock()

	nextU.EngineInstallations = slices.DeleteFunc(slices.Clone(nextU.EngineInstallations), func(rawInst string) bool {
		if canonInst, err := canonical(rawInst); err == nil {
			return canonInst == eng.Path || withinCanonicalDir(installDir, canonInst)
		}
		return filepath.Clean(rawInst) == filepath.Clean(rawPath) || filepath.Clean(rawInst) == eng.Path
	})
	if _, saveErr := s.saveSettings(nextU); saveErr != nil {
		s.settingsMu.Unlock()
		return saveErr
	}

	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		_, _ = s.saveSettings(prevU)
		s.settingsMu.Unlock()
		return workspaceBusyError()
	}
	removeErr := os.RemoveAll(installDir)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		s.mu.Unlock()
		if isRegularNonSymlink(eng.Path) {
			_, _ = s.saveSettings(prevU)
		}
		s.settingsMu.Unlock()
		return Err("internal_error")
	}
	s.pruneAssetSnapshotLocked("engine", installDir)
	s.pruneAssetSnapshotLocked("model", installDir)
	s.mu.Unlock()
	s.settingsMu.Unlock()

	s.checkMu.Lock()
	delete(s.checks, eng.Path)
	delete(s.checkInflight, eng.Path)
	s.checkMu.Unlock()
	s.VerifyEngines()

	_ = s.RefreshAssets()
	return nil
}
