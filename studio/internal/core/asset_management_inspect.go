package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

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
	return err == nil && info.IsDir() && !pathWithinDirSameFS(installDir, canonDD)
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
			ReclaimableBytes: sizePtr,
			Scope:            "standard",
		}, nil
	}
	return AssetDeleteInfo{}, Err("not_found")
}
