package core

import (
	"encoding/json"
	"inferyard/studio/internal/remote"
	"testing"
	"time"
)

func waitForRemoteNodeState(t *testing.T, svc *Service, nodeID string, ready func(NodeSnapshot) bool) NodeSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, apiErr := svc.Node(nodeID)
		if apiErr == nil && ready(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, _ := svc.Node(nodeID)
	t.Fatalf("remote state was not synchronized: %+v", snapshot)
	return NodeSnapshot{}
}

func TestNodeReconnectPreservesHardwareAndSynchronizesRuntime(t *testing.T) {
	hub := newMockRemoteHub()
	peer := newMockNodePeer("node-a", "linux/amd64")
	hub.peers[peer.info.NodeID] = peer
	peer.responses[remote.MethodServiceList] = map[string]any{
		"services": []any{map[string]any{
			"service_id": "svc-remote", "status": "running", "started_at": 1760000000,
			"pid": 123, "port": 48857, "log_path": "/tmp/service.log",
		}},
	}
	peer.responses[remote.MethodJobList] = map[string]any{
		"jobs": []any{map[string]any{
			"job_id": "job-remote", "status": "running", "created_at": "2026-11-28T00:00:00Z",
		}},
	}
	svc, _ := newNodeTestService(t, hub)
	snapshot := waitForRemoteNodeState(t, svc, "node-a", func(snapshot NodeSnapshot) bool {
		return len(snapshot.Services) == 1 && len(snapshot.Jobs) == 1
	})
	if snapshot.Services[0].ServiceID != "svc-remote" || snapshot.Services[0].State != "running" || snapshot.Services[0].StartedAt == "" {
		t.Fatalf("service list not decoded: %+v", snapshot.Services)
	}
	if snapshot.Jobs[0].Task.TaskId != "job-remote" || snapshot.Jobs[0].Task.State != "running" {
		t.Fatalf("job list not decoded: %+v", snapshot.Jobs)
	}

	info := peer.info
	info.HardwareSummary = ""
	svc.nodes.peerConnected(info)
	snapshot, apiErr := svc.Node("node-a")
	if apiErr != nil || snapshot.Record.HardwareSummary != "test GPU" {
		t.Fatalf("reconnect cleared hardware summary: %+v err=%v", snapshot.Record, apiErr)
	}
	waitForRemoteNodeState(t, svc, "node-a", func(snapshot NodeSnapshot) bool {
		return len(peer.methodCalls(remote.MethodServiceList)) >= 2 && len(peer.methodCalls(remote.MethodJobList)) >= 2
	})
}

func TestNodeServiceExitNoticeDecoding(t *testing.T) {
	hub := newMockRemoteHub()
	peer := newMockNodePeer("node-a", "linux/amd64")
	hub.peers[peer.info.NodeID] = peer
	svc, _ := newNodeTestService(t, hub)
	waitForRemoteNodeState(t, svc, "node-a", func(snapshot NodeSnapshot) bool {
		return len(peer.methodCalls(remote.MethodJobList)) > 0
	})

	payloads := []struct {
		raw  string
		want []string
	}{
		{raw: `{"service_id":"svc-a","exit_code":7,"log_tail":"fatal\nstack"}`, want: []string{"fatal", "stack"}},
		{raw: `{"service_id":"svc-b","log_tail":["one","two"]}`, want: []string{"one", "two"}},
	}
	for _, item := range payloads {
		svc.nodes.handlePeerEvent(remote.HubEvent{
			NodeID: "node-a", EventName: remote.EventServiceExited, Payload: json.RawMessage(item.raw),
		})
		snapshot, apiErr := svc.Node("node-a")
		if apiErr != nil || len(snapshot.ExitNotices) == 0 {
			t.Fatalf("exit notice missing: %+v err=%v", snapshot, apiErr)
		}
		notice := snapshot.ExitNotices[0]
		if notice.ID == "" || len(notice.LogTail) != len(item.want) || notice.LogTail[0] != item.want[0] || notice.LogTail[1] != item.want[1] {
			t.Fatalf("exit notice not decoded: %+v", notice)
		}
	}

	svc.nodes.handlePeerEvent(remote.HubEvent{NodeID: "node-a", EventName: remote.EventServiceExited, Payload: json.RawMessage(`{`)})
	snapshot, _ := svc.Node("node-a")
	if len(snapshot.ExitNotices) != 3 || snapshot.ExitNotices[0].ID == "" || len(snapshot.ExitNotices[0].LogTail) != 1 {
		t.Fatalf("malformed exit receipt was dropped: %+v", snapshot.ExitNotices)
	}
}
