//go:build ui_debug && !release

package pages

import (
	"context"

	"inferyard/studio/internal/core"
)

func seedPreviewNodeActions(s *viewState) {
	s.nodes.fetchRemoteServiceLogs = func(nodeID, serviceID string) (string, *core.ApiError) {
		return "preview logs for " + serviceID + " on " + nodeID + "\nready for requests", nil
	}
	s.nodes.startServiceFn = func(_ context.Context, nodeID string, req core.NodeServiceStartRequest) (core.EngineState, *core.ApiError) {
		service := core.EngineState{
			ServiceID: "preview-started", State: "running", ModelName: req.Model, EngineName: req.Engine,
			ServedModel: req.Model, ContextSize: req.ContextSize, Port: req.Port,
		}
		for i := range s.nodes.snapshots {
			if s.nodes.snapshots[i].Record.NodeID == nodeID {
				s.nodes.snapshots[i].Services = append(s.nodes.snapshots[i].Services, service)
			}
		}
		return service, nil
	}
	s.nodes.startBenchmarkFn = func(_ context.Context, _ string, req core.NodeBenchmarkStartRequest) ([]core.RunTask, *core.ApiError) {
		return []core.RunTask{{TaskId: "preview-remote-benchmark", State: "running"}}, nil
	}
	s.nodes.installEngineFn = func(_ context.Context, _ string, _ core.ManagedEngineInstallRequest) (core.NodeOperation, *core.ApiError) {
		return core.NodeOperation{OperationID: "preview-engine-install", State: "submitted"}, nil
	}
	s.nodes.syncReportFn = func(_ context.Context, nodeID, reportID string) (core.UnifiedReportSummary, *core.ApiError) {
		report := core.UnifiedReportSummary{
			ReportSummary: previewReportSummary(reportID), ReportRef: core.RemoteReportReference(nodeID, reportID),
			NodeID: nodeID, NodeName: s.remoteNodeDisplayName(nodeID), IsRemote: true, SourceOnline: true,
		}
		s.nodes.unifiedReports = append(s.nodes.unifiedReports, report)
		s.nodes.reportsLoaded = true
		return report, nil
	}
}
