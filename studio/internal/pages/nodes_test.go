package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/remote"
)

func TestSelectViewNodeKeepsOtherNodeState(t *testing.T) {
	localService := core.EngineState{ServiceID: "local-service", State: "running"}
	tunnel := remote.TunnelInfo{TunnelID: "tunnel-1", NodeID: "node-a", LocalURL: "http://127.0.0.1:18001/v1"}
	s := &viewState{managedServices: []core.EngineState{localService}}
	s.nodes.snapshots = []core.NodeSnapshot{
		{Record: core.NodeRecord{NodeID: core.LocalNodeID}, Online: true, Services: []core.EngineState{localService}},
		{Record: core.NodeRecord{NodeID: "node-a"}, Online: true},
		{Record: core.NodeRecord{NodeID: "node-b"}, Online: true},
	}
	s.nodes.activeID = core.LocalNodeID
	s.nodes.tunnels = []remote.TunnelInfo{tunnel}
	selected := ""
	s.nodes.setActiveNodeFn = func(nodeID string) *core.ApiError { selected = nodeID; return nil }
	s.nodes.serviceFormOpen = true
	s.nodes.serviceModel = "/node-a/model.gguf"
	s.nodes.benchmarkFormOpen = true
	s.nodes.benchmarkConfigs = "/node-a/config.toml"
	s.selectViewNode("node-a")
	s.nodes.benchmarkConfigs = "/node-a/config.toml"
	s.selectViewNode("node-b")
	if s.nodes.benchmarkConfigs != "" || s.nodes.serviceModel != "" || s.nodes.benchmarkFormOpen || s.nodes.serviceFormOpen {
		t.Fatalf("switching nodes leaked remote drafts: %+v", s.nodes)
	}
	if got := compareSideModel(false, "", "Qwen3"); got != compareMissing {
		t.Fatalf("missing side model filled with %q, want %q", got, compareMissing)
	}
	if got := compareSideModel(true, "", "Qwen3"); got != "Qwen3" {
		t.Fatalf("present side model = %q, want Qwen3", got)
	}
	if selected != "node-b" || s.nodes.activeID != "node-b" {
		t.Fatalf("unexpected active node: selected=%q active=%q", selected, s.nodes.activeID)
	}
	if len(s.managedServices) != 1 || s.managedServices[0].State != "running" || len(s.nodes.tunnels) != 1 {
		t.Fatalf("switching nodes mutated services or tunnels: %+v %+v", s.managedServices, s.nodes.tunnels)
	}
}

func TestPairingAndHostKeyUseExplicitOptions(t *testing.T) {
	s := &viewState{}
	s.nodes.setActiveNodeFn = func(string) *core.ApiError { return nil }
	request := previewPairingRequestForTest()
	var pairing remote.PairingDecision
	s.nodes.decidePairingFn = func(decision remote.PairingDecision) (core.NodeRecord, *core.ApiError) {
		pairing = decision
		return core.NodeRecord{NodeID: "node-a"}, nil
	}
	s.nodes.addOpen = true
	s.submitPairingDecision(request, remote.PairingApprove)
	if pairing.RequestID != request.RequestID || pairing.Option != remote.PairingApprove {
		t.Fatalf("unexpected pairing decision: %+v", pairing)
	}
	s.nodes.addOpen = true
	s.submitPairingDecision(request, remote.PairingReject)
	if pairing.Option != remote.PairingReject || s.nodes.addOpen {
		t.Fatalf("unexpected rejection: %+v open=%v", pairing, s.nodes.addOpen)
	}

	hostKey := remote.HostKeyRequest{RequestID: "hostkey-1", Host: "example.com", Port: 22}
	for _, option := range []remote.HostKeyOption{remote.HostKeyTrustOnce, remote.HostKeyTrustAndSave, remote.HostKeyReject} {
		var decision remote.HostKeyDecision
		s.nodes.decideHostKeyFn = func(got remote.HostKeyDecision) *core.ApiError { decision = got; return nil }
		s.nodes.hostKeyRequests = []remote.HostKeyRequest{hostKey}
		s.submitHostKeyDecision(hostKey, option)
		if decision.RequestID != hostKey.RequestID || decision.Option != option {
			t.Fatalf("option %q became %+v", option, decision)
		}
	}
}

func previewPairingRequestForTest() remote.PairingRequest {
	return remote.PairingRequest{RequestID: "pair-1", Hostname: "Win-RTX4090", Options: []remote.PairingOption{remote.PairingApprove, remote.PairingReject}}
}

