package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/remote"
	goruntime "runtime"
	"strings"
)

func (s *Service) RefreshNodeDevice(ctx context.Context, nodeID string) (DeviceResponse, *ApiError) {
	if s.nodes == nil {
		return DeviceResponse{}, Err("not_found")
	}
	runtime := s.nodes.runtime(nodeID)
	if runtime == nil {
		return DeviceResponse{}, Err("not_found")
	}
	runtime.mu.Lock()
	if runtime.Refreshing {
		runtime.mu.Unlock()
		return DeviceResponse{}, Conflict("refresh_in_progress")
	}
	runtime.Refreshing = true
	runtime.mu.Unlock()
	var raw json.RawMessage
	err := s.nodes.call(ctx, nodeID, remote.MethodNodeDeviceCheck, map[string]any{}, &raw)
	runtime.mu.Lock()
	runtime.Refreshing = false
	if err != nil {
		runtime.DeviceSnapshot.LastError = err
		runtime.mu.Unlock()
		return DeviceResponse{}, err
	}
	var snapshot json.RawMessage
	if decodeNodePayload(raw, []string{"snapshot", "device"}, &snapshot) != nil {
		snapshot = raw
	}
	runtime.DeviceSnapshot.Snapshot = append([]byte(nil), snapshot...)
	captured := now()
	runtime.DeviceSnapshot.CapturedAt = &captured
	runtime.DeviceSnapshot.LastError = nil
	response := clone(runtime.DeviceSnapshot)
	runtime.mu.Unlock()
	s.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "device": snapshot})
	return response, nil
}

func (s *Service) RefreshNodeAssets(ctx context.Context, nodeID string) (AssetsState, *ApiError) {
	if s.nodes == nil {
		return AssetsState{}, Err("not_found")
	}
	runtime := s.nodes.runtime(nodeID)
	if runtime == nil {
		return AssetsState{}, Err("not_found")
	}
	runtime.mu.Lock()
	if runtime.AssetsSnapshot.AssetsRefreshing {
		runtime.mu.Unlock()
		return AssetsState{}, Conflict("refresh_in_progress")
	}
	runtime.AssetsSnapshot.AssetsRefreshing = true
	runtime.mu.Unlock()
	var raw json.RawMessage
	err := s.nodes.call(ctx, nodeID, remote.MethodNodeAssetsScan, map[string]any{}, &raw)
	runtime.mu.Lock()
	runtime.AssetsSnapshot.AssetsRefreshing = false
	if err != nil {
		runtime.AssetsSnapshot.AssetsError = err
		runtime.mu.Unlock()
		return AssetsState{}, err
	}
	var snapshot json.RawMessage
	if decodeNodePayload(raw, []string{"snapshot", "assets"}, &snapshot) != nil {
		snapshot = raw
	}
	runtime.AssetsSnapshot.Snapshot = append([]byte(nil), snapshot...)
	captured := now()
	runtime.AssetsSnapshot.CapturedAt = &captured
	runtime.AssetsSnapshot.AssetsError = nil
	response := cloneAssetsState(runtime.AssetsSnapshot)
	runtime.mu.Unlock()
	s.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "assets": snapshot})
	return response, nil
}

func catalogForPlatform(platform string) []EngineFamilyCatalog {
	families := DefaultEngineCatalog().Families()
	out := make([]EngineFamilyCatalog, 0, len(families))
	for _, family := range families {
		family.Releases = family.ReleasesForPlatform(platform, false)
		if len(family.Releases) > 0 {
			out = append(out, family)
		}
	}
	return out
}

func (s *Service) EngineCatalogForNode(nodeID string) ([]EngineFamilyCatalog, *ApiError) {
	if nodeID == "" || nodeID == LocalNodeID {
		return catalogForPlatform(goruntime.GOOS + "/" + goruntime.GOARCH), nil
	}
	if s.nodes == nil || !validNodeID(nodeID) {
		return nil, Err("not_found")
	}
	s.nodes.mu.RLock()
	record, ok := s.nodes.records[nodeID]
	s.nodes.mu.RUnlock()
	if !ok || record.Revoked {
		return nil, Err("not_found")
	}
	platform := strings.TrimSpace(record.Platform)
	if platform == "" && record.OS != "" && record.Arch != "" {
		platform = record.OS + "/" + record.Arch
	}
	if platform == "" {
		return nil, Err("node_platform_unknown")
	}
	return catalogForPlatform(platform), nil
}

