package core

import (
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type replacedEngineTarget struct {
	name    string
	dir     string
	exePath string
	entry   registryEntry
}

func parsePreparedEngineDetails(cr inferyard.CommandResult, parseErr error, outDir, sourceURL string) (string, int64, string, bool) {
	if parseErr != nil {
		return "", 0, "", false
	}
	var details struct {
		Engine string `json:"engine"`
		Bytes  int64  `json:"bytes"`
		Sha256 string `json:"sha256"`
	}
	if err := json.Unmarshal(cr.Details, &details); err != nil || details.Engine == "" || details.Bytes < 0 || len(details.Sha256) != 64 {
		return "", 0, "", false
	}
	if writeManagedEngineReceipt(outDir, managedBinaryReceipt(sourceURL)) != nil {
		return "", 0, "", false
	}
	return details.Engine, details.Bytes, details.Sha256, true
}

func hasSymlinkBetween(canonBase, rawDescendant string) bool {
	if canonBase == "" || strings.TrimSpace(rawDescendant) == "" {
		return true
	}
	cleanRaw := filepath.Clean(rawDescendant)
	rel, err := filepath.Rel(canonBase, cleanRaw)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	cur := canonBase
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		if isSymlinkPath(cur) {
			return true
		}
	}
	return false
}

func (s *Service) resolveReplacedManagedBinaryTarget(name string, entry registryEntry, allEngines []registryEntry) *replacedEngineTarget {
	if entry.installation != "" || strings.TrimSpace(entry.path) == "" {
		return nil
	}
	u := s.userSettings()
	wsEngines := workspaceEnginesRoot(u, s.paths)
	if wsEngines == "" || isSymlinkPath(wsEngines) || isSymlinkPath(entry.rawPath) || isSymlinkPath(entry.path) {
		return nil
	}
	canonWS, err := canonical(wsEngines)
	if err != nil || !filepath.IsAbs(canonWS) {
		return nil
	}
	canonAsset, err := canonical(entry.path)
	if err != nil {
		return nil
	}
	if _, ok := strictlyInsideAnyRoot(canonAsset, []string{canonWS}); !ok {
		return nil
	}
	rawCheck := entry.rawPath
	if strings.TrimSpace(rawCheck) == "" {
		rawCheck = entry.path
	}
	if hasSymlinkBetween(canonWS, rawCheck) && hasSymlinkBetween(filepath.Clean(wsEngines), rawCheck) {
		return nil
	}
	assets := make([]EngineAsset, 0, len(allEngines))
	for _, item := range allEngines {
		assets = append(assets, EngineAsset{Name: item.engine.Name, Path: item.path})
	}
	targetDir := resolveEngineDeleteTarget(canonAsset, canonWS, name, assets)
	if targetDir == "" || sameFileOrPath(targetDir, canonAsset) || sameFileOrPath(targetDir, canonWS) {
		return nil
	}
	info, err := os.Lstat(targetDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	canonDir, err := canonical(targetDir)
	if err != nil {
		return nil
	}
	if _, ok := strictlyInsideAnyRoot(canonDir, []string{canonWS}); !ok {
		return nil
	}
	if overlapsResultsRoot(canonDir, derivedResultsRoot(u, s.paths)) {
		return nil
	}
	for _, other := range allEngines {
		if other.path == "" || sameFileOrPath(other.path, canonAsset) {
			continue
		}
		if otherCanon, err := canonical(other.path); err == nil && (sameFileOrPath(otherCanon, canonDir) || withinCanonicalDir(canonDir, otherCanon)) {
			return nil
		}
	}
	return &replacedEngineTarget{
		name:    name,
		dir:     canonDir,
		exePath: resolveEngineExecutablePath(entry),
		entry:   entry,
	}
}

func (s *Service) isEngineDirInUseLocked(canonDir string) bool {
	inDir := func(raw string) bool {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || !filepath.IsAbs(trimmed) {
			return false
		}
		cand := filepath.Clean(trimmed)
		if sameFileOrPath(cand, canonDir) || withinCanonicalDir(canonDir, cand) {
			return true
		}
		if c, err := canonical(cand); err == nil && (sameFileOrPath(c, canonDir) || withinCanonicalDir(canonDir, c)) {
			return true
		}
		return false
	}
	checkSvc := func(e *engineEntry) bool {
		if e == nil || !isActiveServiceState(e.state.State) {
			return false
		}
		if inDir(e.state.Executable) {
			return true
		}
		return e.cmd != nil && inDir(e.cmd.Path)
	}
	if checkSvc(s.engine) {
		return true
	}
	for _, svc := range s.services {
		if checkSvc(svc) {
			return true
		}
	}
	for _, r := range s.runs {
		if r != nil && !terminal(r.Task.State) && r.Task.CandidatePath != nil && inDir(*r.Task.CandidatePath) {
			return true
		}
	}
	return false
}

func (s *Service) cleanupReplacedManagedBinaryEngine(replaced *replacedEngineTarget, newOutDir string) {
	if replaced == nil || strings.TrimSpace(replaced.dir) == "" || isSymlinkPath(replaced.dir) {
		return
	}
	u := s.userSettings()
	wsEngines := workspaceEnginesRoot(u, s.paths)
	if wsEngines == "" || isSymlinkPath(wsEngines) {
		return
	}
	canonWS, err := canonical(wsEngines)
	if err != nil {
		return
	}
	info, err := os.Lstat(replaced.dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	canonOld, err := canonical(replaced.dir)
	if err != nil {
		return
	}
	if _, ok := strictlyInsideAnyRoot(canonOld, []string{canonWS}); !ok {
		return
	}
	if overlapsResultsRoot(canonOld, derivedResultsRoot(u, s.paths)) {
		return
	}
	canonNew, err := canonical(newOutDir)
	if err != nil || sameFileOrPath(canonOld, canonNew) || withinCanonicalDir(canonOld, canonNew) || withinCanonicalDir(canonNew, canonOld) {
		return
	}
	if verifyAssetTreeSafe(canonOld, true) != nil {
		return
	}

	s.mu.Lock()
	if s.isEngineActiveLocked(replaced.name, replaced.entry) || s.isEngineDirInUseLocked(canonOld) {
		s.mu.Unlock()
		return
	}
	s.deletingAssets++
	s.mu.Unlock()

	removeErr := os.RemoveAll(canonOld)

	s.mu.Lock()
	if removeErr == nil {
		s.pruneAssetSnapshotLocked("engine", canonOld)
	}
	s.deletingAssets--
	s.mu.Unlock()

	if removeErr != nil {
		return
	}
	if s.engineUpdater != nil && replaced.exePath != "" {
		s.engineUpdater.clearBinaryVersion(replaced.exePath)
	}
	s.checkMu.Lock()
	for path := range s.checks {
		if sameFileOrPath(path, canonOld) || withinCanonicalDir(canonOld, path) {
			delete(s.checks, path)
		}
	}
	for path := range s.checkInflight {
		if sameFileOrPath(path, canonOld) || withinCanonicalDir(canonOld, path) {
			delete(s.checkInflight, path)
		}
	}
	s.checkNames = slices.DeleteFunc(s.checkNames, func(t engineCheckTarget) bool {
		return sameFileOrPath(t.path, canonOld) || withinCanonicalDir(canonOld, t.path)
	})
	s.checkMu.Unlock()
}
