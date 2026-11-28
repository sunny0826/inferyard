package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/remote"
	"strings"
	"time"
)

func (s *Service) StartNodePairing(ctx context.Context, opts remote.PairingSessionOptions) (remote.PairingSession, *ApiError) {
	if s.nodes == nil {
		return remote.PairingSession{}, Err("not_found")
	}
	u := s.userSettings()
	if strings.TrimSpace(opts.DownloadSource) == "" {
		opts.DownloadSource = NormalizeDownloadSource(u.DownloadSource)
	}
	if strings.TrimSpace(opts.DownloadProxy) == "" {
		opts.DownloadProxy = str(u.DownloadProxy)
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
	peer, connected := m.hub.Peer(c.NodeID)
	runtime.mu.Lock()
	runtime.Online = connected && peer != nil && peer.Connected()
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
	u := s.userSettings()
	if strings.TrimSpace(req.DownloadSource) == "" {
		req.DownloadSource = NormalizeDownloadSource(u.DownloadSource)
	}
	if strings.TrimSpace(req.DownloadProxy) == "" {
		req.DownloadProxy = str(u.DownloadProxy)
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
