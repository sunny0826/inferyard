package core

import (
	"errors"
	"inferyard/studio/internal/inferyard"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type SuggestedModelDir struct {
	Label string
	Path  string
}

func extraModelsRootsOf(u UserSettings) []string {
	return append([]string(nil), u.ExtraModelsRoots...)
}

func (s *Service) extraModelsRoots() []string {
	return extraModelsRootsOf(s.userSettings())
}

func activeModelRoots(u UserSettings) []string {
	roots := []string{}
	seen := map[string]bool{}
	add := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.ContainsRune(trimmed, 0) {
			return
		}
		clean := filepath.Clean(trimmed)
		key := clean
		if c, err := canonical(clean); err == nil {
			key = c
		}
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, clean)
	}
	if u.ModelsRoot != nil {
		add(*u.ModelsRoot)
	}
	for _, extra := range extraModelsRootsOf(u) {
		add(extra)
	}
	return roots
}

func workspaceEnginesRoot(u UserSettings, paths *BoundPaths) string {
	if paths != nil && strings.TrimSpace(paths.Engines) != "" {
		return filepath.Clean(paths.Engines)
	}
	if w := workspaceRoot(u); w != nil && strings.TrimSpace(*w) != "" {
		return filepath.Join(filepath.Clean(*w), "engines")
	}
	return ""
}

func isSymlinkPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func strictlyInsideAnyRoot(canonPath string, roots []string) (string, bool) {
	if canonPath == "" || !filepath.IsAbs(canonPath) {
		return "", false
	}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		canonRoot, err := canonical(root)
		if err != nil || !filepath.IsAbs(canonRoot) {
			continue
		}
		if canonPath != canonRoot && withinCanonicalDir(canonRoot, canonPath) {
			return canonRoot, true
		}
	}
	return "", false
}

func overlapsResultsRoot(canonTarget string, resultsRoot *string) bool {
	if resultsRoot == nil || strings.TrimSpace(*resultsRoot) == "" {
		return false
	}
	canonResults, err := canonical(*resultsRoot)
	if err != nil {
		return true
	}
	return canonTarget == canonResults ||
		withinCanonicalDir(canonResults, canonTarget) ||
		withinCanonicalDir(canonTarget, canonResults)
}

func isModelDeletable(rawPath, canonPath string, modelRoots []string, resultsRoot *string) bool {
	if isSymlinkPath(rawPath) || isSymlinkPath(canonPath) {
		return false
	}
	if _, ok := strictlyInsideAnyRoot(canonPath, modelRoots); !ok {
		return false
	}
	return !overlapsResultsRoot(canonPath, resultsRoot)
}

func isEngineDeletable(rawPath, canonPath, kind, wsEnginesRoot string, resultsRoot *string) bool {
	if kind == "local_installation" || strings.TrimSpace(wsEnginesRoot) == "" {
		return false
	}
	if isSymlinkPath(rawPath) || isSymlinkPath(canonPath) {
		return false
	}
	if _, ok := strictlyInsideAnyRoot(canonPath, []string{wsEnginesRoot}); !ok {
		return false
	}
	return !overlapsResultsRoot(canonPath, resultsRoot)
}

