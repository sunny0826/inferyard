package core

import (
	"context"
	"errors"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type NodeManager struct {
	service        *Service
	hub            remote.Hub
	storePath      string
	mu             sync.RWMutex
	records        map[string]NodeRecord
	runtimes       map[string]*RemoteNodeRuntime
	activeViewNode string
	unsubscribe    func()
	closeOnce      sync.Once
}

func defaultNodeHubConfig(s *Service, configDir string) remote.HubConfig {
	u := s.userSettings()
	return remote.HubConfig{
		DataDir: configDir, ListenAddr: "", HTTPJoinAddr: ":0",
		TunnelPortMin: remote.DefaultTunnelMin, TunnelPortMax: remote.DefaultTunnelMax,
		CLIVersion: Version, DownloadSource: NormalizeDownloadSource(u.DownloadSource),
		DownloadProxy: str(u.DownloadProxy), KnownHostsPath: filepath.Join(configDir, "known_hosts"),
	}
}

func newNodeManager(s *Service, hub remote.Hub) (*NodeManager, error) {
	configDir := filepath.Dir(s.settingsPath)
	hubConfig := defaultNodeHubConfig(s, configDir)
	defaultHub := hub == nil
	if defaultHub {
		var err error
		hub, err = remote.NewHub(hubConfig)
		if err != nil {
			return nil, err
		}
	}
	records, err := loadNodeStore(filepath.Join(configDir, "nodes.json"))
	if err != nil {
		return nil, err
	}
	m := &NodeManager{
		service: s, hub: hub, storePath: filepath.Join(configDir, "nodes.json"),
		records: records, runtimes: map[string]*RemoteNodeRuntime{}, activeViewNode: LocalNodeID,
	}
	for nodeID, record := range records {
		if !record.Revoked {
			m.runtimes[nodeID] = &RemoteNodeRuntime{NodeID: nodeID, CLIUpgrade: CLIUpgradeState{State: CLIUpgradeIdle}}
		}
	}
	err = m.hub.Start(s.ctx)
	if defaultHub && errors.Is(err, syscall.EADDRINUSE) {
		_ = m.hub.Close()
		hubConfig.ListenAddr = ":0"
		m.hub, err = remote.NewHub(hubConfig)
		if err == nil {
			err = m.hub.Start(s.ctx)
		}
	}
	if err != nil {
		return nil, err
	}
	events, unsubscribe := m.hub.SubscribeEvents()
	m.unsubscribe = unsubscribe
	go m.listen(events)
	for _, peer := range m.hub.Peers() {
		if peer != nil && peer.Connected() {
			m.peerConnected(peer.Info())
		}
	}
	return m, nil
}

func (s *Service) Nodes() []NodeSnapshot {
	if s.nodes == nil {
		return []NodeSnapshot{s.localNodeSnapshot()}
	}
	return s.nodes.nodes()
}

func (m *NodeManager) nodes() []NodeSnapshot {
	out := []NodeSnapshot{m.service.localNodeSnapshot()}
	m.mu.RLock()
	ids := make([]string, 0, len(m.runtimes))
	for nodeID, runtime := range m.runtimes {
		if !m.records[nodeID].Revoked && runtime != nil {
			ids = append(ids, nodeID)
		}
	}
	sort.Strings(ids)
	for _, nodeID := range ids {
		snapshot := snapshotRemoteRuntime(m.runtimes[nodeID])
		snapshot.Record = m.records[nodeID]
		out = append(out, snapshot)
	}
	m.mu.RUnlock()
	return out
}

func (s *Service) localNodeSnapshot() NodeSnapshot {
	hostname, _ := os.Hostname()
	tasks := s.RunTasks()
	jobs := make([]RemoteNodeJob, len(tasks))
	for i, task := range tasks {
		jobs[i] = RemoteNodeJob{Task: task}
	}
	device := s.Device()
	assets := s.Assets()
	return NodeSnapshot{
		Record: NodeRecord{
			NodeID: LocalNodeID, DisplayName: LocalNodeID, Hostname: hostname,
			OS: goruntime.GOOS, Arch: goruntime.GOARCH,
			Platform:   goruntime.GOOS + "/" + goruntime.GOARCH,
			CLIVersion: Version, Transport: NodeTransportLocal, PairedAt: time.Now().UTC(),
		},
		Online: true, Refreshing: device.Refreshing || assets.AssetsRefreshing,
		DeviceSnapshot: device, AssetsSnapshot: assets, Services: s.ManagedServices(),
		ExitNotices: s.ExitedServiceNotices(), Jobs: jobs, Tunnels: []remote.TunnelInfo{},
		CLIUpgrade: s.CLIUpgradeStatus(),
	}
}

func (s *Service) Node(nodeID string) (NodeSnapshot, *ApiError) {
	if nodeID == LocalNodeID {
		return s.localNodeSnapshot(), nil
	}
	if s.nodes == nil || !validNodeID(nodeID) {
		return NodeSnapshot{}, Err("not_found")
	}
	s.nodes.mu.RLock()
	runtime, record, ok := s.nodes.runtimes[nodeID], s.nodes.records[nodeID], false
	if runtime != nil && !record.Revoked {
		ok = true
	}
	s.nodes.mu.RUnlock()
	if !ok {
		return NodeSnapshot{}, Err("not_found")
	}
	snapshot := snapshotRemoteRuntime(runtime)
	snapshot.Record = record
	return snapshot, nil
}

func (s *Service) ActiveViewNodeID() string {
	if s.nodes == nil {
		return LocalNodeID
	}
	s.nodes.mu.RLock()
	defer s.nodes.mu.RUnlock()
	return s.nodes.activeViewNode
}

func (s *Service) SetActiveViewNodeID(nodeID string) *ApiError {
	if _, err := s.Node(nodeID); err != nil {
		return err
	}
	if s.nodes != nil {
		s.nodes.mu.Lock()
		s.nodes.activeViewNode = nodeID
		s.nodes.mu.Unlock()
	}
	return nil
}

func (s *Service) RemoveNode(nodeID string) *ApiError {
	if s.nodes == nil || !validNodeID(nodeID) {
		return Err("invalid_request")
	}
	s.nodes.mu.RLock()
	record, ok := s.nodes.records[nodeID]
	s.nodes.mu.RUnlock()
	if !ok || record.Revoked {
		return Err("not_found")
	}
	if err := s.nodes.hub.RevokeNode(nodeID); err != nil {
		return nodeEventError(err)
	}
	s.nodes.mu.Lock()
	record, ok = s.nodes.records[nodeID]
	if !ok {
		s.nodes.mu.Unlock()
		return Err("not_found")
	}
	record.Revoked = true
	s.nodes.records[nodeID] = record
	delete(s.nodes.runtimes, nodeID)
	if s.nodes.activeViewNode == nodeID {
		s.nodes.activeViewNode = LocalNodeID
	}
	err := s.nodes.persistLocked()
	s.nodes.mu.Unlock()
	if err != nil {
		return Err("internal_error")
	}
	s.Bus.Publish("node.updated", map[string]any{"node_id": nodeID, "record": record})
	return nil
}

func (s *Service) Tunnels() []remote.TunnelInfo {
	if s.nodes == nil {
		return nil
	}
	return s.nodes.hub.Tunnels()
}

func (s *Service) OpenNodeTunnel(ctx context.Context, req remote.OpenTunnelRequest) (remote.TunnelInfo, *ApiError) {
	if s.nodes == nil || !validNodeID(req.NodeID) {
		return remote.TunnelInfo{}, Err("invalid_request")
	}
	if _, err := s.nodes.peer(req.NodeID); err != nil {
		return remote.TunnelInfo{}, err
	}
	if strings.TrimSpace(req.NodeName) == "" {
		s.nodes.mu.RLock()
		req.NodeName = s.nodes.records[req.NodeID].DisplayName
		s.nodes.mu.RUnlock()
	}
	tunnel, err := s.nodes.hub.OpenTunnel(ctx, req)
	return tunnel, nodeEventError(err)
}

func (s *Service) CloseNodeTunnel(tunnelID string) *ApiError {
	if s.nodes == nil || strings.TrimSpace(tunnelID) == "" {
		return Err("invalid_request")
	}
	return nodeEventError(s.nodes.hub.CloseTunnel(tunnelID))
}

func (s *Service) closeNodesOnShutdown() {
	if s.nodes != nil {
		s.nodes.close()
	}
}

func (m *NodeManager) close() {
	m.closeOnce.Do(func() {
		if m.unsubscribe != nil {
			m.unsubscribe()
		}
		_ = m.hub.Close()
	})
}
