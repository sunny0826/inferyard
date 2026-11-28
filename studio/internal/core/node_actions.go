package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/remote"
	goruntime "runtime"
	"strings"
)

func (m *NodeManager) peer(nodeID string) (remote.NodePeer, *ApiError) {
	if !validNodeID(nodeID) {
		return nil, Err("invalid_request")
	}
	m.mu.RLock()
	record, ok := m.records[nodeID]
	m.mu.RUnlock()
	if !ok || record.Revoked {
		return nil, Err("not_found")
	}
	peer, ok := m.hub.Peer(nodeID)
	if !ok || peer == nil || !peer.Connected() {
		return nil, Err("node_offline")
	}
	return peer, nil
}

func (m *NodeManager) call(ctx context.Context, nodeID, method string, params, result any) *ApiError {
	peer, apiErr := m.peer(nodeID)
	if apiErr != nil {
		return apiErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := peer.Call(ctx, method, params, result); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return Err("cancelled")
		}
		return nodeEventError(err)
	}
	return nil
}

func (s *Service) StartNodeService(ctx context.Context, nodeID string, req NodeServiceStartRequest) (EngineState, *ApiError) {
	if s.nodes == nil || !validOpaqueRemotePath(req.Model) || !validOpaqueRemotePath(req.Engine) {
		return EngineState{}, Err("invalid_request")
	}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodServiceStart, req, &raw); err != nil {
		return EngineState{}, err
	}
	var service EngineState
	if len(raw) > 0 && decodeNodePayload(raw, []string{"service"}, &service) != nil {
		return EngineState{}, Err("remote_rpc_failed")
	}
	if service.ServiceID == "" {
		service.ServiceID = ServiceIDFor(req.Model, req.Engine)
	}
	if service.State == "" {
		service.State = "starting"
	}
	if runtime := s.nodes.runtime(nodeID); runtime != nil {
		runtime.mu.Lock()
		upsertRemoteService(runtime, service)
		runtime.mu.Unlock()
	}
	s.Bus.Publish("node.service.updated", map[string]any{"node_id": nodeID, "service": service})
	return service, nil
}

func (s *Service) StopNodeService(ctx context.Context, nodeID, serviceID string) (EngineState, *ApiError) {
	if s.nodes == nil || strings.TrimSpace(serviceID) == "" {
		return EngineState{}, Err("invalid_request")
	}
	params := map[string]any{"service_id": serviceID}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodServiceStop, params, &raw); err != nil {
		return EngineState{}, err
	}
	service := EngineState{ServiceID: serviceID, State: "stopping"}
	if len(raw) > 0 {
		if err := decodeNodePayload(raw, []string{"service"}, &service); err != nil {
			return EngineState{}, Err("remote_rpc_failed")
		}
	}
	if runtime := s.nodes.runtime(nodeID); runtime != nil {
		runtime.mu.Lock()
		upsertRemoteService(runtime, service)
		runtime.mu.Unlock()
	}
	s.Bus.Publish("node.service.updated", map[string]any{"node_id": nodeID, "service": service})
	return service, nil
}

func (s *Service) GetNodeServiceLogs(ctx context.Context, nodeID, serviceID string, maxLines int) (string, *ApiError) {
	if s.nodes == nil || strings.TrimSpace(serviceID) == "" {
		return "", Err("invalid_request")
	}
	params := map[string]any{"service_id": serviceID, "max_lines": maxLines}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodServiceLogs, params, &raw); err != nil {
		return "", err
	}
	var response struct {
		Logs string `json:"logs"`
		Text string `json:"text"`
	}
	if err := decodeNodePayload(raw, nil, &response); err == nil {
		if response.Logs != "" {
			return response.Logs, nil
		}
		return response.Text, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", Err("remote_rpc_failed")
	}
	return text, nil
}