func registryStorageMetrics(u UserSettings, modelRoots []string, models []registryEntry, scanned *inferyard.AssetScan) (int64, *int64) {
	var total int64
	countedFiles := map[string]bool{}
	for _, entry := range models {
		if entry.model.Format == "strata_pack" {
			continue
		}
		if entry.path != "" {
			countedFiles[entry.path] = true
			if entry.model.Format == "gguf" {
				if shards, ok := expandSplitGGUFShards(entry.path); ok {
					for _, sh := range shards {
						countedFiles[sh.path] = true
					}
				}
			}
		}
		if entry.model.SizeBytes != nil && *entry.model.SizeBytes > 0 {
			total += *entry.model.SizeBytes
		}
	}
	for _, entry := range models {
		if entry.model.Format != "strata_pack" {
			continue
		}
		if len(entry.assetFiles) > 0 {
			for _, f := range entry.assetFiles {
				if f.size <= 0 || countedFiles[f.path] {
					continue
				}
				countedFiles[f.path] = true
				total += f.size
			}
			continue
		}
		if entry.model.SizeBytes != nil && *entry.model.SizeBytes > 0 {
			total += *entry.model.SizeBytes
		}
	}
	if total == 0 && scanned != nil && scanned.TotalModelBytes != nil {
		total = *scanned.TotalModelBytes
	}
	if u.ModelsRoot != nil && strings.TrimSpace(*u.ModelsRoot) != "" {
		if free := DiskFreeBytesForPath(*u.ModelsRoot); free != nil {
			return total, free
		}
	}
	for _, root := range modelRoots {
		if free := DiskFreeBytesForPath(root); free != nil {
			return total, free
		}
	}
	if scanned != nil {
		var fallback *int64
		primaryCanon := ""
		if u.ModelsRoot != nil && strings.TrimSpace(*u.ModelsRoot) != "" {
			if c, err := canonical(*u.ModelsRoot); err == nil {
				primaryCanon = c
			}
		}
		for _, r := range scanned.Roots {
			if r.Kind != "models" || r.DiskFreeBytes == nil {
				continue
			}
			v := *r.DiskFreeBytes
			if primaryCanon != "" {
				if c, err := canonical(r.Path); err == nil && c == primaryCanon {
					return total, &v
				}
			}
			if fallback == nil {
				fallback = &v
			}
		}
		if fallback != nil {
			return total, fallback
		}
	}
	return total, nil
}

func SuggestLocalModelDirs(existing []string) []SuggestedModelDir {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, raw := range existing {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		seen[filepath.Clean(trimmed)] = true
		if c, err := canonical(trimmed); err == nil {
			seen[c] = true
		}
	}
	candidates := []SuggestedModelDir{
		{Label: "LM Studio", Path: filepath.Join(home, ".lmstudio", "models")},
		{Label: "Hugging Face", Path: filepath.Join(home, ".cache", "huggingface", "hub")},
		{Label: "Ollama", Path: filepath.Join(home, ".ollama", "models")},
		{Label: "~/models", Path: filepath.Join(home, "models")},
	}
	out := []SuggestedModelDir{}
	for _, cand := range candidates {
		linfo, err := os.Lstat(cand.Path)
		if err != nil || linfo.Mode()&os.ModeSymlink != 0 || !linfo.IsDir() {
			continue
		}
		canon, err := canonical(cand.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(canon)
		if err != nil || !info.IsDir() || seen[canon] {
			continue
		}
		seen[canon] = true
		out = append(out, SuggestedModelDir{Label: cand.Label, Path: canon})
	}
	return out
}

func (s *Service) RevealAsset(kind, name string) *ApiError {
	if (kind != "model" && kind != "engine") || strings.TrimSpace(name) == "" {
		return Err("invalid_request")
	}
	assetPath, _ := s.lookupAsset(kind, name)
	if assetPath == "" {
		return Err("not_found")
	}
	info, err := os.Stat(assetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Err("not_found")
		}
		return Err("internal_error")
	}
	target := assetPath
	if !info.IsDir() {
		target = filepath.Dir(assetPath)
	}
	if err := OpenPathInOS(target); err != nil {
		return Err("internal_error")
	}
	return nil
}

func (s *Service) lookupAsset(kind, name string) (path string, deletable bool) {
	reg := s.Registry()
	if kind == "model" {
		for _, m := range reg.Models {
			if m.Name == name {
				return m.Path, m.Deletable
			}
		}
		return "", false
	}
	for _, e := range reg.Engines {
		if e.Name == name {
			return e.Path, e.Deletable
		}
	}
	return "", false
}

