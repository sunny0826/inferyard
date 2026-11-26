package pages

import (
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type bgRefreshCache struct {
	epoch    uint64
	hasEpoch bool
	history  core.RunHistoryResponse
	reports  []core.ReportSummary
	progress *inferyard.WorkspaceProgress
}

func (s *viewState) runRefreshUI(fn func()) {
	if s.win != nil {
		s.win.Update(fn)
		return
	}
	if s.updateWindow != nil {
		s.updateWindow(fn)
		return
	}
	fn()
}

// bootstrap hydrates fast local state (Readiness, Settings, service snapshot,
// in-memory tasks/downloads/device, and local Registry) so primary views like
// Services, Library, and Settings paint immediately without waiting on
// History/Reports disk scans or progress subprocesses.
func (s *viewState) bootstrap() {
	if s.preview != nil || s.service == nil {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	settingsEpoch := s.settingsSequence()
	ready := s.service.Readiness()
	settings := s.service.Settings()
	device, tasks, downloads := s.service.Device(), s.service.RunTasks(), s.service.Downloads()
	updateState := s.service.UpdateStatus()
	cliUpgrade := s.service.CLIUpgradeStatus()
	engineUpdateStatus := s.service.EngineUpdateStatus()
	registry := s.service.Registry()
	checks := s.service.EngineChecks()
	svcSnap := s.captureServiceStateSnapshot()
	if s.ctx.Err() != nil {
		return
	}
	probingServe := ready.CLI.Status == "ok"
	svc, ctx := s.service, s.ctx
	s.runRefreshUI(func() {
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		if !s.refreshBatchCurrent(settingsEpoch) || s.loaded {
			return
		}
		s.ready = ready
		s.applySettings(settings)
		s.device, s.tasks, s.downloads = device, tasks, downloads
		s.reconcileLocalPIDs(tasks)
		s.registry = registry
		s.checks = checks
		s.applyServiceStateSnapshot(svcSnap)
		s.applyUpdateState(updateState)
		s.applyCLIUpgradeState(cliUpgrade)
		if engineUpdateStatus.Updates != nil {
			s.engineUpdates = engineUpdateStatus.Updates
		}
		if engineUpdateStatus.State != "" || engineUpdateStatus.Updates != nil {
			s.engineUpdateStatus = engineUpdateStatus
		}
		s.adoptDownloadedEngine()
		s.adoptOnlyModel()
		if s.initDir == "" {
			s.initDir = emptyText(ready.Workspace.Path)
		}
		s.serveProbing = probingServe
		s.hydrating = true
		s.refreshing = true
		s.loaded = true
	})
	if probingServe && s.win != nil {
		go func() {
			if ctx.Err() != nil {
				return
			}
			supported := svc.ServeSupported(ctx)
			if ctx.Err() != nil {
				return
			}
			s.runRefreshUI(func() {
				if !s.refreshBatchCurrent(settingsEpoch) {
					return
				}
				s.serveSupported = supported
				s.serveProbing = false
			})
		}()
	}
}

func (s *viewState) refresh(full bool) {
	if full {
		s.refreshMask(refreshAll)
		return
	}
	s.refreshMask(refreshCore | refreshProgress)
}

func (s *viewState) refreshMask(mask refreshMask) {
	if s.preview != nil || s.service == nil || mask == refreshNone {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	settingsEpoch := s.settingsSequence()
	if !s.bgRefresh.hasEpoch || s.bgRefresh.epoch != settingsEpoch {
		s.bgRefresh = bgRefreshCache{epoch: settingsEpoch, hasEpoch: true}
	}
	s.runRefreshUI(func() { s.refreshing = true })

	device, tasks, downloads := s.service.Device(), s.service.RunTasks(), s.service.Downloads()
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
	var runProgress *core.RunProgress
	if active := activeRunTask(tasks); active != nil {
		if d, e := s.service.RunDetail(active.TaskId); e == nil {
			runProgress = d.Progress
		}
	}

	settings := s.service.Settings()
	updateState := s.service.UpdateStatus()
	cliUpgrade := s.service.CLIUpgradeStatus()
	engineUpdateStatus := s.service.EngineUpdateStatus()
	engineUpdates := engineUpdateStatus.Updates
	ready := s.service.Readiness()

	var speedTasks []core.RunTask
	var speedHistoryError *core.ApiError
	var history core.RunHistoryResponse
	var he *core.ApiError
	if mask&refreshHistory != 0 {
		speedTasks, speedHistoryError = s.service.ContextSpeedHistory()
		history, he = s.service.History()
		if he == nil {
			s.bgRefresh.history = history
		}
	}

	var reports []core.ReportSummary
	var re *core.ApiError
	var reportExclusivities map[string]string
	if mask&refreshReports != 0 {
		_, reports, re = s.service.Reports()
		if re == nil {
			s.bgRefresh.reports = reports
			reportExclusivities = s.service.ReportExclusivities(reports)
		}
	}

	effHistory := s.bgRefresh.history
	effReports := s.bgRefresh.reports

	var registry core.Registry
	var modelEvidence map[string]core.ModelEvidenceSummary
	hasEvidence := false
	if mask&refreshRegistry != 0 {
		registry, modelEvidence = s.service.RegistryAndEvidence(effReports, effHistory)
		hasEvidence = true
	} else if (mask&refreshReports != 0 && re == nil) || (mask&refreshHistory != 0 && he == nil) {
		modelEvidence = s.service.ModelBenchmarkEvidenceWith(effReports, effHistory)
		hasEvidence = true
	}

	var progress core.ProgressResponse
	var pe *core.ApiError
	var dashboard dashboardSnapshot
	if mask&refreshProgress != 0 {
		if ready.CLI.Status == "ok" && ready.Workspace.Initialized {
			progress, pe = s.service.ProgressCached(s.ctx)
		}
		if pe == nil {
			s.bgRefresh.progress = progress.Progress
		} else {
			s.bgRefresh.progress = nil
		}
		dashboard = s.readDashboard(progress.Progress)
	} else {
		dashboard = s.readDashboard(s.bgRefresh.progress)
	}

	serveSupported := s.service.ServeSupported(s.ctx)
	svcSnap := s.captureServiceStateSnapshot()
	if s.ctx.Err() != nil {
		return
	}
	svc := s.service
	s.runRefreshUI(func() {
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		s.refreshing = false
		batch := refreshBatch{
			epoch: settingsEpoch, mask: mask, device: device, tasks: tasks, downloads: downloads,
			speedTasks: speedTasks, speedHistoryError: speedHistoryError, runProgress: runProgress,
			id: id, detail: detail, detailError: de, settings: settings,
			history: history, reports: reports, historyError: he, reportsError: re,
			ready: ready, progress: progress, progressError: pe, dashboard: dashboard, registry: registry,
			updateState: updateState, cliUpgrade: cliUpgrade, serveSupported: serveSupported,
			serviceSnap: svcSnap, hasEvidence: hasEvidence,
			modelEvidence: modelEvidence, reportExclusivities: reportExclusivities,
			engineUpdates: engineUpdates, engineUpdateStatus: engineUpdateStatus,
		}
		if !s.applyRefreshBatch(batch) {
			return
		}
		if mask&refreshEngineVfy == 0 || svc == nil {
			return
		}
		curPage := canonicalPageID(s.selected)
		verifyVisible := curPage == "library" ||
			(curPage == "services" && (s.serviceParams.open || s.standalone.drawerOpen)) ||
			(curPage == "benchmarks" && s.benchmarkNeedsEngineVerify())
		if s.shouldVerifyEngines(mask == refreshAll, verifyVisible, time.Now()) {
			go svc.VerifyEngines()
		}
	})
}

func (s *viewState) benchmarkNeedsEngineVerify() bool {
	if s.task.stage == "prepare_candidate" {
		return true
	}
	if s.progress == nil || s.progress.DefaultScopeComplete {
		return false
	}
	return s.progress.NextAction == "prepare_candidate" ||
		(s.progress.NextAction == "device_check" && s.hasLocalEngine())
}

type refreshBatch struct {
	epoch                      uint64
	full                       bool
	mask                       refreshMask
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
	serviceSnap                serviceStateSnapshot
	hasEvidence                bool
	modelEvidence              map[string]core.ModelEvidenceSummary
	reportExclusivities        map[string]string
	engineUpdates              map[string]core.EngineUpdateInfo
	engineUpdateStatus         core.EngineUpdatesStatus
}

func (s *viewState) applyRefreshBatch(b refreshBatch) bool {
	if !s.refreshBatchCurrent(b.epoch) {
		return false
	}
	mask := b.mask
	if b.full && mask == refreshNone {
		mask = refreshAll
	}
	s.device, s.tasks, s.downloads = b.device, b.tasks, b.downloads
	s.reconcileLocalPIDs(b.tasks)
	s.runProgress = b.runProgress
	if s.detailID == b.id {
		if b.detailError == nil {
			s.detail = b.detail
		}
		s.detailError = b.detailError
	}
	s.applySettings(b.settings)
	if mask&refreshHistory != 0 {
		if b.speedHistoryError == nil {
			s.syncSpeedTasks(b.speedTasks)
		}
		if b.historyError == nil {
			s.history = b.history
		}
		s.historyError = b.historyError
	}
	if mask&refreshReports != 0 {
		if b.reportsError == nil {
			s.reports = b.reports
		}
		s.reportsError = b.reportsError
		if b.reportExclusivities != nil {
			s.reportExclusivities = b.reportExclusivities
		}
	}
	if (b.hasEvidence || b.full) && b.modelEvidence != nil {
		s.modelEvidence = b.modelEvidence
	}
	if mask&refreshRegistry != 0 {
		prevEngineName, prevModelName := s.engine, s.model
		prevEnginePath := s.selectedEnginePath()
		prevModelPath := s.selectedModelPath()
		s.registry = b.registry
		skipEngineAdopt := false
		skipModelAdopt := false
		if s.applyPendingPolystrataSelection(prevEngineName, prevModelName) {
			skipEngineAdopt = true
			skipModelAdopt = true
		} else {
			skipEngineAdopt = s.reconcileSelectedEngine(prevEnginePath)
			skipModelAdopt = s.reconcileSelectedModel(prevModelPath)
		}
		if !skipEngineAdopt {
			s.adoptDownloadedEngine()
		}
		if !skipModelAdopt {
			s.adoptOnlyModel()
		}
	}
	if b.engineUpdates != nil {
		s.engineUpdates = b.engineUpdates
	}
	if b.engineUpdateStatus.State != "" || b.engineUpdateStatus.Updates != nil {
		s.engineUpdateStatus = b.engineUpdateStatus
	}
	s.ready = b.ready
	if mask&refreshProgress != 0 {
		s.progress = b.progress.Progress
		s.applyDashboard(b.dashboard, b.progressError)
	} else if s.progress != nil && b.dashboard.failure == nil {
		s.startup, s.bindingStale = b.dashboard.startup, b.dashboard.stale
	}
	s.checks = s.service.EngineChecks()
	s.serveSupported = b.serveSupported
	s.serveProbing = false
	s.applyServiceStateSnapshot(b.serviceSnap)
	s.applyUpdateState(b.updateState)
	s.applyCLIUpgradeState(b.cliUpgrade)
	if mask&(refreshHistory|refreshReports) == (refreshHistory|refreshReports) {
		s.hydrating = false
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