func TestTunnelActionsPreserveEndpointAndPasswordClears(t *testing.T) {
	s := &viewState{}
	node := core.NodeSnapshot{Record: core.NodeRecord{NodeID: "node-a", DisplayName: "Node A"}, Online: true}
	node.Services = []core.EngineState{{ServiceID: "svc", ModelName: "Qwen", State: "running", Port: 48857}}
	s.nodes.snapshots = []core.NodeSnapshot{node}
	var opened remote.OpenTunnelRequest
	s.nodes.openTunnelFn = func(_ context.Context, req remote.OpenTunnelRequest) (remote.TunnelInfo, *core.ApiError) {
		opened = req
		return remote.TunnelInfo{TunnelID: "tunnel-1", NodeID: req.NodeID, ServiceID: req.ServiceID, LocalURL: "http://127.0.0.1:18001/v1"}, nil
	}
	s.openServiceTunnel(node, node.Services[0])
	if opened.NodeID != "node-a" || opened.ServiceID != "svc" || opened.RemotePort != 48857 {
		t.Fatalf("unexpected tunnel request: %+v", opened)
	}
	closed := ""
	s.nodes.closeTunnelFn = func(id string) *core.ApiError { closed = id; return nil }
	s.closeTunnel("tunnel-1")
	if closed != "tunnel-1" {
		t.Fatalf("closed tunnel %q", closed)
	}

	s.nodes.sshCommand = "ssh -p 22 root@example.com"
	s.nodes.sshParsedRaw = s.nodes.sshCommand
	s.nodes.sshPassword = "one-time-secret"
	s.nodes.sshParsed = &remote.ParsedSSHCommand{User: "root", Host: "example.com", Port: 22}
	password := ""
	s.nodes.connectSSHFn = func(_ context.Context, req remote.SSHConnectRequest) (core.NodeRecord, *core.ApiError) {
		password = req.Password
		return core.NodeRecord{NodeID: "node-a"}, nil
	}
	s.nodes.setActiveNodeFn = func(string) *core.ApiError { return nil }
	s.updateWindow = func(fn func()) { fn() }
	s.connectSSHNode()
	deadline := time.Now().Add(time.Second)
	for s.busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if password != "one-time-secret" {
		t.Fatalf("one-time password was not passed to the request")
	}
	if s.nodes.sshPassword != "" {
		t.Fatal("one-time password remained in UI state")
	}
}

func TestNodeErrorMessagesAreLocalized(t *testing.T) {
	for _, code := range []string{"node_offline", "node_platform_unknown", "remote_rpc_failed"} {
		if core.Err(code).Message == "" {
			t.Fatalf("missing localized error for %q", code)
		}
	}
}

func TestReportCompareErrorReopensPickerAndWorkspaceResetsRefs(t *testing.T) {
	s := readyState("reports")
	s.reports = []core.ReportSummary{{ReportId: "r-a"}, {ReportId: "r-b"}}
	s.nodes.compareOpen = true
	s.nodes.compareLeftRef, s.nodes.compareRightRef = "r-a", "r-b"
	s.nodes.compareErr = &core.ApiError{Code: "unreadable", Message: "unreadable report"}

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()
	click(t, tt, l10n.TR("nodes.compare.action"))
	tt.Frame()
	if !s.nodes.comparePickerOpen {
		t.Fatal("expected compare picker to open from error state")
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, l10n.TR("nodes.compare.title")); !ok {
		t.Fatalf("compare picker dialog did not render on error state: %v", tt.Texts())
	}

	s.resetWorkspace()
	if s.nodes.compareLeftRef != "" || s.nodes.compareRightRef != "" || s.nodes.compareErr != nil || s.nodes.compareOpen {
		t.Fatalf("resetWorkspace left stale compare state: %+v", s.nodes)
	}
	if got := s.reportSummaries(); len(got) != 0 {
		t.Fatalf("resetWorkspace left stale fallback reports: %+v", got)
	}
	s.openReportComparePicker()
	if s.nodes.comparePickerOpen {
		t.Fatal("compare picker opened immediately after resetWorkspace before new reports loaded")
	}
	s.reports = []core.ReportSummary{{ReportId: "r-c"}, {ReportId: "r-d"}}
	s.openReportComparePicker()
	if s.nodes.compareLeftRef != "r-c" || s.nodes.compareRightRef != "r-d" {
		t.Fatalf("compare picker did not default to new workspace reports: left=%q right=%q", s.nodes.compareLeftRef, s.nodes.compareRightRef)
	}

	s.nodes.unifiedReports = []core.UnifiedReportSummary{
		{ReportSummary: core.ReportSummary{ReportId: "local-1"}, ReportRef: "local-1", NodeID: core.LocalNodeID},
		{ReportSummary: core.ReportSummary{ReportId: "remote-1"}, ReportRef: "remote://node-a/remote-1", NodeID: "node-a", IsRemote: true},
	}
	s.nodes.reportsLoaded = true
	s.clearCleanedWorkspaceState()
	if got := s.reportSummaries(); len(got) != 1 || !got[0].IsRemote {
		t.Fatalf("cleaning local artifacts did not drop local unified reports: %+v", got)
	}
	s.openReportComparePicker()
	if s.nodes.comparePickerOpen {
		t.Fatal("compare picker opened with fewer than two remaining reports after cleanup")
	}

	s.resetWorkspace()
	if s.nodes.reportsLoaded || len(s.nodes.unifiedReports) != 0 {
		t.Fatalf("resetWorkspace left stale unifiedReports: loaded=%v len=%d", s.nodes.reportsLoaded, len(s.nodes.unifiedReports))
	}
}

func TestRemoteServiceCardWithoutTunnelRendersSingleUsageButton(t *testing.T) {
	s := readyState("services")
	node := core.NodeSnapshot{
		Record:   core.NodeRecord{NodeID: "node-a", DisplayName: "Node A"},
		Online:   true,
		Services: []core.EngineState{{ServiceID: "svc-1", ModelName: "Qwen", State: "running", Port: 48857}},
	}
	s.nodes.snapshots = []core.NodeSnapshot{
		{Record: core.NodeRecord{NodeID: core.LocalNodeID}, Online: true},
		node,
	}
	s.nodes.activeID = "node-a"

	tt := ui.NewTester(s.view, 1120, 800)
	tt.Frame()
	usageLabel := l10n.TR("services.card.action.usage")
	count := 0
	for _, item := range tt.AccessAll() {
		if item.Role == ui.RoleButton && item.Name == usageLabel {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("usage button count = %d, want 1", count)
	}
}
