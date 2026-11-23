package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

const (
	polystrataRuntimeDefinition  = "polystrata-runtime.v1"
	polystrataPinnedSamplePath   = "bench/results/2026-10-09-community-gfx1151-ud-iq4xs-0.1.41/raw/events.jsonl"
	polystrataPinnedSampleBytes  = int64(9150)
	polystrataPinnedSampleSHA256 = "27c7f41b753d89d170cd2d1ed7af449e3cf1e8794c6ca6bb937a6ec3734540e5"
)

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

type polystrataSampleSpec struct {
	archiveSHA string
	relPath    string
	size       int64
	sha256     string
}

var knownPolystrataSamples = []polystrataSampleSpec{{
	archiveSHA: polystrataDefaultSourceSHA256,
	relPath:    polystrataPinnedSamplePath,
	size:       polystrataPinnedSampleBytes,
	sha256:     polystrataPinnedSampleSHA256,
}}

func isKnownPolystrataArchiveSample(archiveSHA, canonRoot, fullPath string, d fs.DirEntry) bool {
	if archiveSHA == "" || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
		return false
	}
	rel, err := filepath.Rel(canonRoot, fullPath)
	if err != nil {
		return false
	}
	slashRel := filepath.ToSlash(rel)
	info, err := d.Info()
	if err != nil {
		return false
	}
	for _, spec := range knownPolystrataSamples {
		if archiveSHA != spec.archiveSHA || slashRel != spec.relPath || info.Size() != spec.size {
			continue
		}
		f, err := os.Open(fullPath)
		if err != nil {
			return false
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(f, spec.size+1))
		_ = f.Close()
		if copyErr == nil && n == spec.size && hex.EncodeToString(h.Sum(nil)) == spec.sha256 {
			return true
		}
	}
	return false
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

func defaultManagedPolystrataDataDirs(u UserSettings, wsEnginesRoot string) []string {
	var out []string
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
		if err != nil || !pathStrictlyInsideDirSameFS(canonCand, canonParent) || overlapsResultsRoot(canonCand, u.ResultsRoot) {
			return
		}
		for _, prev := range out {
			if sameFileOrPath(prev, canonCand) {
				return
			}
		}
		out = append(out, canonCand)
	}
	if u.ModelsRoot != nil {
		tryAdd(*u.ModelsRoot, "polystrata-data")
	}
	tryAdd(wsEnginesRoot, "PolyStrata-data")
	tryAdd(wsEnginesRoot, "polystrata-data")
	return out
}

