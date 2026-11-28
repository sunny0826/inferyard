package core

import (
	"encoding/json"
	"errors"
	"inferyard/studio/internal/remote"
	"strings"
	"time"
)

func decodeRemoteRecord(raw json.RawMessage, keys []string) (map[string]any, error) {
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if payload, ok := record[key]; ok {
			encoded, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(encoded, &record); err != nil {
				return nil, err
			}
			break
		}
	}
	return record, nil
}

func decodeRemoteService(raw json.RawMessage) (EngineState, error) {
	record, err := decodeRemoteRecord(raw, []string{"service"})
	if err != nil {
		return EngineState{}, err
	}
	if record["state"] == nil || record["state"] == "" {
		record["state"] = record["status"]
	}
	if startedAt, ok := record["started_at"].(float64); ok {
		record["started_at"] = time.Unix(int64(startedAt), 0).UTC().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return EngineState{}, err
	}
	var service EngineState
	err = json.Unmarshal(encoded, &service)
	return service, err
}

func decodeRemoteTask(raw json.RawMessage) (RunTask, error) {
	record, err := decodeRemoteRecord(raw, []string{"job", "task"})
	if err != nil {
		return RunTask{}, err
	}
	if record["task_id"] == nil || record["task_id"] == "" {
		record["task_id"] = record["job_id"]
	}
	if record["state"] == nil || record["state"] == "" {
		record["state"] = record["status"]
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return RunTask{}, err
	}
	var task RunTask
	err = json.Unmarshal(encoded, &task)
	return task, err
}

func decodeRemoteExitNotice(raw json.RawMessage) (ServiceExitNotice, error) {
	record, err := decodeRemoteRecord(raw, []string{"notice", "exit"})
	if err != nil {
		return ServiceExitNotice{}, err
	}
	if logTail, ok := record["log_tail"].(string); ok {
		record["log_tail"] = strings.Split(logTail, "\n")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return ServiceExitNotice{}, err
	}
	var notice ServiceExitNotice
	err = json.Unmarshal(encoded, &notice)
	return notice, err
}

func decodeRemoteServices(raw json.RawMessage) ([]EngineState, error) {
	var records []json.RawMessage
	if json.Unmarshal(raw, &records) != nil {
		var envelope struct {
			Services []json.RawMessage `json:"services"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Services == nil {
			return nil, errors.New("invalid remote service list")
		}
		records = envelope.Services
	}
	services := make([]EngineState, 0, len(records))
	for _, rawRecord := range records {
		service, err := decodeRemoteService(rawRecord)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return services, nil
}

func (m *NodeManager) syncPeerRuntime(nodeID string) {
	var raw json.RawMessage
	if err := m.call(m.service.ctx, nodeID, remote.MethodServiceList, map[string]any{}, &raw); err == nil {
		if services, decodeErr := decodeRemoteServices(raw); decodeErr == nil {
			if runtime := m.runtime(nodeID); runtime != nil {
				runtime.mu.Lock()
				online := runtime.Online
				if online {
					runtime.Services = services
				}
				runtime.mu.Unlock()
				if online {
					m.service.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "services": services})
				}
			}
		}
	}
	raw = nil
	if err := m.call(m.service.ctx, nodeID, remote.MethodJobList, map[string]any{}, &raw); err == nil {
		if tasks, decodeErr := decodeRemoteTasks(raw); decodeErr == nil {
			if runtime := m.runtime(nodeID); runtime != nil {
				jobs := make([]RemoteNodeJob, len(tasks))
				for i, task := range tasks {
					jobs[i] = RemoteNodeJob{Task: task}
				}
				runtime.mu.Lock()
				online := runtime.Online
				if online {
					runtime.Jobs = jobs
				}
				runtime.mu.Unlock()
				if online {
					m.service.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "jobs": jobs})
				}
			}
		}
	}
}
