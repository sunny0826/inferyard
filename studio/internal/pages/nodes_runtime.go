package pages

import (
	"context"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/remote"
)

func (s *viewState) syncNodeState() {
	if s.service == nil || s.preview != nil {
		return
	}
	s.nodes.snapshots = s.service.Nodes()
	s.syncCachedRemoteReportStates()
	s.nodes.activeID = s.service.ActiveViewNodeID()
	s.nodes.tunnels = s.service.Tunnels()
	s.nodes.pairingRequests = s.service.PendingPairingRequests()
	s.nodes.hostKeyRequests = s.service.PendingHostKeyRequests()
	if session, ok := s.service.ActiveNodePairing(); ok {
		s.nodes.pairing = &session
	}
	node := s.activeNode()
	if node.Record.NodeID != core.LocalNodeID && s.nodes.catalogNodeID != node.Record.NodeID {
		catalog, err := s.service.EngineCatalogForNode(node.Record.NodeID)
		if err == nil {
			s.nodes.catalog = catalog
			s.nodes.catalogNodeID = node.Record.NodeID
		}
	}
}

func (s *viewState) ingestNodeEvents(events []core.Event) {
	if s.service == nil || s.preview != nil {
		return
	}
	sync, syncReports := false, false
	for _, event := range events {
		switch event.Type {
		case "node.pairing_requested":
			s.nodes.addOpen, s.nodes.addMode = true, 0
			sync = true
		case "node.hostkey_requested":
			s.nodes.addOpen, s.nodes.addMode = true, 1
			sync = true
		case "remote_report.imported":
			sync, syncReports = true, true
		default:
			if strings.HasPrefix(event.Type, "node.") {
				sync, syncReports = true, true
			}
		}
	}
	if sync {
		s.syncNodeState()
	}
	if syncReports {
		s.refreshUnifiedReports()
	}
}

func (s *viewState) refreshUnifiedReports() {
	if s.service == nil || s.preview != nil {
		return
	}
	reports, err := s.service.AllReports()
	if err == nil {
		s.nodes.unifiedReports = reports
		s.nodes.reportsLoaded = true
	}
}

func (s *viewState) selectViewNode(nodeID string) {
	if nodeID == "" || nodeID == s.nodes.activeID {
		return
	}
	setActive := s.nodes.setActiveNodeFn
	if setActive == nil && s.service != nil {
		setActive = s.service.SetActiveViewNodeID
	}
	if setActive == nil {
		return
	}
	if err := setActive(nodeID); err != nil {
		s.failure, s.failurePage = err, s.selected
		return
	}
	s.nodes.activeID = nodeID
	s.nodes.catalogNodeID = ""
	s.nodes.serviceFormOpen = false
	s.nodes.serviceModel, s.nodes.serviceEngine, s.nodes.serviceContext, s.nodes.servicePort = "", "", "", ""
	s.nodes.benchmarkFormOpen = false
	s.nodes.benchmarkConfigs = ""
	s.closeOpenedReport()
	s.nodes.compareOpen = false
	if s.preview == nil {
		s.syncNodeState()
	}
}

func (s *viewState) nodeDownloadOptions() (string, string) {
	if s.service == nil {
		return "", ""
	}
	effective := s.service.Settings().Effective
	return effective.DownloadSource, emptyText(effective.DownloadProxy)
}

func (s *viewState) openAddNodeDialog() {
	s.nodes.addOpen = true
	s.nodes.pairingErr = nil
	if s.preview != nil || s.service == nil {
		return
	}
	if session, ok := s.service.ActiveNodePairing(); ok {
		s.nodes.pairing = &session
		return
	}
	start := s.nodes.startPairingFn
	if start == nil {
		start = s.service.StartNodePairing
	}
	source, proxy := s.nodeDownloadOptions()
	session, err := start(s.nodeContext(), remote.PairingSessionOptions{DownloadSource: source, DownloadProxy: proxy})
	if err != nil {
		s.nodes.pairingErr = err
		return
	}
	s.nodes.pairing = &session
}

func (s *viewState) submitPairingDecision(request remote.PairingRequest, option remote.PairingOption) {
	decide := s.nodes.decidePairingFn
	if decide == nil && s.service != nil {
		decide = s.service.DecideNodePairing
	}
	if decide == nil {
		return
	}
	record, err := decide(remote.PairingDecision{RequestID: request.RequestID, Option: option, DisplayName: request.Hostname})
	if option == remote.PairingReject {
		s.nodes.pairingRequests = nil
		s.nodes.addOpen = false
		return
	}
	if err != nil {
		s.nodes.pairingErr = err
		return
	}
	s.nodes.pairingRequests = nil
	s.nodes.addOpen = false
	s.selectViewNode(record.NodeID)
}