func polystrataManagedScopeFor(u UserSettings, wsEnginesRoot string) polystrataManagedScope {
	scope := polystrataManagedScope{
		dataDirs: defaultManagedPolystrataDataDirs(u, wsEnginesRoot),
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
		installDir, ok := managedPolystrataInstallDir(rawInst, canonInst, wsEnginesRoot, u.ResultsRoot)
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
		if err != nil || !filepath.IsAbs(canonDD) || overlapsResultsRoot(canonDD, u.ResultsRoot) {
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

func isEngineAssetDeletable(u UserSettings, rawPath, canonPath, kind, family, wsEnginesRoot string, resultsRoot *string, engines []registryEntry) bool {
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
	return !polystrataInstallHasExternalDependents(u, wsEnginesRoot, canonPath, installDir, engines)
}

func otherEngineSharesInstallDir(installDir, selfCanonPath string, engines []registryEntry) bool {
	for _, other := range engines {
		if other.path == "" || sameFileOrPath(other.path, selfCanonPath) {
			continue
		}
		if otherCanon, err := canonical(other.path); err == nil && pathWithinDirSameFS(installDir, otherCanon) {
			return true
		}
	}
	return false
}

// isAllowedPolystrataVenvSymlink permits standard python* interpreter symlinks inside a genuine
// non-symlink .venv/bin or .venv/Scripts directory of a managed PolyStrata runtime, while rejecting
// unrelated or arbitrary external targets.
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
	if !polystrataVenvPythonName.MatchString(filepath.Base(linkPath)) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		rawTarget, rerr := os.Readlink(linkPath)
		if rerr != nil {
			return false
		}
		targetBase := filepath.Base(rawTarget)
		return polystrataPythonTargetName.MatchString(targetBase) && !isSealedEvidenceMarker(targetBase)
	}
	if isSealedEvidenceMarker(filepath.Base(resolved)) || overlapsResultsRoot(resolved, resultsRoot) {
		return false
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil || !info.Mode().IsRegular() {
		return false
	}
	return polystrataPythonTargetName.MatchString(filepath.Base(resolved))
}

func verifyPolystrataRuntimeTreeSafe(canonInstallDir string, resultsRoot *string) error {
	canonRoot, err := canonical(canonInstallDir)
	if err != nil {
		return errUnsafeAssetTree
	}
	archiveSHA, _ := readPolystrataRuntimeReceiptSHA256(canonRoot)
	return filepath.WalkDir(canonRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && isSealedEvidenceMarker(d.Name()) {
			if d.Type()&os.ModeSymlink == 0 && isKnownPolystrataArchiveSample(archiveSHA, canonRoot, path, d) {
				return nil
			}
			return errUnsafeAssetTree
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
		return nil
	})
}

func checkDirTreeWritable(root string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if info.Mode().Perm()&0o300 != 0o300 {
				return errUnsafeAssetTree
			}
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

func workspaceBusyError() *ApiError {
	err := Conflict("workspace_busy")
	err.Message = "当前有任务、下载或模型服务正在运行，请停止后再删除。"
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

	rawPath := ""
	for _, rawInst := range u.EngineInstallations {
		if canonInst, err := canonical(rawInst); err == nil && sameFileOrPath(canonInst, eng.Path) {
			rawPath = rawInst
			break
		}
	}
	if rawPath == "" {
		return false, Err("not_found")
	}
	wsEngines := workspaceEnginesRoot(u, paths)
	installDir, ok := managedPolystrataInstallDir(rawPath, eng.Path, wsEngines, u.ResultsRoot)
	if !ok {
		return false, errAssetNotDeletable()
	}
	data := s.registry()
	if polystrataInstallHasExternalDependents(u, wsEngines, eng.Path, installDir, data.engines) {
		return false, errAssetNotDeletable()
	}
	if err := verifyPolystrataRuntimeTreeSafe(installDir, u.ResultsRoot); err != nil {
		return false, errAssetNotDeletable()
	}
	if expectedTarget != "" {
		if !sameFileOrPath(installDir, expectedTarget) {
			return false, deleteTargetChangedError()
		}
		if expectedFP != "" && polystrataEngineDeleteFingerprint(installDir, eng.Path) != expectedFP {
			return false, deleteTargetChangedError()
		}
	}

	removeRaw := map[string]bool{}
	for _, rawInst := range u.EngineInstallations {
		if canonInst, err := canonical(rawInst); err == nil {
			if sameFileOrPath(canonInst, eng.Path) || pathWithinDirSameFS(installDir, canonInst) {
				removeRaw[rawInst] = true
			}
		} else if sameFileOrPath(rawInst, rawPath) || sameFileOrPath(rawInst, eng.Path) {
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

	migratedPaths, migErr := migratePolystrataRootConfigsToDataDir(stagedDir, installDir, defaultManagedPolystrataDataDirs(u, wsEngines))
	if migErr != nil {
		_ = os.Rename(stagedDir, installDir)
		return false, Err("internal_error")
	}

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

	rmErr := removeStagedPolystrataDir(stagedDir)

	s.mu.Lock()
	s.pruneAssetSnapshotLocked("engine", installDir)
	s.pruneAssetSnapshotLocked("model", installDir)
	s.mu.Unlock()

	s.checkMu.Lock()
	for path := range s.checks {
		if sameFileOrPath(path, eng.Path) || pathWithinDirSameFS(installDir, path) {
			delete(s.checks, path)
		}
	}
	for path := range s.checkInflight {
		if sameFileOrPath(path, eng.Path) || pathWithinDirSameFS(installDir, path) {
			delete(s.checkInflight, path)
		}
	}
	s.checkMu.Unlock()

	if rmErr != nil && !os.IsNotExist(rmErr) {
		apiErr := Err("internal_error")
		apiErr.Message = "已取消注册引擎，但暂存目录中的部分文件无法移除（可检查权限后清理暂存目录）。"
		return true, apiErr
	}
	return true, nil
}
