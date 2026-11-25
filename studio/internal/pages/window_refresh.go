package pages

import (
	"time"

	"inferyard/studio/internal/core"
)

func (s *viewState) refresh(full bool) {
	if s.preview != nil {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	settingsEpoch := s.settingsSequence()
	s.win.Update(func() { s.refreshing = true })
	device, tasks, downloads := s.service.Device(), s.service.RunTasks(), s.service.Downloads()
	speedTasks, speedHistoryError := s.service.ContextSpeedHistory()
	s.detailMu.Lock()
	id := s.requestedDetail
	s.detailMu.Unlock()
	var detail *core.RunTaskDetail
	var de *core.ApiError
	if id != "" {
		d, e := s.service.RunDetail(id)
		de = e
		if e == nil {
			detail = &d
		}
	}
	var settings core.SettingsResponse
	var ready core.Readiness
	var progress core.ProgressResponse
	var history core.RunHistoryResponse
	var reports []core.ReportSummary
	var he, re, pe *core.ApiError
	var dashboard dashboardSnapshot
	var runProgress *core.RunProgress
	if active := activeRunTask(tasks); active != nil {
		if d, e := s.service.RunDetail(active.TaskId); e == nil {
			runProgress = d.Progress
		}
	}
	settings = s.service.Settings()
	updateState := s.service.UpdateStatus()
	cliUpgrade := s.service.CLIUpgradeStatus()
	var modelEvidence map[string]core.ModelEvidenceSummary
	var reportExclusivities map[string]string
	if full {
		history, he = s.service.History()
		_, reports, re = s.service.Reports()
		modelEvidence = s.service.ModelBenchmarkEvidence()
		reportExclusivities = s.service.ReportExclusivities(reports)
	}
	engineUpdateStatus := s.service.EngineUpdateStatus()
	engineUpdates := engineUpdateStatus.Updates
	ready = s.service.Readiness()
	if ready.CLI.Status == "ok" && ready.Workspace.Initialized {
		progress, pe = s.service.Progress(s.ctx)
	}
	dashboard = s.readDashboard(progress.Progress)
	var registry core.Registry
	if full {
		registry = s.service.Registry()
	}
	serveSupported := s.service.ServeSupported(s.ctx)
	recentServices := s.service.RecentServices()
	exitedNotices := s.service.ExitedServiceNotices()
	managedServices := s.service.ManagedServices()
	memorySummary := s.service.CurrentServiceMemorySummary()
	if s.ctx.Err() != nil {
		return
	}
	verifyVisible := false
	s.win.Update(func() {
		if s.ctx.Err() != nil {
			return
		}
		s.refreshing = false
		batch := refreshBatch{
			epoch: settingsEpoch, full: full, device: device, tasks: tasks, downloads: downloads,
			speedTasks: speedTasks, speedHistoryError: speedHistoryError, runProgress: runProgress,
			id: id, detail: detail, detailError: de, settings: settings,
			history: history, reports: reports, historyError: he, reportsError: re,
			ready: ready, progress: progress, progressError: pe, dashboard: dashboard, registry: registry,
			updateState: updateState, cliUpgrade: cliUpgrade, serveSupported: serveSupported,
			recentServices: recentServices, exitedNotices: exitedNotices,
			managedServices: managedServices, memorySummary: memorySummary,
			modelEvidence: modelEvidence, reportExclusivities: reportExclusivities,
			engineUpdates: engineUpdates, engineUpdateStatus: engineUpdateStatus,
		}
		if !s.applyRefreshBatch(batch) {
			return
		}
		curPage := canonicalPageID(s.selected)
		verifyVisible = curPage == "library" ||
			(curPage == "benchmarks" && s.progress != nil && !s.progress.DefaultScopeComplete && s.progress.NextAction == "prepare_candidate")
	})
	if s.shouldVerifyEngines(full, verifyVisible, time.Now()) {
		s.service.VerifyEngines()
	}
}

type refreshBatch struct {
	epoch                      uint64
	full                       bool
	device                     core.DeviceResponse
	tasks                      []core.RunTask
	downloads                  []core.DownloadTask
	speedTasks                 []core.RunTask
	speedHistoryError          *core.ApiError
	runProgress                *core.RunProgress
	id                         string
	detail                     *core.RunTaskDetail
	detailError                *core.ApiError
	settings                   core.SettingsResponse
	history                    core.RunHistoryResponse
	reports                    []core.ReportSummary
	historyError, reportsError *core.ApiError
	ready                      core.Readiness
	progress                   core.ProgressResponse
	progressError              *core.ApiError
	dashboard                  dashboardSnapshot
	registry                   core.Registry
	updateState                core.UpdateState
	cliUpgrade                 core.CLIUpgradeState
	serveSupported             bool
	recentServices             []core.ServicePreset
	exitedNotices              []core.ServiceExitNotice
	managedServices            []core.EngineState
	memorySummary              core.ServiceMemoryBudget
	modelEvidence              map[string]core.ModelEvidenceSummary
	reportExclusivities        map[string]string
	engineUpdates              map[string]core.EngineUpdateInfo
	engineUpdateStatus         core.EngineUpdatesStatus
}

func (s *viewState) applyRefreshBatch(b refreshBatch) bool {
	if !s.refreshBatchCurrent(b.epoch) {
		return false
	}
	s.device, s.tasks, s.downloads = b.device, b.tasks, b.downloads
	s.reconcileLocalPIDs(b.tasks)
	if b.speedHistoryError == nil {
		s.syncSpeedTasks(b.speedTasks)
	}
	s.runProgress = b.runProgress
	if s.detailID == b.id {
		if b.detailError == nil {
			s.detail = b.detail
		}
		s.detailError = b.detailError
	}
	s.applySettings(b.settings)
	skipEngineAdopt := false
	skipModelAdopt := false
	if b.full {
		if b.historyError == nil {
			s.history = b.history
		}
		if b.reportsError == nil {
			s.reports = b.reports
		}
		s.historyError, s.reportsError = b.historyError, b.reportsError
		if b.modelEvidence != nil {
			s.modelEvidence = b.modelEvidence
		}
		if b.reportExclusivities != nil {
			s.reportExclusivities = b.reportExclusivities
		}
		prevEngineName, prevModelName := s.engine, s.model
		prevEnginePath := s.selectedEnginePath()
		prevModelPath := s.selectedModelPath()
		s.registry = b.registry
		if s.applyPendingPolystrataSelection(prevEngineName, prevModelName) {
			skipEngineAdopt = true
			skipModelAdopt = true
		} else {
			skipEngineAdopt = s.reconcileSelectedEngine(prevEnginePath)
			skipModelAdopt = s.reconcileSelectedModel(prevModelPath)
		}
	}
	if b.engineUpdates != nil {
		s.engineUpdates = b.engineUpdates
	}
	if b.engineUpdateStatus.State != "" || b.engineUpdateStatus.Updates != nil {
		s.engineUpdateStatus = b.engineUpdateStatus
	}
	s.ready, s.progress = b.ready, b.progress.Progress
	s.applyDashboard(b.dashboard, b.progressError)
	s.engineState = s.service.Engine()
	s.checks = s.service.EngineChecks()
	s.serveSupported = b.serveSupported
	s.recentServices = b.recentServices
	s.managedServices = b.managedServices
	s.memorySummary = b.memorySummary
	s.syncExitedNotices(b.exitedNotices)
	s.applyUpdateState(b.updateState)
	s.applyCLIUpgradeState(b.cliUpgrade)
	if !skipEngineAdopt {
		s.adoptDownloadedEngine()
	}
	if !skipModelAdopt {
		s.adoptOnlyModel()
	}
	s.loaded = true
	cliKey := emptyText(b.ready.CLI.Path) + "|" + emptyText(b.ready.CLI.Version) + "|" + b.ready.CLI.Status
	if cliKey != s.discoveryCLI {
		s.discoveryCLI, s.discoveryLoaded = cliKey, false
	}
	if !s.discoveryLoaded && !s.discovering {
		s.refreshDiscovery()
	}
	return true
}
