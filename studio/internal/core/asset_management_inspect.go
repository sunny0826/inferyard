package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/l10n"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AssetDeleteInfo describes the validated target and disk impact of deleting an asset.
type AssetDeleteInfo struct {
	Kind              string
	Name              string
	TargetPath        string
	Fingerprint       string
	ReclaimableBytes  *int64
	Scope             string
	PreservedShared   bool
	PreservedExternal bool
	KeepsModelData    bool
}

func deleteTargetChangedError() *ApiError {
	err := Conflict("delete_target_changed")
	err.Message = l10n.TR("core.asset.err.delete_target_changed")
	return err
}

func reclaimableDiskBytes(dirs, files []string) int64 {
	var uniqueDirs []string
	for _, d := range dirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		covered := false
		for _, prev := range uniqueDirs {
			if sameFileOrPath(prev, d) || pathWithinDirSameFS(prev, d) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		kept := uniqueDirs[:0]
		for _, prev := range uniqueDirs {
			if !pathWithinDirSameFS(d, prev) {
				kept = append(kept, prev)
			}
		}
		uniqueDirs = append(kept, d)
	}

	type inodeAcc struct {
		size  int64
		nlink uint64
		count uint64
	}
	inodes := map[fileInodeKey]*inodeAcc{}
	var total int64
	recordInfo := func(path string, info os.FileInfo) {
		if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return
		}
		if key, nlink, ok := fileInodeLinks(path, info); ok {
			acc, exists := inodes[key]
			if !exists {
				acc = &inodeAcc{size: info.Size(), nlink: nlink}
				inodes[key] = acc
			}
			acc.count++
			return
		}
		total += info.Size()
	}

	for _, d := range uniqueDirs {
		_ = filepath.WalkDir(d, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil || entry.Type()&os.ModeSymlink != 0 {
				if entry != nil && entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() {
				if info, err := entry.Info(); err == nil {
					recordInfo(path, info)
				}
			}
			return nil
		})
	}

	var seenFiles []string
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		if slices.ContainsFunc(uniqueDirs, func(d string) bool { return pathWithinDirSameFS(d, f) }) {
			continue
		}
		if containsSameDirEntry(seenFiles, f) {
			continue
		}
		seenFiles = append(seenFiles, f)
		if info, err := os.Lstat(f); err == nil {
			recordInfo(f, info)
		}
	}

	for _, acc := range inodes {
		if acc.count >= acc.nlink {
			total += acc.size
		}
	}
	return total
}

func fileStatToken(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%t|%d|%d", info.Mode()&os.ModeSymlink != 0, info.Size(), info.ModTime().UnixNano())
}

