//go:build ui_debug && !release

package pages

import (
	"context"
	"strings"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

const (
	previewWinNodeID   = "node-win-4090"
	previewCloudNodeID = "node-autodl-h100"
	previewRemoteRef   = "remote://node-win-4090/report-win"
)

func seedPreviewNodes(s *viewState) {
	local := core.NodeSnapshot{
		Record: core.NodeRecord{NodeID: core.LocalNodeID, DisplayName: "Mac M3 Max", OS: "darwin", Arch: "arm64", Platform: "darwin/arm64", HardwareSummary: "macOS · M3 Max 48GB", Transport: core.NodeTransportLocal},
		Online: true, Services: s.managedServices,
	}
	win := core.NodeSnapshot{
		Record: core.NodeRecord{NodeID: previewWinNodeID, DisplayName: "Win-RTX4090", OS: "windows", Arch: "amd64", Platform: "windows/amd64", HardwareSummary: "Windows 11 · RTX 4090 24GB", Transport: core.NodeTransportMTLS},
		Online: true,
		Services: []core.EngineState{{
			ServiceID: "qwen-win", State: "running", ModelName: "Qwen3-32B-Q4_K_M", EngineName: "PolyStrata (CUDA)",
			Port: 48857, Endpoint: "http://127.0.0.1:48857", ServedModel: "Qwen3-32B-Q4_K_M", RuntimeMemoryGB: previewValue(19.4),
		}},
	}
	cloud := core.NodeSnapshot{
		Record: core.NodeRecord{NodeID: previewCloudNodeID, DisplayName: "AutoDL-H100", OS: "linux", Arch: "amd64", Platform: "linux/amd64", HardwareSummary: "Ubuntu 24.04 · H100 80GB", Transport: core.NodeTransportSSH},
		Online: false,
	}
	s.nodes.snapshots = []core.NodeSnapshot{local, win, cloud}
	s.nodes.activeID = previewWinNodeID
	s.nodes.setActiveNodeFn = func(nodeID string) *core.ApiError { return nil }
	s.nodes.openTunnelFn = func(_ context.Context, req remote.OpenTunnelRequest) (remote.TunnelInfo, *core.ApiError) {
		tunnel := remote.TunnelInfo{TunnelID: "tunnel-preview-new", NodeID: req.NodeID, NodeName: req.NodeName, ServiceID: req.ServiceID, ModelName: req.ModelName, LocalPort: 18003, LocalURL: "http://127.0.0.1:18003/v1", RemotePort: req.RemotePort, CreatedAt: time.Now()}
		s.nodes.tunnels = append(s.nodes.tunnels, tunnel)
		return tunnel, nil
	}
	s.nodes.closeTunnelFn = func(tunnelID string) *core.ApiError {
		kept := s.nodes.tunnels[:0]
		for _, tunnel := range s.nodes.tunnels {
			if tunnel.TunnelID != tunnelID {
				kept = append(kept, tunnel)
			}
		}
		s.nodes.tunnels = kept
		return nil
	}
	s.nodes.decidePairingFn = func(decision remote.PairingDecision) (core.NodeRecord, *core.ApiError) {
		return win.Record, nil
	}
	s.nodes.decideHostKeyFn = func(decision remote.HostKeyDecision) *core.ApiError { return nil }
	s.nodes.parseSSHFn = func(raw string) (remote.ParsedSSHCommand, *core.ApiError) {
		if strings.ContainsAny(raw, ";|&`") {
			return remote.ParsedSSHCommand{}, core.Err("invalid_request")
		}
		return remote.ParsedSSHCommand{User: "root", Host: "connect.westb.seetacloud.com", Port: 38291}, nil
	}
	s.nodes.connectSSHFn = func(context.Context, remote.SSHConnectRequest) (core.NodeRecord, *core.ApiError) {
		return cloud.Record, nil
	}
	s.nodes.compareReportsFn = func(left, right string) (*core.ReportComparison, *core.ApiError) {
		return previewNodeComparison(left, right), nil
	}
	s.nodes.cleanNodeFn = func(context.Context, string) (core.WorkspaceCleanupResult, *core.ApiError) {
		return core.WorkspaceCleanupResult{}, nil
	}
	s.nodes.cleanReportCacheFn = func(...string) (core.WorkspaceCleanupResult, *core.ApiError) {
		kept := s.nodes.unifiedReports[:0]
		for _, report := range s.nodes.unifiedReports {
			if !report.IsRemote {
				kept = append(kept, report)
			}
		}
		s.nodes.unifiedReports = kept
		return core.WorkspaceCleanupResult{}, nil
	}
	seedPreviewNodeActions(s)
}

func previewPairingSession() *remote.PairingSession {
	return &remote.PairingSession{
		Code: "839204", FormattedCode: "839-204",
		WindowsCommand: "irm http://100.84.1.2:48860/join/839204.ps1 | iex",
		UnixCommand:    "curl -fsSL http://100.84.1.2:48860/join/839204 | sh",
	}
}

func previewPairingRequest() remote.PairingRequest {
	return remote.PairingRequest{
		RequestID: "pair-preview", Code: "839204", FormattedCode: "839-204", Hostname: "Win-RTX4090",
		OS: "windows", Arch: "amd64", Platform: "windows/amd64", HardwareSummary: "RTX 4090 24GB · 64GB RAM",
		SourceIP: "100.84.12.9", SourceHint: "Tailscale", Options: []remote.PairingOption{remote.PairingApprove, remote.PairingReject},
	}
}

func previewHostKeyRequest() remote.HostKeyRequest {
	return remote.HostKeyRequest{
		RequestID: "hostkey-preview", Host: "connect.westb.seetacloud.com", Port: 38291,
		KeyType: "ssh-ed25519", FingerprintSHA256: "SHA256:7m2K9previewFingerprint",
		Options: []remote.HostKeyOption{remote.HostKeyTrustOnce, remote.HostKeyTrustAndSave, remote.HostKeyReject},
	}
}

func previewNodePairingScene(s *viewState) {
	seedPreviewNodes(s)
	s.nodes.activeID = core.LocalNodeID
	s.nodes.addOpen, s.nodes.addMode, s.nodes.pairing = true, 0, previewPairingSession()
}

func previewNodePairingConfirmScene(s *viewState) {
	previewNodePairingScene(s)
	s.nodes.pairingRequests = []remote.PairingRequest{previewPairingRequest()}
}

func previewNodeSSHScene(s *viewState) {
	seedPreviewNodes(s)
	s.nodes.activeID = core.LocalNodeID
	s.nodes.addOpen, s.nodes.addMode = true, 1
	s.nodes.sshCommand = "ssh -p 38291 root@connect.westb.seetacloud.com"
	s.nodes.hostKeyRequests = []remote.HostKeyRequest{previewHostKeyRequest()}
}

func previewNodeRemoteServicesScene(s *viewState) {
	seedPreviewNodes(s)
	s.selected = "services"
	s.nodes.tunnels = []remote.TunnelInfo{
		{TunnelID: "tunnel-win", NodeID: previewWinNodeID, NodeName: "Win-RTX4090", ServiceID: "qwen-win", ModelName: "Qwen3-32B-Q4_K_M", LocalPort: 18001, LocalURL: "http://127.0.0.1:18001/v1", RemotePort: 48857},
		{TunnelID: "tunnel-cloud", NodeID: previewCloudNodeID, NodeName: "AutoDL-H100", ServiceID: "qwen-cloud", ModelName: "Qwen3-32B-Q4_K_M", LocalPort: 18002, LocalURL: "http://127.0.0.1:18002/v1", RemotePort: 48858},
	}
}

func previewNodeReportsScene(s *viewState) {
	seedPreviewNodes(s)
	s.selected = "reports"
	local := core.UnifiedReportSummary{ReportSummary: previewReportSummary("preview-report"), ReportRef: "preview-report", NodeID: core.LocalNodeID, NodeName: "Mac M3 Max", SourceOnline: true}
	remoteSummary := core.UnifiedReportSummary{ReportSummary: previewReportSummary("compare-report"), ReportRef: previewRemoteRef, NodeID: previewWinNodeID, NodeName: "Win-RTX4090", IsRemote: true, SourceOnline: true, VerifiedFromRaw: true, CanResume: true, CanRerun: true}
	s.nodes.unifiedReports = []core.UnifiedReportSummary{local, remoteSummary}
	s.nodes.reportsLoaded = true
	s.nodes.compareLeftRef, s.nodes.compareRightRef = "preview-report", previewRemoteRef
	s.nodes.comparison = previewNodeComparison(s.nodes.compareLeftRef, s.nodes.compareRightRef)
	s.nodes.compareOpen = true
}

func previewNodeComparison(left, right string) *core.ReportComparison {
	passed, valid := int64(104), int64(120)
	rate := 104.0 / 120.0
	leftLatencyP50, rightLatencyP50 := 980.0, 420.0
	leftLatencyP95, rightLatencyP95 := 1840.0, 760.0
	leftTTFTP50, leftTTFTP95 := 82.0, 160.0
	leftOutput, rightOutput := 31.2, 68.4
	leftMemory, rightMemory := 20.1*float64(1<<30), 19.4*float64(1<<30)
	leftTemp := 74.2
	ratio := rightOutput / leftOutput
	quality := core.ReportCompareMetrics{Passed: &passed, Valid: &valid, Rate: &rate}
	leftMetrics := &core.ReportCompareMetrics{
		LatencyP50Ms: &leftLatencyP50, LatencyP95Ms: &leftLatencyP95, TTFTP50MS: &leftTTFTP50, TTFTP95MS: &leftTTFTP95,
		OutputTokensPerSecond: &leftOutput, PeakMemoryBytes: &leftMemory, PeakWorkingSetBytes: &leftMemory, PeakTemperatureCelsius: &leftTemp,
	}
	rightMetrics := &core.ReportCompareMetrics{
		LatencyP50Ms: &rightLatencyP50, LatencyP95Ms: &rightLatencyP95,
		OutputTokensPerSecond: &rightOutput, PeakMemoryBytes: &rightMemory, PeakWorkingSetBytes: &rightMemory,
	}
	pass := "pass"
	latency := 118.0
	return &core.ReportComparison{
		LeftRef: left, RightRef: right, LeftQuality: quality, RightQuality: quality,
		Runs: []core.ReportCompareRun{{
			LeftRunID: "run-mac", RightRunID: "run-win", Model: "Qwen3-32B-Q4_K_M",
			LeftModel: "Qwen3-32B-Q4_K_M", RightModel: "Qwen3-32B-Q8_0", Left: leftMetrics, Right: rightMetrics, SpeedupRatio: &ratio,
		}},
		Cases: []core.ReportCompareCase{{CaseID: "zh-core-001", Left: &core.ReportRequest{CaseID: "zh-core-001", Quality: &pass, LatencyMS: &latency}, Right: &core.ReportRequest{CaseID: "zh-core-001", Quality: &pass, LatencyMS: &latency}}},
	}
}
