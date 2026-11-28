package pages

import (
	"context"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

type nodeViewState struct {
	snapshots          []core.NodeSnapshot
	activeID           string
	tunnels            []remote.TunnelInfo
	tunnelsOpen        bool
	addOpen            bool
	addMode            int
	pairOS             int
	pairing            *remote.PairingSession
	pairingRequests    []remote.PairingRequest
	hostKeyRequests    []remote.HostKeyRequest
	pairingErr         *core.ApiError
	sshCommand         string
	sshPassword        string
	sshParsed          *remote.ParsedSSHCommand
	sshParsedRaw       string
	sshErr             *core.ApiError
	catalog            []core.EngineFamilyCatalog
	catalogNodeID      string
	unifiedReports     []core.UnifiedReportSummary
	reportsLoaded      bool
	comparePickerOpen  bool
	compareOpen        bool
	compareLoading     bool
	compareLeftRef     string
	compareRightRef    string
	comparison         *core.ReportComparison
	compareErr         *core.ApiError
	cleanNodeConfirm   bool
	cleanCacheConfirm  bool
	logOpen            bool
	logTitle           string
	logText            string
	setActiveNodeFn    func(string) *core.ApiError
	decidePairingFn    func(remote.PairingDecision) (core.NodeRecord, *core.ApiError)
	decideHostKeyFn    func(remote.HostKeyDecision) *core.ApiError
	parseSSHFn         func(string) (remote.ParsedSSHCommand, *core.ApiError)
	connectSSHFn       func(context.Context, remote.SSHConnectRequest) (core.NodeRecord, *core.ApiError)
	openTunnelFn       func(context.Context, remote.OpenTunnelRequest) (remote.TunnelInfo, *core.ApiError)
	closeTunnelFn      func(string) *core.ApiError
	compareReportsFn   func(string, string) (*core.ReportComparison, *core.ApiError)
	resumeRemoteFn     func(context.Context, string, string) ([]core.RunTask, *core.ApiError)
	rerunRemoteFn      func(context.Context, string, string) ([]core.RunTask, *core.ApiError)
	cleanNodeFn        func(context.Context, string) (core.WorkspaceCleanupResult, *core.ApiError)
	cleanReportCacheFn func(...string) (core.WorkspaceCleanupResult, *core.ApiError)
}

func remoteReportReferenceParts(reference string) (string, string, bool) {
	if !strings.HasPrefix(reference, "remote://") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(reference, "remote://"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func localReportSummaries(reports []core.UnifiedReportSummary) []core.ReportSummary {
	out := make([]core.ReportSummary, 0, len(reports))
	for _, report := range reports {
		if !report.IsRemote {
			out = append(out, report.ReportSummary)
		}
	}
	return out
}

func (s *viewState) reportSummaries() []core.UnifiedReportSummary {
	if s.nodes.reportsLoaded {
		return s.nodes.unifiedReports
	}
	out := make([]core.UnifiedReportSummary, 0, len(s.reports))
	for _, report := range s.reports {
		out = append(out, core.UnifiedReportSummary{
			ReportSummary: report, ReportRef: report.ReportId,
			NodeID: core.LocalNodeID, NodeName: core.LocalNodeID, SourceOnline: true,
		})
	}
	return out
}

func (s *viewState) activeNode() core.NodeSnapshot {
	activeID := s.nodes.activeID
	if activeID == "" {
		activeID = core.LocalNodeID
	}
	for _, snapshot := range s.nodes.snapshots {
		if snapshot.Record.NodeID == activeID {
			return snapshot
		}
	}
	return core.NodeSnapshot{
		Record: core.NodeRecord{NodeID: core.LocalNodeID, DisplayName: core.LocalNodeID, Transport: core.NodeTransportLocal},
		Online: true,
	}
}

func (s *viewState) remoteView() bool {
	return s.activeNode().Record.NodeID != core.LocalNodeID
}

func (s *viewState) tunnelForService(nodeID, serviceID string) *remote.TunnelInfo {
	for i := range s.nodes.tunnels {
		tunnel := &s.nodes.tunnels[i]
		if tunnel.NodeID == nodeID && tunnel.ServiceID == serviceID {
			return tunnel
		}
	}
	return nil
}
