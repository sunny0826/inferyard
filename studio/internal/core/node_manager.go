package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
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

func newNodeManager(s *Service, hub remote.Hub) (*NodeManager, error) {
	configDir := filepath.Dir(s.settingsPath)
	if hub == nil {
		var err error
		hub, err = remote.NewHub(remote.HubConfig{
			DataDir: configDir, ListenAddr: ":0", HTTPJoinAddr: ":0",
			TunnelPortMin: remote.DefaultTunnelMin, TunnelPortMax: remote.DefaultTunnelMax,
			CLIVersion: Version, KnownHostsPath: filepath.Join(configDir, "known_hosts"),
		})
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
	if err := m.hub.Start(s.ctx); err != nil {
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

func (s *Service) StartNodePairing(ctx context.Context, opts remote.PairingSessionOptions) (remote.PairingSession, *ApiError) {
	if s.nodes == nil {
		return remote.PairingSession{}, Err("not_found")
	}
	session, err := s.nodes.hub.StartPairingSession(ctx, opts)
	return session, nodeEventError(err)
}

func (s *Service) ActiveNodePairing() (remote.PairingSession, bool) {
	if s.nodes == nil {
		return remote.PairingSession{}, false
	}
	return s.nodes.hub.ActivePairingSession()
}

func (s *Service) CancelNodePairing(code string) *ApiError {
	if s.nodes == nil {
		return Err("not_found")
	}
	return nodeEventError(s.nodes.hub.CancelPairingSession(code))
}

func (s *Service) PendingPairingRequests() []remote.PairingRequest {
	if s.nodes == nil {
		return nil
	}
	return s.nodes.hub.PendingPairingRequests()
}

func (s *Service) DecideNodePairing(decision remote.PairingDecision) (NodeRecord, *ApiError) {
	if s.nodes == nil {
		return NodeRecord{}, Err("not_found")
	}
	credentials, err := s.nodes.hub.DecidePairing(decision)
	if err != nil {
		return NodeRecord{}, nodeEventError(err)
	}
	record, apiErr := s.nodes.registerCredentials(credentials)
	if apiErr == nil {
		s.Bus.Publish("node.updated", map[string]any{"node_id": record.NodeID, "record": record})
	}
	return record, apiErr
}

func (m *NodeManager) registerCredentials(c remote.PairedNodeCredentials) (NodeRecord, *ApiError) {
	if !validNodeID(c.NodeID) {
		return NodeRecord{}, Err("invalid_request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	pairedAt := c.PairedAt
	if pairedAt.IsZero() {
		pairedAt = time.Now().UTC()
	}
	record := NodeRecord{
		NodeID: c.NodeID, DisplayName: c.DisplayName, Hostname: c.Hostname,
		OS: c.OS, Arch: c.Arch, Platform: c.Platform, CLIVersion: c.CLIVersion,
		HardwareSummary: c.HardwareSummary, Transport: NodeTransportMTLS, PairedAt: pairedAt,
	}
	if record.DisplayName == "" {
		record.DisplayName = record.Hostname
	}
	m.records[c.NodeID] = record
	if err := m.persistLocked(); err != nil {
		return NodeRecord{}, Err("internal_error")
	}
	runtime := m.runtimes[c.NodeID]
	if runtime == nil {
		runtime = &RemoteNodeRuntime{NodeID: c.NodeID, CLIUpgrade: CLIUpgradeState{State: CLIUpgradeIdle}}
		m.runtimes[c.NodeID] = runtime
	}
	runtime.mu.Lock()
	runtime.Online = true
	runtime.DeviceSnapshot.Snapshot = append([]byte(nil), c.DeviceSnapshot...)
	runtime.mu.Unlock()
	return record, nil
}

func (s *Service) ParseSSHCommand(raw string) (remote.ParsedSSHCommand, *ApiError) {
	if s.nodes == nil {
		return remote.ParsedSSHCommand{}, Err("not_found")
	}
	parsed, err := s.nodes.hub.ParseSSHCommand(raw)
	return parsed, nodeEventError(err)
}

func safeSSHCommand(parsed remote.ParsedSSHCommand) string {
	target := parsed.Host
	if parsed.User != "" {
		target = parsed.User + "@" + target
	}
	if parsed.Port > 0 && parsed.Port != 22 {
		return fmt.Sprintf("ssh -p %d %s", parsed.Port, target)
	}
	return "ssh " + target
}

func (s *Service) ConnectSSHNode(ctx context.Context, req remote.SSHConnectRequest) (NodeRecord, *ApiError) {
	if s.nodes == nil {
		return NodeRecord{}, Err("not_found")
	}
	parsed, err := s.nodes.hub.ParseSSHCommand(req.Command)
	if err != nil {
		return NodeRecord{}, nodeEventError(err)
	}
	session, err := s.nodes.hub.ConnectSSH(ctx, req)
	if err != nil {
		return NodeRecord{}, nodeEventError(err)
	}
	if !validNodeID(session.NodeID) {
		return NodeRecord{}, Err("invalid_request")
	}
	record := NodeRecord{
		NodeID: session.NodeID, DisplayName: req.DisplayName, Hostname: session.Host,
		Transport: NodeTransportSSH, SSHCommandSafe: safeSSHCommand(parsed), PairedAt: session.ConnectedAt,
	}
	if peer, ok := s.nodes.hub.Peer(session.NodeID); ok {
		info := peer.Info()
		record.OS, record.Arch, record.Platform = info.OS, info.Arch, info.Platform
		record.CLIVersion, record.HardwareSummary = info.CLIVersion, info.HardwareSummary
	}
	if record.DisplayName == "" {
		record.DisplayName = session.Host
	}
	if record.PairedAt.IsZero() {
		record.PairedAt = time.Now().UTC()
	}
	s.nodes.mu.Lock()
	s.nodes.records[session.NodeID] = record
	if s.nodes.runtimes[session.NodeID] == nil {
		s.nodes.runtimes[session.NodeID] = &RemoteNodeRuntime{NodeID: session.NodeID, Online: true, CLIUpgrade: CLIUpgradeState{State: CLIUpgradeIdle}}
	}
	err = s.nodes.persistLocked()
	s.nodes.mu.Unlock()
	if err != nil {
		return NodeRecord{}, Err("internal_error")
	}
	s.Bus.Publish("node.updated", map[string]any{"node_id": record.NodeID, "record": record})
	return record, nil
}

func (s *Service) PendingHostKeyRequests() []remote.HostKeyRequest {
	if s.nodes == nil {
		return nil
	}
	return s.nodes.hub.PendingHostKeyRequests()
}

func (s *Service) DecideHostKey(decision remote.HostKeyDecision) *ApiError {
	if s.nodes == nil {
		return Err("not_found")
	}
	return nodeEventError(s.nodes.hub.DecideHostKey(decision))
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