func errAssetNotDeletable() *ApiError {
	return &ApiError{
		Code:        "asset_not_deletable",
		Message:     "该资源受保护或不在受管目录内，不能删除。",
		Limitations: []string{},
	}
}

func (s *Service) assetWorkspaceBusyLocked() bool {
	if s.runsActiveLocked() {
		return true
	}
	for _, d := range s.downloads {
		if d.Task.State == "downloading" {
			return true
		}
	}
	if s.engine != nil {
		switch s.engine.state.State {
		case "starting", "running", "stopping":
			return true
		}
	}
	return false
}

func isSealedEvidenceMarker(name string) bool {
	switch name {
	case "manifest.json", "events.jsonl", "studio.json", "run-result.json":
		return true
	default:
		return false
	}
}

func canRemoveSingleGGUFDir(parent, matchedRoot, ggufName string) bool {
	if parent == matchedRoot || filepath.Dir(parent) != matchedRoot || isSymlinkPath(parent) {
		return false
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return false
		}
		name := entry.Name()
		if name == ggufName || name == "source.json" || name == "model-assets.json" || strings.HasSuffix(name, ".part") {
			continue
		}
		return false
	}
	return true
}

func resolveEngineDeleteTarget(canonAsset, wsEnginesCanon, name string, engines []EngineAsset) string {
	rel, err := filepath.Rel(wsEnginesCanon, canonAsset)
	if err != nil {
		return canonAsset
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return canonAsset
	}
	topChild := filepath.Join(wsEnginesCanon, parts[0])
	if len(parts) == 2 || (len(parts) == 3 && parts[1] == "bin") {
		return topChild
	}
	shared := false
	for _, other := range engines {
		if other.Name == name || other.Path == "" {
			continue
		}
		if otherCanon, err := canonical(other.Path); err == nil && withinCanonicalDir(topChild, otherCanon) {
			shared = true
			break
		}
	}
	if !shared {
		return topChild
	}
	parent := filepath.Dir(canonAsset)
	if filepath.Base(parent) == "bin" && filepath.Dir(parent) != wsEnginesCanon {
		return filepath.Dir(parent)
	}
	if parent != wsEnginesCanon {
		return parent
	}
	return canonAsset
}

var errUnsafeAssetTree = errors.New("unsafe asset tree")