func (s *viewState) submitHostKeyDecision(request remote.HostKeyRequest, option remote.HostKeyOption) {
	decide := s.nodes.decideHostKeyFn
	if decide == nil && s.service != nil {
		decide = s.service.DecideHostKey
	}
	if decide == nil {
		return
	}
	err := decide(remote.HostKeyDecision{RequestID: request.RequestID, Option: option})
	if option == remote.HostKeyReject {
		s.nodes.hostKeyRequests = nil
		return
	}
	if err != nil {
		s.nodes.sshErr = err
		return
	}
	s.nodes.hostKeyRequests = nil
}

func (s *viewState) parseSSHInput() {
	raw := s.nodes.sshCommand
	if raw == s.nodes.sshParsedRaw {
		return
	}
	s.nodes.sshParsedRaw = raw
	s.nodes.sshParsed, s.nodes.sshErr = nil, nil
	parse := s.nodes.parseSSHFn
	if parse == nil && s.service != nil {
		parse = s.service.ParseSSHCommand
	}
	if parse == nil || strings.TrimSpace(raw) == "" {
		return
	}
	parsed, err := parse(raw)
	if err != nil {
		s.nodes.sshErr = err
		return
	}
	s.nodes.sshParsed = &parsed
}

func (s *viewState) connectSSHNode() {
	s.parseSSHInput()
	if s.nodes.sshParsed == nil || s.busy {
		return
	}
	source, proxy := s.nodeDownloadOptions()
	req := remote.SSHConnectRequest{
		Command: s.nodes.sshCommand, Password: s.nodes.sshPassword,
		DownloadSource: source, DownloadProxy: proxy,
	}
	s.nodes.sshPassword = ""
	if s.preview != nil && s.nodes.connectSSHFn != nil {
		record, err := s.nodes.connectSSHFn(s.nodeContext(), req)
		if err != nil {
			s.nodes.sshErr = err
			return
		}
		s.nodes.addOpen = false
		s.nodes.sshCommand, s.nodes.sshParsedRaw, s.nodes.sshParsed = "", "", nil
		s.selectViewNode(record.NodeID)
		return
	}
	s.workAs("connect-ssh-node", func() (string, *core.ApiError) {
		connect := s.nodes.connectSSHFn
		if connect == nil && s.service != nil {
			connect = s.service.ConnectSSHNode
		}
		if connect == nil {
			return "", core.Err("not_found")
		}
		record, err := connect(s.ctx, req)
		if err != nil {
			return "", err
		}
		s.updateView(func() {
			s.nodes.addOpen = false
			s.nodes.sshCommand, s.nodes.sshParsedRaw, s.nodes.sshParsed = "", "", nil
			s.selectViewNode(record.NodeID)
		})
		return l10n.TR("nodes.ssh.connected"), nil
	})
}

func (s *viewState) openServiceTunnel(node core.NodeSnapshot, service core.EngineState) {
	if service.Port < 1 {
		return
	}
	req := remote.OpenTunnelRequest{
		NodeID: node.Record.NodeID, NodeName: node.Record.DisplayName,
		ServiceID: service.ServiceID, ModelName: s.serviceModelTitle(nil, service),
		EngineName: serviceEngineTitle(service), RemotePort: service.Port,
	}
	open := s.nodes.openTunnelFn
	if open == nil && s.service != nil {
		open = s.service.OpenNodeTunnel
	}
	if open == nil {
		return
	}
	if _, err := open(s.ctx, req); err != nil {
		s.failure, s.failurePage = err, s.selected
		return
	}
	if s.preview == nil {
		s.syncNodeState()
	}
}

func (s *viewState) closeTunnel(tunnelID string) {
	closeFn := s.nodes.closeTunnelFn
	if closeFn == nil && s.service != nil {
		closeFn = s.service.CloseNodeTunnel
	}
	if closeFn == nil {
		return
	}
	if err := closeFn(tunnelID); err != nil {
		s.failure, s.failurePage = err, s.selected
		return
	}
	if s.preview == nil {
		s.syncNodeState()
	}
}

func (s *viewState) nodeContext() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