func remoteEngineBuild(req ManagedEngineInstallRequest) (EngineFamilyCatalog, EngineRelease, EnginePlatformBuild, *ApiError) {
	platform := strings.TrimSpace(req.TargetPlatform)
	family, ok := DefaultEngineCatalog().FamilyByID(strings.TrimSpace(req.Family))
	if !ok {
		return EngineFamilyCatalog{}, EngineRelease{}, EnginePlatformBuild{}, Err("invalid_request")
	}
	for _, release := range family.ReleasesForPlatform(platform, false) {
		if release.ID != strings.TrimSpace(req.ReleaseID) {
			continue
		}
		for _, build := range release.Builds {
			if build.ID == strings.TrimSpace(req.BuildID) &&
				(build.CanManagedVenvInstallOn(platform) || build.CanDirectDownloadOn(platform)) {
				return family, release, build, nil
			}
		}
	}
	return EngineFamilyCatalog{}, EngineRelease{}, EnginePlatformBuild{}, Err("invalid_request")
}

func (s *Service) InstallNodeManagedEngine(ctx context.Context, nodeID string, req ManagedEngineInstallRequest) (NodeOperation, *ApiError) {
	if s.nodes == nil || !validNodeID(nodeID) {
		return NodeOperation{}, Err("invalid_request")
	}
	s.nodes.mu.RLock()
	record, ok := s.nodes.records[nodeID]
	s.nodes.mu.RUnlock()
	if !ok || record.Revoked {
		return NodeOperation{}, Err("not_found")
	}
	req.TargetPlatform = strings.TrimSpace(req.TargetPlatform)
	if req.TargetPlatform == "" {
		req.TargetPlatform = strings.TrimSpace(record.Platform)
		if req.TargetPlatform == "" && record.OS != "" && record.Arch != "" {
			req.TargetPlatform = record.OS + "/" + record.Arch
		}
	}
	if req.TargetPlatform == "" || record.Platform != "" && req.TargetPlatform != record.Platform {
		return NodeOperation{}, Err("invalid_request")
	}
	_, release, build, failure := remoteEngineBuild(req)
	if failure != nil {
		return NodeOperation{}, failure
	}
	params := map[string]any{
		"family": req.Family, "release_id": req.ReleaseID, "build_id": req.BuildID,
		"target_platform": req.TargetPlatform, "install_mode": string(build.AcquireMode),
		"source_url": managedEngineSourceURL(release, build), "release_version": release.Version,
	}
	if build.Profile != "" {
		params["runtime_profile"] = build.Profile
	}
	for _, artifact := range build.Artifacts {
		if artifact.Role == "runtime" && artifact.URL != "" {
			params["runtime_source_url"] = artifact.URL
			if artifact.SHA256 != "" {
				params["runtime_expected_sha256"] = artifact.SHA256
			}
		} else if artifact.Role != "runtime" && artifact.SHA256 != "" {
			params["expected_sha256"] = artifact.SHA256
		}
	}
	if build.ManagedSpec != nil {
		params["python"] = build.ManagedSpec.Python
		params["packages"] = build.ManagedSpec.Packages
		params["wheel_urls"] = build.ManagedSpec.WheelURLs
	}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodNodeEngineInstall, params, &raw); err != nil {
		return NodeOperation{}, err
	}
	operation := NodeOperation{State: "submitted", Detail: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &operation)
		if operation.State == "" {
			operation.State = "submitted"
		}
		if len(operation.Detail) == 0 {
			operation.Detail = raw
		}
	}
	return operation, nil
}

func (s *Service) CleanNodeWorkspace(ctx context.Context, nodeID string) (WorkspaceCleanupResult, *ApiError) {
	if s.nodes == nil {
		return WorkspaceCleanupResult{}, Err("not_found")
	}
	var result WorkspaceCleanupResult
	if err := s.nodes.call(ctx, nodeID, remote.MethodWorkspaceClean, map[string]any{}, &result); err != nil {
		return WorkspaceCleanupResult{}, err
	}
	return result, nil
}
