package core

import (
	"encoding/json"
	"inferyard/studio/internal/remote"
	"time"
)

func (m *NodeManager) listen(events <-chan remote.HubEvent) {
	for {
		select {
		case <-m.service.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			m.handleEvent(event)
		}
	}
}

func (m *NodeManager) handleEvent(event remote.HubEvent) {
	switch event.Type {
	case remote.HubEventPairingRequested:
		m.service.Bus.Publish("node.pairing_requested", map[string]any{"node_id": event.NodeID, "request": event.Pairing})
	case remote.HubEventHostKeyRequested:
		m.service.Bus.Publish("node.hostkey_requested", map[string]any{"node_id": event.NodeID, "request": event.HostKey})
	case remote.HubEventPeerConnected:
		if event.Peer != nil {
			m.peerConnected(*event.Peer)
		}
	case remote.HubEventPeerDisconnected:
		m.peerDisconnected(event.NodeID)
	case remote.HubEventPeerEvent:
		m.handlePeerEvent(event)
	case remote.HubEventTunnelOpened, remote.HubEventTunnelClosed:
		m.refreshTunnels(event.NodeID, event.Tunnel)
	}
}

func (m *NodeManager) peerConnected(info remote.NodePeerInfo) {
	if !validNodeID(info.NodeID) {
		return
	}
	now := time.Now().UTC()
	m.mu.Lock()
	record := m.records[info.NodeID]
	record.NodeID, record.Hostname = info.NodeID, info.Hostname
	if record.DisplayName == "" {
		record.DisplayName = info.Hostname
	}
	record.OS, record.Arch, record.Platform = info.OS, info.Arch, info.Platform
	record.CLIVersion, record.HardwareSummary = info.CLIVersion, info.HardwareSummary
	record.Transport, record.LastSeenAt, record.Revoked = info.Transport, &now, false
	if record.Transport == "" {
		record.Transport = NodeTransportMTLS
	}
	if record.PairedAt.IsZero() {
		record.PairedAt = now
	}
	m.records[info.NodeID] = record
	runtime := m.runtimes[info.NodeID]
	if runtime == nil {
		runtime = &RemoteNodeRuntime{NodeID: info.NodeID, CLIUpgrade: CLIUpgradeState{State: CLIUpgradeIdle}}
		m.runtimes[info.NodeID] = runtime
	}
	err := m.persistLocked()
	m.mu.Unlock()
	runtime.mu.Lock()
	runtime.Online = true
	runtime.DeviceSnapshot.Snapshot = append([]byte(nil), info.DeviceSnapshot...)
	runtime.DeviceSnapshot.CapturedAt = nil
	runtime.DeviceSnapshot.LastError = nil
	runtime.mu.Unlock()
	if err == nil {
		m.service.Bus.Publish("node.updated", map[string]any{"node_id": info.NodeID, "record": record})
	}
}

func (m *NodeManager) peerDisconnected(nodeID string) {
	m.mu.RLock()
	runtime := m.runtimes[nodeID]
	m.mu.RUnlock()
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.Online, runtime.Refreshing = false, false
	runtime.mu.Unlock()
	m.service.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "online": false})
}

func decodeNodePayload(raw json.RawMessage, keys []string, out any) error {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) == nil {
		for _, key := range keys {
			if payload, ok := envelope[key]; ok && len(payload) > 0 && string(payload) != "null" {
				return json.Unmarshal(payload, out)
			}
		}
	}
	return json.Unmarshal(raw, out)
}

func (m *NodeManager) runtime(nodeID string) *RemoteNodeRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runtimes[nodeID]
}

