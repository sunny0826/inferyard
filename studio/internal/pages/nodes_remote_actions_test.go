package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

func waitForNodeAction(t *testing.T, s *viewState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.busy {
		t.Fatal("remote action did not finish")
	}
	if s.failure != nil {
		t.Fatalf("remote action failed: %+v", s.failure)
	}
}

func TestNodeOnboardingPassesEffectiveDownloadSettings(t *testing.T) {
	proxy := "http://127.0.0.1:7897"
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, err := json.Marshal(core.UserSettings{DownloadSource: core.DownloadSourceHuggingFace, DownloadProxy: &proxy})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	service, apiErr := core.New(context.Background(), core.Options{SettingsPath: settingsPath})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	s := &viewState{service: service, ctx: context.Background(), updateWindow: func(fn func()) { fn() }}
	var pairingOptions remote.PairingSessionOptions
	s.nodes.startPairingFn = func(_ context.Context, opts remote.PairingSessionOptions) (remote.PairingSession, *core.ApiError) {
		pairingOptions = opts
		return remote.PairingSession{Code: "123456"}, nil
	}
	s.openAddNodeDialog()
	if pairingOptions.DownloadSource != core.DownloadSourceHuggingFace || pairingOptions.DownloadProxy != proxy {
		t.Fatalf("pairing download options = %+v", pairingOptions)
	}

	s.nodes.sshCommand = "ssh root@example.com"
	s.nodes.sshParsedRaw = s.nodes.sshCommand
	s.nodes.sshParsed = &remote.ParsedSSHCommand{User: "root", Host: "example.com", Port: 22}
	var connectRequest remote.SSHConnectRequest
	s.nodes.connectSSHFn = func(_ context.Context, req remote.SSHConnectRequest) (core.NodeRecord, *core.ApiError) {
		connectRequest = req
		return core.NodeRecord{NodeID: "node-a"}, nil
	}
	s.nodes.setActiveNodeFn = func(string) *core.ApiError { return nil }
	s.connectSSHNode()
	waitForNodeAction(t, s)
	if connectRequest.DownloadSource != core.DownloadSourceHuggingFace || connectRequest.DownloadProxy != proxy {
		t.Fatalf("SSH download options = %+v", connectRequest)
	}
}

func TestRemoteNodeActionsBuildCoreRequests(t *testing.T) {
	s := &viewState{ctx: context.Background(), updateWindow: func(fn func()) { fn() }}
	node := core.NodeSnapshot{Record: core.NodeRecord{NodeID: "node-a", Platform: "linux/amd64"}, Online: true}
	s.nodes.snapshots = []core.NodeSnapshot{node}
	s.nodes.serviceModel, s.nodes.serviceEngine = " /models/qwen.gguf ", " /opt/engines/llama-server "
	s.nodes.serviceContext, s.nodes.servicePort = "16384", "48857"
	var serviceRequest core.NodeServiceStartRequest
	s.nodes.startServiceFn = func(_ context.Context, nodeID string, req core.NodeServiceStartRequest) (core.EngineState, *core.ApiError) {
		if nodeID != node.Record.NodeID {
			t.Fatalf("service node = %q", nodeID)
		}
		serviceRequest = req
		return core.EngineState{}, nil
	}
	s.startRemoteService(node)
	waitForNodeAction(t, s)
	if serviceRequest.Model != "/models/qwen.gguf" || serviceRequest.Engine != "/opt/engines/llama-server" || serviceRequest.ContextSize != 16384 || serviceRequest.Port != 48857 {
		t.Fatalf("service request = %+v", serviceRequest)
	}

	s.nodes.benchmarkConfigs = "/configs/a.json, /configs/b.json\n/configs/c.json"
	var benchmarkRequest core.NodeBenchmarkStartRequest
	s.nodes.startBenchmarkFn = func(_ context.Context, _ string, req core.NodeBenchmarkStartRequest) ([]core.RunTask, *core.ApiError) {
		benchmarkRequest = req
		return nil, nil
	}
	s.startRemoteBenchmark(node)
	waitForNodeAction(t, s)
	if len(benchmarkRequest.ConfigPaths) != 3 || benchmarkRequest.ConfigPaths[2] != "/configs/c.json" {
		t.Fatalf("benchmark request = %+v", benchmarkRequest)
	}

	family := core.EngineFamilyCatalog{ID: "llama-cpp"}
	release := core.EngineRelease{ID: "release-a"}
	build := core.EnginePlatformBuild{ID: "build-a", Platform: "linux/amd64", AcquireMode: core.EngineAcquireDirect}
	var installRequest core.ManagedEngineInstallRequest
	s.nodes.installEngineFn = func(_ context.Context, _ string, req core.ManagedEngineInstallRequest) (core.NodeOperation, *core.ApiError) {
		installRequest = req
		return core.NodeOperation{}, nil
	}
	s.installRemoteEngine(node, family, release, build)
	waitForNodeAction(t, s)
	if installRequest.Family != "llama-cpp" || installRequest.ReleaseID != "release-a" || installRequest.BuildID != "build-a" || installRequest.TargetPlatform != "linux/amd64" {
		t.Fatalf("install request = %+v", installRequest)
	}

	s.nodes.reportSyncNodeID, s.nodes.reportSyncID = "node-a", "report-a"
	var syncedNode, syncedReport string
	s.nodes.syncReportFn = func(_ context.Context, nodeID, reportID string) (core.UnifiedReportSummary, *core.ApiError) {
		syncedNode, syncedReport = nodeID, reportID
		return core.UnifiedReportSummary{}, nil
	}
	s.syncRemoteReport()
	waitForNodeAction(t, s)
	if syncedNode != "node-a" || syncedReport != "report-a" {
		t.Fatalf("sync request = %q / %q", syncedNode, syncedReport)
	}
}

func TestRemoteReportAvailabilityFollowsNodeSnapshot(t *testing.T) {
	s := &viewState{}
	s.nodes.snapshots = []core.NodeSnapshot{{Record: core.NodeRecord{NodeID: "node-a"}, Online: false}}
	s.nodes.unifiedReports = []core.UnifiedReportSummary{{
		ReportRef: "remote://node-a/report-a", NodeID: "node-a", IsRemote: true,
		SourceOnline: true, CanResume: true, CanRerun: true,
	}}
	s.syncCachedRemoteReportStates()
	report := s.nodes.unifiedReports[0]
	if report.SourceOnline || report.CanResume || report.CanRerun {
		t.Fatalf("offline report actions remained enabled: %+v", report)
	}
}

func TestRemoteReportNeverUsesLocalExclusivity(t *testing.T) {
	s := &viewState{reportExclusivities: map[string]string{"report-a": core.ExclusivityExclusive}}
	reference := "remote://node-a/report-a"
	if got := s.reportExclusivity(reference); got != core.ExclusivityUnknown {
		t.Fatalf("remote exclusivity = %q", got)
	}
	if s.hasCachedReportExclusivity(reference) {
		t.Fatal("remote report matched local report ID cache")
	}
	s.storeReportExclusivity(reference, core.ExclusivityExclusive)
	if _, exists := s.reportExclusivities[reference]; exists {
		t.Fatal("remote report wrote local exclusivity cache")
	}
}