func dirTreeManifestHash(root string) string {
	h := sha256.New()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			_, _ = fmt.Fprintf(h, "err|%s|%v\n", path, walkErr)
			return nil
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			_, _ = fmt.Fprintf(h, "sym|%s|%s\n", rel, target)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			_, _ = fmt.Fprintf(h, "dir|%s\n", rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			_, _ = fmt.Fprintf(h, "stat|%s|%v\n", rel, err)
			return nil
		}
		_, _ = fmt.Fprintf(h, "file|%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func standardAssetDeleteFingerprint(kind, canonTarget string) string {
	info, err := os.Lstat(canonTarget)
	if err != nil {
		return ""
	}
	var contentSig string
	if info.IsDir() {
		contentSig = fmt.Sprintf("%d|%s", reclaimableDiskBytes([]string{canonTarget}, nil), dirTreeManifestHash(canonTarget))
	} else {
		contentSig = fmt.Sprintf("%d|%d|%d", reclaimableDiskBytes(nil, []string{canonTarget}), info.Size(), info.ModTime().UnixNano())
	}
	raw := fmt.Sprintf("%s|%s|%t|%s", kind, canonTarget, info.IsDir(), contentSig)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func polystrataEngineDeleteFingerprint(installDir, canonInstPath string, migPlans []migratedPolystrataConfig) string {
	info, err := os.Lstat(canonInstPath)
	if err != nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%s|%s|%d|%d|%s|%s",
		installDir,
		canonInstPath,
		reclaimableDiskBytes([]string{installDir}, nil),
		info.ModTime().UnixNano(),
		dirTreeManifestHash(installDir),
		polystrataMigPlansDigest(migPlans),
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func polystrataModelDeleteFingerprint(plan polystrataModelDeletePlan) string {
	dirEntries := slices.Clone(plan.dirs)
	slices.Sort(dirEntries)
	for i, d := range dirEntries {
		dirEntries[i] = d + ":" + dirTreeManifestHash(d)
	}
	fileEntries := slices.Clone(plan.files)
	slices.Sort(fileEntries)
	for i, f := range fileEntries {
		fileEntries[i] = f + ":" + fileStatToken(f)
	}
	raw := fmt.Sprintf(
		"%s:%s|%d|%s|%s|%t|%t",
		plan.configPath,
		fileStatToken(plan.configPath),
		plan.reclaimableBytes,
		strings.Join(dirEntries, ","),
		strings.Join(fileEntries, ","),
		plan.preservedShared,
		plan.preservedExternal,
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func polystrataInstallKeepsExternalData(u UserSettings, wsEnginesRoot, canonInstPath, installDir string) bool {
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
	if err != nil || pathWithinDirSameFS(installDir, canonDD) {
		return false
	}
	for _, managedDD := range defaultManagedPolystrataDataDirs(u, wsEnginesRoot) {
		if sameFileOrPath(canonDD, managedDD) && dirDiskUsageNoSymlinks(canonDD) > 0 {
			return true
		}
	}
	return false
}

func protectedPolystrataDataDirs(u UserSettings, wsEnginesRoot string) []string {
	out := defaultManagedPolystrataDataDirs(u, wsEnginesRoot)
	addDir := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isSymlinkPath(trimmed) {
			return
		}
		info, err := os.Lstat(trimmed)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return
		}
		canonDir, err := canonical(trimmed)
		if err != nil || !filepath.IsAbs(canonDir) {
			return
		}
		if !slices.ContainsFunc(out, func(prev string) bool { return sameFileOrPath(prev, canonDir) }) {
			out = append(out, canonDir)
		}
	}
	for _, rawInst := range u.EngineInstallations {
		rawBytes, err := installationBytes(rawInst)
		if err != nil {
			continue
		}
		var decl struct {
			Family  string `json:"family"`
			DataDir string `json:"data_dir"`
		}
		if json.Unmarshal(rawBytes, &decl) == nil && decl.Family == "polystrata" {
			addDir(decl.DataDir)
		}
	}
	return out
}

func resolveStandardModelDeleteTarget(canonAsset, matchedRoot string, assetIsDir bool, protectedDataDirs []string) string {
	if assetIsDir || !strings.HasSuffix(strings.ToLower(canonAsset), ".gguf") {
		return canonAsset
	}
	parent := filepath.Dir(canonAsset)
	if !canRemoveSingleGGUFDir(parent, matchedRoot, filepath.Base(canonAsset)) {
		return canonAsset
	}
	if isProtectedPolystrataContainer(parent, protectedDataDirs) {
		return canonAsset
	}
	for _, dd := range protectedDataDirs {
		if sameFileOrPath(parent, dd) || pathWithinDirSameFS(parent, dd) {
			return canonAsset
		}
	}
	return parent
}

func isStandardModelProtectedByPolystrata(canonAsset, canonTarget string, targetIsDir bool, protectedDataDirs []string, prot protectedPolystrataRefs) bool {
	if isProtectedPolystrataContainer(canonAsset, protectedDataDirs) || isProtectedPolystrataContainer(canonTarget, protectedDataDirs) {
		return true
	}
	for _, dd := range protectedDataDirs {
		if sameFileOrPath(canonAsset, dd) || sameFileOrPath(canonTarget, dd) {
			return true
		}
		if targetIsDir && pathWithinDirSameFS(canonTarget, dd) {
			return true
		}
	}
	return prot.touchesFile(canonAsset) || prot.touchesFile(canonTarget) || (targetIsDir && prot.touchesDir(canonTarget))
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
			data := s.registry()
			if polystrataInstallHasExternalDependents(u, wsEngines, eng.Path, installDir, data.engines) {
				return AssetDeleteInfo{}, errAssetNotDeletable()
			}
			if verifyPolystrataRuntimeTreeSafe(installDir, u.ResultsRoot) != nil {
				return AssetDeleteInfo{}, errAssetNotDeletable()
			}
			declWorkDir, declDataDir := readPolystrataInstallDirs(eng.Path)
			migPlans, migErr := snapshotPolystrataRootConfigs(installDir, declWorkDir, declDataDir, defaultManagedPolystrataDataDirs(u, wsEngines))
			if migErr != nil {
				if apiErr, ok := polystrataCredentialApiError(migErr); ok {
					return AssetDeleteInfo{}, apiErr
				}
				apiErr := Err("internal_error")
				apiErr.Message = l10n.TRf("core.asset.err.migration_config_read_failed", "err", migErr.Error())
				return AssetDeleteInfo{}, apiErr
			}
			return AssetDeleteInfo{
				Kind:             "engine",
				Name:             name,
				TargetPath:       installDir,
				Fingerprint:      polystrataEngineDeleteFingerprint(installDir, eng.Path, migPlans),
				ReclaimableBytes: ptr(reclaimableDiskBytes([]string{installDir}, nil)),
				Scope:            "polystrata_engine",
				KeepsModelData:   polystrataInstallKeepsExternalData(u, wsEngines, eng.Path, installDir),
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
				sizePtr = ptr(reclaimableDiskBytes([]string{target}, nil))
			} else if info.Mode().IsRegular() {
				sizePtr = ptr(reclaimableDiskBytes(nil, []string{target}))
			}
		}
		return AssetDeleteInfo{
			Kind:             "engine",
			Name:             name,
			TargetPath:       target,
			Fingerprint:      standardAssetDeleteFingerprint("engine", target),
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
				Fingerprint:       polystrataModelDeleteFingerprint(plan),
				ReclaimableBytes:  ptr(plan.reclaimableBytes),
				Scope:             scope,
				PreservedShared:   plan.preservedShared,
				PreservedExternal: plan.preservedExternal,
			}, nil
		}
		target := m.Path
		sizePtr := clone(m.SizeBytes)
		wsEngines := workspaceEnginesRoot(u, paths)
		protectedDataDirs := protectedPolystrataDataDirs(u, wsEngines)
		prot := collectOtherPolystrataProtectedRefs(u, wsEngines, "")
		targetIsDir := m.Format == "directory"
		if info, err := os.Lstat(m.Path); err == nil {
			targetIsDir = info.IsDir()
			if matchedRoot, ok := strictlyInsideAnyRoot(m.Path, activeModelRoots(u)); ok {
				target = resolveStandardModelDeleteTarget(m.Path, matchedRoot, targetIsDir, protectedDataDirs)
			}
			if tInfo, tErr := os.Lstat(target); tErr == nil {
				targetIsDir = tInfo.IsDir()
				if targetIsDir {
					sizePtr = ptr(reclaimableDiskBytes([]string{target}, nil))
				} else if tInfo.Mode().IsRegular() {
					sizePtr = ptr(reclaimableDiskBytes(nil, []string{target}))
				}
			}
		}
		if isStandardModelProtectedByPolystrata(m.Path, target, targetIsDir, protectedDataDirs, prot) {
			return AssetDeleteInfo{}, errAssetNotDeletable()
		}
		return AssetDeleteInfo{
			Kind:             "model",
			Name:             name,
			TargetPath:       target,
			Fingerprint:      standardAssetDeleteFingerprint("model", target),
			ReclaimableBytes: sizePtr,
			Scope:            "standard",
		}, nil
	}
	return AssetDeleteInfo{}, Err("not_found")
}
