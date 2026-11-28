package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/remote"
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
	if len(raw) > 0 {
		var err error
		if service, err = decodeRemoteService(raw); err != nil {
			return EngineState{}, Err("remote_rpc_failed")
		}
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
		var err error
		if service, err = decodeRemoteService(raw); err != nil {
			return EngineState{}, Err("remote_rpc_failed")
		}
	}
	if service.ServiceID == "" {
		service.ServiceID = serviceID
	}
	if service.State == "" {
		service.State = "stopping"
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
		Log  string `json:"log"`
		Text string `json:"text"`
	}
	if err := decodeNodePayload(raw, nil, &response); err == nil {
		if response.Logs != "" {
			return response.Logs, nil
		}
		if response.Log != "" {
			return response.Log, nil
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
	var records []json.RawMessage
	if json.Unmarshal(raw, &records) != nil {
		var envelope struct {
			Jobs  []json.RawMessage `json:"jobs"`
			Tasks []json.RawMessage `json:"tasks"`
		}
		if json.Unmarshal(raw, &envelope) == nil && (envelope.Jobs != nil || envelope.Tasks != nil) {
			if envelope.Jobs != nil {
				records = envelope.Jobs
			} else {
				records = envelope.Tasks
			}
		} else {
			task, err := decodeRemoteTask(raw)
			if err != nil {
				return nil, err
			}
			return []RunTask{task}, nil
		}
	}
	tasks := make([]RunTask, 0, len(records))
	for _, record := range records {
		task, err := decodeRemoteTask(record)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
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