func verifyAssetTreeSafe(target string, isDir bool) error {
	if !isDir {
		if isSealedEvidenceMarker(filepath.Base(target)) {
			return errUnsafeAssetTree
		}
		return nil
	}
	canonTarget, err := canonical(target)
	if err != nil {
		return errUnsafeAssetTree
	}
	return filepath.WalkDir(canonTarget, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			resolved, linkErr := filepath.EvalSymlinks(path)
			if linkErr != nil || !withinCanonicalDir(canonTarget, resolved) || isSealedEvidenceMarker(filepath.Base(resolved)) {
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

func (s *Service) DeleteManagedAsset(kind, name string) *ApiError {
	if (kind != "model" && kind != "engine") || strings.TrimSpace(name) == "" {
		return Err("invalid_request")
	}
	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		err := Conflict("workspace_busy")
		err.Message = "当前有任务、下载或模型服务正在运行，请停止后再删除。"
		return err
	}
	u := clone(s.settings)
	paths := s.paths
	s.mu.Unlock()

	reg := s.Registry()
	assetPath, deletable := s.lookupAsset(kind, name)
	if assetPath == "" {
		return Err("not_found")
	}
	if !deletable {
		return errAssetNotDeletable()
	}

	assetInfo, err := os.Lstat(assetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Err("not_found")
		}
		return errAssetNotDeletable()
	}
	if assetInfo.Mode()&os.ModeSymlink != 0 {
		return errAssetNotDeletable()
	}
	canonAsset, err := canonical(assetPath)
	if err != nil {
		return errAssetNotDeletable()
	}

	var allowedRoots []string
	if kind == "model" {
		allowedRoots = activeModelRoots(u)
	} else if wsEngines := workspaceEnginesRoot(u, paths); wsEngines != "" {
		allowedRoots = []string{wsEngines}
	}
	matchedRoot, ok := strictlyInsideAnyRoot(canonAsset, allowedRoots)
	if !ok {
		return errAssetNotDeletable()
	}

	deleteTarget := canonAsset
	if kind == "model" && !assetInfo.IsDir() && strings.HasSuffix(strings.ToLower(canonAsset), ".gguf") {
		parent := filepath.Dir(canonAsset)
		if canRemoveSingleGGUFDir(parent, matchedRoot, filepath.Base(canonAsset)) {
			deleteTarget = parent
		}
	} else if kind == "engine" {
		deleteTarget = resolveEngineDeleteTarget(canonAsset, matchedRoot, name, reg.Engines)
	}

	targetInfo, err := os.Lstat(deleteTarget)
	if err != nil {
		if os.IsNotExist(err) {
			return Err("not_found")
		}
		return errAssetNotDeletable()
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		return errAssetNotDeletable()
	}
	canonDelete, err := canonical(deleteTarget)
	if err != nil {
		return errAssetNotDeletable()
	}
	if _, ok := strictlyInsideAnyRoot(canonDelete, allowedRoots); !ok {
		return errAssetNotDeletable()
	}
	if overlapsResultsRoot(canonDelete, u.ResultsRoot) {
		return errAssetNotDeletable()
	}
	if err := verifyAssetTreeSafe(canonDelete, targetInfo.IsDir()); err != nil {
		return errAssetNotDeletable()
	}

	s.mu.Lock()
	if s.assetWorkspaceBusyLocked() {
		s.mu.Unlock()
		err := Conflict("workspace_busy")
		err.Message = "当前有任务、下载或模型服务正在运行，请停止后再删除。"
		return err
	}
	if targetInfo.IsDir() {
		err = os.RemoveAll(canonDelete)
	} else {
		err = os.Remove(canonDelete)
	}
	if err != nil {
		s.mu.Unlock()
		if os.IsNotExist(err) {
			return Err("not_found")
		}
		return Err("internal_error")
	}
	s.pruneAssetSnapshotLocked(kind, canonDelete)
	s.mu.Unlock()

	if refreshErr := s.RefreshAssets(); refreshErr != nil && refreshErr.Code != "conflict" {
		return nil
	}
	return nil
}

func (s *Service) pruneAssetSnapshotLocked(kind, canonDelete string) {
	if s.assets.Parsed == nil && len(s.assets.Snapshot) > 0 {
		if parsed, err := inferyard.ParseAssetScan(s.assets.Snapshot); err == nil {
			s.assets.Parsed = &parsed
		}
	}
	if s.assets.Parsed == nil {
		return
	}
	matchesDeleted := func(raw string) bool {
		c, err := canonical(raw)
		if err != nil {
			c = filepath.Clean(raw)
		}
		return c == canonDelete || withinCanonicalDir(canonDelete, c)
	}
	if kind == "model" {
		s.assets.Parsed.Models = slices.DeleteFunc(s.assets.Parsed.Models, func(m inferyard.ScannedModel) bool {
			return matchesDeleted(m.Path)
		})
		if s.assets.Parsed.TotalModelBytes != nil {
			var total int64
			for _, m := range s.assets.Parsed.Models {
				if m.SizeBytes != nil && *m.SizeBytes > 0 {
					total += *m.SizeBytes
				}
			}
			s.assets.Parsed.TotalModelBytes = &total
		}
	} else {
		s.assets.Parsed.Engines = slices.DeleteFunc(s.assets.Parsed.Engines, func(e inferyard.ScannedEngine) bool {
			return matchesDeleted(e.Path)
		})
	}
	s.assets.Snapshot = nil
}
