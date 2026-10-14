package pages

import "inferyard/studio/internal/core"

func (s *viewState) resetWorkspace() {
	s.initDir, s.engine, s.process, s.bindEndpoint, s.candidate = "", "", "", "", ""
	s.enginePreset, s.assetsPreset, s.bindingStale = false, false, false
	s.engineState, s.startup, s.processes = core.EngineState{}, core.Startup{}, nil
	s.cliInstallStatus, s.engineDownloadStatus, s.refreshFailure = "", "", ""
	s.runProgress, s.progress, s.resumeSource, s.detail = nil, nil, nil, nil
	s.firstBundle, s.secondBundle, s.manualFirst, s.manualSecond = "", "", "", ""
	s.manualConfig, s.single, s.confirmed = false, false, false
	s.endpoint, s.pid, s.detailID = "", "", ""
	s.detailMu.Lock()
	s.requestedDetail = ""
	s.detailMu.Unlock()
	s.groups = nil
	s.reportChoices.Clear()
	s.denied = map[string]bool{}
}
