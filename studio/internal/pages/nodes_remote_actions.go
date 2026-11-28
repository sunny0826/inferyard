package pages

import (
	"strconv"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func parseRemoteServiceRequest(model, engine, contextRaw, portRaw string) (core.NodeServiceStartRequest, bool) {
	req := core.NodeServiceStartRequest{Model: strings.TrimSpace(model), Engine: strings.TrimSpace(engine)}
	contextSize, err := strconv.ParseInt(strings.TrimSpace(contextRaw), 10, 64)
	if req.Model == "" || req.Engine == "" || err != nil || contextSize <= 0 {
		return core.NodeServiceStartRequest{}, false
	}
	req.ContextSize = contextSize
	if strings.TrimSpace(portRaw) != "" {
		port, err := strconv.Atoi(strings.TrimSpace(portRaw))
		if err != nil || port < 0 || port > 65535 {
			return core.NodeServiceStartRequest{}, false
		}
		req.Port = port
	}
	return req, true
}

func splitRemoteConfigPaths(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if path := strings.TrimSpace(part); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func (s *viewState) performRemoteAction(action string, fn func() (string, *core.ApiError)) {
	if s.preview != nil {
		message, err := fn()
		s.finishWork(message, err)
		return
	}
	s.workAs(action, fn)
}

func (s *viewState) startRemoteService(node core.NodeSnapshot) {
	req, ok := parseRemoteServiceRequest(s.nodes.serviceModel, s.nodes.serviceEngine, s.nodes.serviceContext, s.nodes.servicePort)
	if !ok || !node.Online || s.busy {
		return
	}
	s.performRemoteAction("start-remote-service", func() (string, *core.ApiError) {
		start := s.nodes.startServiceFn
		if start == nil && s.service != nil {
			start = s.service.StartNodeService
		}
		if start == nil {
			return "", core.Err("not_found")
		}
		if _, err := start(s.nodeContext(), node.Record.NodeID, req); err != nil {
			return "", err
		}
		s.updateView(func() {
			s.nodes.serviceFormOpen = false
			s.syncNodeState()
		})
		return l10n.TR("nodes.remote.service_started"), nil
	})
}

func (s *viewState) startRemoteBenchmark(node core.NodeSnapshot) {
	paths := splitRemoteConfigPaths(s.nodes.benchmarkConfigs)
	if len(paths) == 0 || !node.Online || s.busy {
		return
	}
	req := core.NodeBenchmarkStartRequest{ConfigPaths: paths}
	s.performRemoteAction("start-remote-benchmark", func() (string, *core.ApiError) {
		start := s.nodes.startBenchmarkFn
		if start == nil && s.service != nil {
			start = s.service.StartNodeBenchmark
		}
		if start == nil {
			return "", core.Err("not_found")
		}
		if _, err := start(s.nodeContext(), node.Record.NodeID, req); err != nil {
			return "", err
		}
		s.updateView(func() {
			s.nodes.benchmarkFormOpen = false
			s.nodes.benchmarkConfigs = ""
			s.syncNodeState()
		})
		return l10n.TR("nodes.remote.benchmark_started"), nil
	})
}

func (s *viewState) installRemoteEngine(node core.NodeSnapshot, family core.EngineFamilyCatalog, release core.EngineRelease, build core.EnginePlatformBuild) {
	platform := nodeTargetPlatform(node)
	if !node.Online || s.busy || (!build.CanManagedVenvInstallOn(platform) && !build.CanDirectDownloadOn(platform)) {
		return
	}
	req := core.ManagedEngineInstallRequest{
		Family: family.ID, ReleaseID: release.ID, BuildID: build.ID, TargetPlatform: platform,
	}
	s.performRemoteAction("install-remote-engine", func() (string, *core.ApiError) {
		install := s.nodes.installEngineFn
		if install == nil && s.service != nil {
			install = s.service.InstallNodeManagedEngine
		}
		if install == nil {
			return "", core.Err("not_found")
		}
		if _, err := install(s.nodeContext(), node.Record.NodeID, req); err != nil {
			return "", err
		}
		s.updateView(s.syncNodeState)
		return l10n.TR("nodes.remote.engine_install_started"), nil
	})
}

func (s *viewState) syncRemoteReport() {
	nodeID, reportID := s.nodes.reportSyncNodeID, strings.TrimSpace(s.nodes.reportSyncID)
	if nodeID == "" || reportID == "" || s.busy {
		return
	}
	online := false
	for _, node := range s.onlineRemoteNodes() {
		online = online || node.Record.NodeID == nodeID
	}
	if !online {
		return
	}
	s.performRemoteAction("sync-remote-report", func() (string, *core.ApiError) {
		sync := s.nodes.syncReportFn
		if sync == nil && s.service != nil {
			sync = s.service.SyncNodeReport
		}
		if sync == nil {
			return "", core.Err("not_found")
		}
		if _, err := sync(s.nodeContext(), nodeID, reportID); err != nil {
			return "", err
		}
		s.updateView(func() {
			s.nodes.reportSyncOpen = false
			s.nodes.reportSyncID = ""
			s.refreshUnifiedReports()
		})
		return l10n.TR("nodes.reports.synced"), nil
	})
}