func decodeRemoteTasks(raw json.RawMessage) ([]RunTask, error) {
	var tasks []RunTask
	if json.Unmarshal(raw, &tasks) == nil {
		return tasks, nil
	}
	var envelope struct {
		Jobs  []RunTask `json:"jobs"`
		Tasks []RunTask `json:"tasks"`
	}
	if json.Unmarshal(raw, &envelope) == nil && (envelope.Jobs != nil || envelope.Tasks != nil) {
		if envelope.Jobs != nil {
			return envelope.Jobs, nil
		}
		return envelope.Tasks, nil
	}
	var task RunTask
	if err := decodeNodePayload(raw, []string{"job", "task"}, &task); err != nil {
		return nil, err
	}
	return []RunTask{task}, nil
}

func actionParams(action string, req any) map[string]any {
	raw, _ := json.Marshal(req)
	params := map[string]any{}
	_ = json.Unmarshal(raw, &params)
	params["action"] = action
	return params
}

func (s *Service) startNodeBenchmark(ctx context.Context, nodeID, action string, req any) ([]RunTask, *ApiError) {
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodJobStart, actionParams(action, req), &raw); err != nil {
		return nil, err
	}
	tasks, err := decodeRemoteTasks(raw)
	if err != nil {
		return nil, Err("remote_rpc_failed")
	}
	if runtime := s.nodes.runtime(nodeID); runtime != nil {
		runtime.mu.Lock()
		for _, task := range tasks {
			upsertRemoteJob(runtime, RemoteNodeJob{Task: task})
		}
		runtime.mu.Unlock()
	}
	for _, task := range tasks {
		s.Bus.Publish("node.job.updated", map[string]any{"node_id": nodeID, "job": task})
	}
	return tasks, nil
}

func (s *Service) StartNodeBenchmark(ctx context.Context, nodeID string, req NodeBenchmarkStartRequest) ([]RunTask, *ApiError) {
	if s.nodes == nil || len(req.ConfigPaths) == 0 {
		return nil, Err("invalid_request")
	}
	for _, path := range req.ConfigPaths {
		if !validOpaqueRemotePath(path) {
			return nil, Err("invalid_request")
		}
	}
	return s.startNodeBenchmark(ctx, nodeID, "start", req)
}

func (s *Service) CancelNodeBenchmark(ctx context.Context, nodeID, jobID string) (RunTask, *ApiError) {
	if s.nodes == nil || strings.TrimSpace(jobID) == "" {
		return RunTask{}, Err("invalid_request")
	}
	var raw json.RawMessage
	params := map[string]any{"job_id": jobID}
	if err := s.nodes.call(ctx, nodeID, remote.MethodJobCancel, params, &raw); err != nil {
		return RunTask{}, err
	}
	tasks, err := decodeRemoteTasks(raw)
	if err != nil || len(tasks) != 1 {
		return RunTask{}, Err("remote_rpc_failed")
	}
	if runtime := s.nodes.runtime(nodeID); runtime != nil {
		runtime.mu.Lock()
		upsertRemoteJob(runtime, RemoteNodeJob{Task: tasks[0]})
		runtime.mu.Unlock()
	}
	s.Bus.Publish("node.job.updated", map[string]any{"node_id": nodeID, "job": tasks[0]})
	return tasks[0], nil
}

func (s *Service) ResumeNodeBenchmark(ctx context.Context, nodeID string, req NodeBenchmarkResumeRequest) ([]RunTask, *ApiError) {
	if s.nodes == nil || !validOpaqueRemotePath(req.FromRun) {
		return nil, Err("invalid_request")
	}
	return s.startNodeBenchmark(ctx, nodeID, "resume", req)
}

func (s *Service) RerunNodeBenchmark(ctx context.Context, nodeID string, req NodeBenchmarkStartRequest) ([]RunTask, *ApiError) {
	if s.nodes == nil || len(req.ConfigPaths) == 0 {
		return nil, Err("invalid_request")
	}
	for _, path := range req.ConfigPaths {
		if !validOpaqueRemotePath(path) {
			return nil, Err("invalid_request")
		}
	}
	return s.startNodeBenchmark(ctx, nodeID, "rerun", req)
}

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
	if _, _, _, err := managedEngineBuild(req); err != nil {
		return NodeOperation{}, err
	}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodNodeEngineInstall, req, &raw); err != nil {
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