func (m *NodeManager) handlePeerEvent(event remote.HubEvent) {
	runtime := m.runtime(event.NodeID)
	if runtime == nil {
		return
	}
	switch event.EventName {
	case remote.EventServiceUpdated:
		var service EngineState
		if decodeNodePayload(event.Payload, []string{"service"}, &service) != nil {
			return
		}
		runtime.mu.Lock()
		upsertRemoteService(runtime, service)
		runtime.mu.Unlock()
		m.service.Bus.Publish("node.service.updated", map[string]any{"node_id": event.NodeID, "service": service})
	case remote.EventServiceExited:
		var notice ServiceExitNotice
		if decodeNodePayload(event.Payload, []string{"notice", "exit"}, &notice) != nil {
			return
		}
		if notice.ID == "" {
			notice.ID = uuid()
		}
		runtime.mu.Lock()
		runtime.ExitNotices = append([]ServiceExitNotice{notice}, runtime.ExitNotices...)
		for i := range runtime.Services {
			if runtime.Services[i].ServiceID == notice.ServiceID {
				runtime.Services[i].State = "exited"
			}
		}
		runtime.mu.Unlock()
		m.service.Bus.Publish("node.service.exited", map[string]any{"node_id": event.NodeID, "notice": notice})
	case remote.EventJobUpdated:
		var task RunTask
		if decodeNodePayload(event.Payload, []string{"job", "task"}, &task) != nil || task.TaskId == "" {
			return
		}
		runtime.mu.Lock()
		upsertRemoteJob(runtime, RemoteNodeJob{Task: task})
		runtime.mu.Unlock()
		m.service.Bus.Publish("node.job.updated", map[string]any{"node_id": event.NodeID, "job": task})
	case remote.EventJobProgress:
		m.updateRemoteJobProgress(runtime, event)
	case remote.EventDeviceUpdated:
		var snapshot json.RawMessage
		if decodeNodePayload(event.Payload, []string{"snapshot", "device"}, &snapshot) != nil {
			return
		}
		captured := now()
		runtime.mu.Lock()
		runtime.DeviceSnapshot.Snapshot = append([]byte(nil), snapshot...)
		runtime.DeviceSnapshot.CapturedAt = &captured
		runtime.DeviceSnapshot.Refreshing = false
		runtime.DeviceSnapshot.LastError = nil
		runtime.mu.Unlock()
		m.service.Bus.Publish("node.updated", map[string]any{"node_id": event.NodeID, "device": snapshot})
	}
}

func upsertRemoteService(runtime *RemoteNodeRuntime, service EngineState) {
	for i := range runtime.Services {
		if runtime.Services[i].ServiceID == service.ServiceID {
			runtime.Services[i] = service
			return
		}
	}
	runtime.Services = append(runtime.Services, service)
}

func upsertRemoteJob(runtime *RemoteNodeRuntime, job RemoteNodeJob) {
	for i := range runtime.Jobs {
		if runtime.Jobs[i].Task.TaskId == job.Task.TaskId {
			if job.Progress != nil {
				runtime.Jobs[i].Progress = job.Progress
			}
			runtime.Jobs[i].Task = job.Task
			return
		}
	}
	runtime.Jobs = append([]RemoteNodeJob{job}, runtime.Jobs...)
}

func (m *NodeManager) updateRemoteJobProgress(runtime *RemoteNodeRuntime, event remote.HubEvent) {
	var payload struct {
		JobID    string       `json:"job_id"`
		TaskID   string       `json:"task_id"`
		Progress *RunProgress `json:"progress"`
	}
	if json.Unmarshal(event.Payload, &payload) != nil || payload.Progress == nil {
		return
	}
	jobID := payload.JobID
	if jobID == "" {
		jobID = payload.TaskID
	}
	runtime.mu.Lock()
	for i := range runtime.Jobs {
		if runtime.Jobs[i].Task.TaskId == jobID {
			runtime.Jobs[i].Progress = cloneRunProgress(payload.Progress)
		}
	}
	runtime.mu.Unlock()
	m.service.Bus.Publish("node.job.updated", map[string]any{"node_id": event.NodeID, "job_id": jobID, "progress": payload.Progress})
}

func (m *NodeManager) refreshTunnels(nodeID string, tunnel *remote.TunnelInfo) {
	if nodeID == "" && tunnel != nil {
		nodeID = tunnel.NodeID
	}
	if runtime := m.runtime(nodeID); runtime != nil {
		all := m.hub.Tunnels()
		filtered := make([]remote.TunnelInfo, 0, len(all))
		for _, item := range all {
			if item.NodeID == nodeID {
				filtered = append(filtered, item)
			}
		}
		runtime.mu.Lock()
		runtime.Tunnels = filtered
		runtime.mu.Unlock()
	}
	m.service.Bus.Publish("node.tunnel.updated", map[string]any{"node_id": nodeID, "tunnel": tunnel})
}
