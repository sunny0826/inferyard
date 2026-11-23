package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	err.Message = "待删除资源或其关联文件已发生变化，请关闭后重新确认。"
	return err
}

func standardAssetDeleteFingerprint(kind, canonTarget string) string {
	info, err := os.Lstat(canonTarget)
	if err != nil {
		return ""
	}
	var size int64
	if info.IsDir() {
		size = dirDiskUsageNoSymlinks(canonTarget)
	} else {
		size = info.Size()
	}
	raw := fmt.Sprintf("%s|%s|%t|%d|%d", kind, canonTarget, info.IsDir(), size, info.ModTime().UnixNano())
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func polystrataEngineDeleteFingerprint(installDir, canonInstPath string) string {
	info, err := os.Lstat(canonInstPath)
	if err != nil {
		return ""
	}
	raw := fmt.Sprintf("%s|%s|%d|%d", installDir, canonInstPath, dirDiskUsageNoSymlinks(installDir), info.ModTime().UnixNano())
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func polystrataModelDeleteFingerprint(plan polystrataModelDeletePlan) string {
	raw := fmt.Sprintf(
		"%s|%d|%s|%s|%t|%t",
		plan.configPath,
		plan.reclaimableBytes,
		strings.Join(plan.dirs, ","),
		strings.Join(plan.files, ","),
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

func polystrataInstallHasExternalDependents(u UserSettings, wsEnginesRoot, canonInstPath, installDir string, engines []registryEntry) bool {
	if otherEngineSharesInstallDir(installDir, canonInstPath, engines) {
		return true
	}
	for _, rawInst := range u.EngineInstallations {
		canonOther, err := canonical(rawInst)
		if err != nil || sameFileOrPath(canonOther, canonInstPath) {
			continue
		}
		if pathWithinDirSameFS(installDir, canonOther) {
			return true
		}
		rawBytes, err := installationBytes(canonOther)
		if err != nil {
			continue
		}
		var decl struct {
			Executable       string   `json:"executable"`
			WorkingDirectory string   `json:"working_directory"`
			DataDir          string   `json:"data_dir"`
			RuntimeRoots     []string `json:"runtime_roots"`
		}
		if json.Unmarshal(rawBytes, &decl) != nil {
			continue
		}
		refs := append([]string{decl.Executable, decl.WorkingDirectory, decl.DataDir}, decl.RuntimeRoots...)
		for _, rawRef := range refs {
			trimmed := strings.TrimSpace(rawRef)
			if trimmed == "" {
				continue
			}
			resolved := filepath.Clean(trimmed)
			if c, cerr := canonical(trimmed); cerr == nil {
				resolved = c
			}
			if pathWithinDirSameFS(installDir, resolved) {
				return true
			}
		}
	}

	var seenCfgs []string
	checkSurvivingDir := func(dir, declWorkDir, declDataDir string) bool {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, item := range dirEntries {
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
			if err != nil || pathWithinDirSameFS(installDir, resolved) {
				continue
			}
			if containsSameDirEntry(seenCfgs, resolved) {
				continue
			}
			seenCfgs = append(seenCfgs, resolved)
			files, dirs := resolvePolystrataConfigRefsLenient(resolved, declWorkDir, declDataDir)
			for _, f := range files {
				if pathWithinDirSameFS(installDir, f.path) {
					return true
				}
			}
			for _, d := range dirs {
				if sameFileOrPath(d, installDir) || pathWithinDirSameFS(installDir, d) {
					return true
				}
			}
		}
		return false
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
			if checkSurvivingDir(dir, decl.WorkingDirectory, decl.DataDir) {
				return true
			}
		}
	}
	for _, dd := range defaultManagedPolystrataDataDirs(u, wsEnginesRoot) {
		for _, dir := range polystrataConfigSearchDirs("", dd) {
			if checkSurvivingDir(dir, "", dd) {
				return true
			}
		}
	}
	return false
}

func migratePolystrataRootConfigsToDataDir(stagedDir, origInstallDir string, managedDataDirs []string) {
	if len(managedDataDirs) == 0 {
		return
	}
	entries, err := os.ReadDir(stagedDir)
	if err != nil {
		return
	}
	for _, item := range entries {
		name := item.Name()
		if item.IsDir() || !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		srcPath := filepath.Join(stagedDir, name)
		info, err := os.Lstat(srcPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxInstallationBytes {
			continue
		}
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		var cfg struct {
			Exe  string   `json:"exe"`
			Args []string `json:"args"`
		}
		if json.Unmarshal(raw, &cfg) != nil || strings.TrimSpace(cfg.Exe) == "" || cfg.Args == nil {
			continue
		}
		for _, dd := range managedDataDirs {
			if isSymlinkPath(dd) {
				continue
			}
			files, dirs := resolvePolystrataConfigRefsLenient(srcPath, origInstallDir, dd)
			inDD, inRuntime := false, false
			for _, f := range files {
				if pathWithinDirSameFS(stagedDir, f.path) || pathWithinDirSameFS(origInstallDir, f.path) {
					inRuntime = true
				}
				if pathStrictlyInsideDirSameFS(f.path, dd) {
					inDD = true
				}
			}
			for _, d := range dirs {
				if pathWithinDirSameFS(stagedDir, d) || pathWithinDirSameFS(origInstallDir, d) {
					inRuntime = true
				}
				if pathStrictlyInsideDirSameFS(d, dd) {
					inDD = true
				}
			}
			if !inDD || inRuntime {
				continue
			}
			destPath := filepath.Join(dd, name)
			if existing, readErr := os.ReadFile(destPath); readErr == nil {
				if bytes.Equal(existing, raw) {
					break
				}
				destPath = ""
				stem := strings.TrimSuffix(name, ".json")
				for i := 1; i <= 100; i++ {
					cand := filepath.Join(dd, fmt.Sprintf("%s-retained-%d.json", stem, i))
					if _, lerr := os.Lstat(cand); os.IsNotExist(lerr) {
						destPath = cand
						break
					}
				}
			}
			if destPath != "" {
				_ = os.WriteFile(destPath, raw, 0o600)
			}
			break
		}
	}
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
			return AssetDeleteInfo{
				Kind:             "engine",
				Name:             name,
				TargetPath:       installDir,
				Fingerprint:      polystrataEngineDeleteFingerprint(installDir, eng.Path),
				ReclaimableBytes: ptr(dirDiskUsageNoSymlinks(installDir)),
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
				sizePtr = ptr(dirDiskUsageNoSymlinks(target))
			} else if info.Mode().IsRegular() {
				sizePtr = ptr(info.Size())
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
		if info, err := os.Lstat(m.Path); err == nil && !info.IsDir() && strings.HasSuffix(strings.ToLower(m.Path), ".gguf") {
			if matchedRoot, ok := strictlyInsideAnyRoot(m.Path, activeModelRoots(u)); ok {
				parent := filepath.Dir(m.Path)
				if canRemoveSingleGGUFDir(parent, matchedRoot, filepath.Base(m.Path)) {
					target = parent
					sizePtr = ptr(dirDiskUsageNoSymlinks(target))
				}
			}
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
