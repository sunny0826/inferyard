//go:build ui_debug && !release

package pages

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (p *previewSession) dispatch(intent previewIntent) {
	intent.previewIntent()
	s := p.state
	s.failure, p.noticeKey = nil, ""
	switch op := intent.(type) {
	case previewManagedEngine:
		p.startService(false, op.candidate)
	case previewStandaloneStart:
		p.startPreviewStandalone(op)
	case previewSaveServicePreset:
		p.savePreviewServicePreset(op.preset)
	case previewDismissExitNotice:
		p.dismissPreviewExitNotice(op.id)
	case previewStopManagedService:
		p.stopPreviewManagedService(op.serviceID, op.after)
	case previewTestServiceEndpoint:
		p.testPreviewServiceEndpoint(op)
	case previewStopEngineThen:
		p.stopPreviewEngine(op.after)
	case previewSpeedStart:
		p.startSpeed(op)
	case previewSpeedCancel:
		p.cancelSpeed(nil)
	case previewSpeedRead:
		p.readSpeed(op.id)
	case previewBenchmark:
		p.beginBenchmark(op)
	case previewDownload:
		p.startDownload(op)
	case previewPolystrataSetup:
		p.startPolystrataSetup(op.req)
	case previewCancelDownload:
		p.cancelDownload(op.id)
	case previewRetryDownload:
		p.retryDownload(op.id)
	case previewBrowsePath:
		p.browse(op.key)
	case previewSettings:
		p.saveSettings(op.patch)
	case previewInstallation:
		p.installation(op)
	case previewLocalBinding:
		p.bindLocal(op)
	case previewCreateReport:
		p.createReportLater(op.paths)
	case previewStartRun:
		s.resumeSource = nil
		p.beginBenchmark(previewBenchmark{model: s.model, engine: s.engine, bundles: []string{op.bundle}, resumePath: op.resumePath})
	case previewCancelRun:
		p.cancelRun(op.id)
	case previewUpdateCheck:
		p.checkUpdate()
	case previewUpdateStart:
		p.startUpdateDownload(true)
	case previewUpdateCancel:
		p.cancelUpdate()
	case previewUpdateApply:
		p.applyUpdate(op.confirmStopWorkloads)
	case previewUpdateRetry:
		p.retryUpdate()
	case previewStep:
		p.dispatchStep(op)
	default:
		p.showNotice()
	}
}

func (p *previewSession) retryDownload(id string) {
	for _, task := range p.state.downloads {
		if task.TaskId == id && (task.State == "failed" || task.State == "cancelled") {
			if p.state.downloadRetryActive(task) {
				return
			}
			if task.Kind == "polystrata" {
				if saved, ok := p.polystrataReqs[task.TaskId]; ok {
					p.startPolystrataSetup(saved)
					return
				}
				p.startPolystrataSetup(previewPolystrataRequestFromTask(task, p.state.defaultPolystrataSetupRequest()))
				return
			}
			p.startDownload(previewDownload{kind: task.Kind, source: task.SourceUrl})
			return
		}
	}
}

func (p *previewSession) createReportLater(paths []string) {
	s := p.state
	fail := p.takeFailure()
	s.busy = true
	p.after("report", 600*time.Millisecond, func() {
		s.busy = false
		if fail {
			s.failure = core.Err("report_unreadable")
			return
		}
		p.createReport(paths)
		p.noticeKey = "preview.notice.report_created"
	})
}

func (p *previewSession) dispatchStep(op previewStep) {
	s := p.state
	switch op {
	case previewPrepareEnvironment:
		p.prepareEnvironment()
	case previewCheckDevice:
		p.checkDevice()
	case previewStartEngine, previewRetryBenchmark:
		if op == previewRetryBenchmark && p.local != nil {
			p.retryLocalBenchmark()
			return
		}
		if s.task.stage == "bind" {
			names := s.task.bundles
			if len(names) == 0 {
				names = s.checkedBundles()
			}
			p.beginBenchmark(previewBenchmark{model: s.model, engine: s.engine, bundles: names})
		} else {
			p.startService(true, s.candidate)
		}
	case previewBind:
		p.startService(true, s.candidate)
	case previewStopEngine:
		p.stopPreviewEngine(nil)
	case previewRefreshDevice:
		fail := p.takeFailure()
		s.device.Refreshing, s.device.LastError = true, nil
		p.after("device", 600*time.Millisecond, func() {
			s.device.Refreshing = false
			if fail {
				s.device.LastError = core.Err("device_preflight_blocked")
				return
			}
			s.device.Snapshot = previewBase(p.ctx).device.Snapshot
			s.device.CapturedAt = previewValue(p.timestamp())
			if s.progress != nil && s.progress.DevicePreflight.Status == "blocked" {
				s.progress.DevicePreflight.Status, s.progress.NextAction = "ok", "prepare_candidate"
			}
			p.noticeKey = "preview.notice.device_refreshed"
		})
	case previewRefreshDiscovery:
		fail := p.takeFailure()
		s.discovering = true
		p.after("discovery", 500*time.Millisecond, func() {
			s.discovering, s.discoveryLoaded = false, true
			s.discoveryError = nil
			if fail {
				s.discoveryError = core.Err("service_discovery_error")
			}
		})
	case previewReload:
		fail := p.takeFailure()
		s.loaded, s.refreshing = true, true
		p.after("reload", 400*time.Millisecond, func() {
			s.refreshing, s.refreshFailure = false, ""
			if fail {
				s.refreshFailure = l10n.TR("preview.error.refresh_failed")
				return
			}
			s.historyError, s.reportsError = nil, nil
			p.noticeKey = "preview.notice.page_refreshed"
		})
	default:
		p.showNotice()
	}
}

func (p *previewSession) prepareEnvironment() {
	s := p.state
	if s.busy {
		return
	}
	fail := p.takeFailure()
	s.busy, s.cliInstallStatus = true, l10n.TR("preview.status.preparing_env")
	p.after("environment", 600*time.Millisecond, func() {
		s.busy, s.cliInstallStatus = false, ""
		if fail {
			s.failure = core.Err("install_failed")
			return
		}
		s.ready.CLI.Status = "ok"
		s.serveSupported = true
		s.cliUpgrade = core.CLIUpgradeState{State: core.CLIUpgradeIdle}
		s.ready.Workspace.Exists, s.ready.Workspace.Initialized = true, true
		s.ready.Results.Exists, s.ready.Models.Exists = true, true
		s.progress = previewBase(p.ctx).progress
		s.progress.Workspace = s.workspace
		s.progress.NextAction, s.progress.DevicePreflight.Status = "device_check", "missing"
		s.task.stage = "prepare_candidate"
		p.checkDevice()
	})
}

func (p *previewSession) checkDevice() {
	s := p.state
	if s.busy || s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.needsTaskAssets() {
		return
	}
	fail := p.takeFailure()
	s.busy = true
	p.after("environment", 600*time.Millisecond, func() {
		s.busy = false
		if fail {
			s.failure = core.Err("device_preflight_blocked")
			s.progress.DevicePreflight.Status = "blocked"
			return
		}
		s.progress.NextAction, s.progress.DevicePreflight.Status = "prepare_candidate", "ok"
		s.progress.DevicePreflight.Path = previewValue(filepath.Join(s.workspace, "preflight"))
		p.noticeKey = "preview.notice.env_device_checked"
	})
}

func (p *previewSession) browse(key string) {
	s := p.state
	root := emptyText(s.settings.Effective.WorkspaceRoot)
	if root == "" {
		root = emptyText(s.settings.Settings.WorkspaceRoot)
	}
	var current *string
	switch key {
	case browseInstallation.key:
		current = &s.installationPath
	case browseWorkspace.key:
		current = &s.workspace
	default:
		s.failure = &core.ApiError{Code: "preview_unsupported", Message: l10n.TR("preview.notice.unsupported")}
		return
	}
	*current = browseChoice(root, key, *current)
}

func (p *previewSession) saveSettings(patch map[string]json.RawMessage) {
	s := p.state
	data, _ := json.Marshal(s.settings.Settings)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for key, value := range patch {
		fields[key] = value
	}
	data, _ = json.Marshal(fields)
	var value core.UserSettings
	_ = json.Unmarshal(data, &value)
	workspaceChanged := emptyText(value.WorkspaceRoot) != emptyText(s.settings.Settings.WorkspaceRoot)
	if workspaceChanged && (s.running() != nil || engineLocked(s.engineState.State) || s.engineState.State == "orphaned") {
		s.failure = core.Err("workspace_busy")
		return
	}
	if p.takeFailure() {
		if _, hasTheme := patch["theme"]; hasTheme {
			s.themeMode = previewThemeModeString(p.themeMode)
			s.syncNativeThemeIfNeeded()
			s.failurePage = s.selected
		}
		if _, hasLang := patch["language"]; hasLang {
			s.languageMode = previewLanguageModeString(p.languageMode)
			s.syncNativeLocaleIfNeeded()
			s.failurePage = s.selected
		}
		if _, hasTray := patch["minimize_to_tray"]; hasTray {
			s.minimizeToTray = s.settings.Settings.MinimizeToTray
			_ = s.syncMinimizeToTrayIfNeeded()
			s.failurePage = s.selected
		}
		if _, hasKeep := patch["keep_services_on_quit"]; hasKeep {
			s.keepServicesOnQuit = s.settings.Effective.KeepServicesOnQuit
			s.failurePage = s.selected
		}
		s.failure = &core.ApiError{Code: "preview_save_failed", Message: l10n.TR("preview.error.save_failed")}
		return
	}
	value.Theme = core.NormalizeTheme(value.Theme)
	value.Language = core.NormalizeLanguage(value.Language)
	var resultsRoot, modelsRoot, reportsRoot *string
	if ws := emptyText(value.WorkspaceRoot); ws != "" {
		resultsRoot = previewValue(filepath.Join(ws, "results"))
		modelsRoot = previewValue(filepath.Join(ws, "models"))
		reportsRoot = previewValue(filepath.Join(ws, "reports"))
	}
	s.settings.Settings = value
	s.settings.Effective = core.EffectiveSettings{
		WorkspaceRoot:    value.WorkspaceRoot,
		ResultsRoot:      resultsRoot,
		ModelsRoot:       modelsRoot,
		ReportsRoot:      reportsRoot,
		InferyardPath:    value.InferyardPath,
		ExtraModelsRoots: slices.Clone(value.ExtraModelsRoots),
		DownloadProxy:    value.DownloadProxy,
		Theme:              value.Theme,
		Language:           value.Language,
		MinimizeToTray:     value.MinimizeToTray,
		KeepServicesOnQuit: core.EffectiveKeepServicesOnQuitValue(value),
	}
	s.syncSavedGroups(patch, s.settingsDraft(), s.settings)
	if _, hasTheme := patch["theme"]; hasTheme {
		p.themeMode = previewThemeModeIndex(value.Theme)
		s.themeMode = value.Theme
		s.appliedThemeMode = value.Theme
		p.syncNativeTheme()
	}
	if _, hasLang := patch["language"]; hasLang {
		p.languageMode = previewLanguageModeIndex(value.Language)
		s.languageMode = value.Language
		s.appliedLanguageMode = value.Language
		p.syncNativeLanguage()
	}
	if _, hasTray := patch["minimize_to_tray"]; hasTray {
		s.minimizeToTray = value.MinimizeToTray
		_ = s.syncMinimizeToTrayIfNeeded()
	}
	if _, hasKeep := patch["keep_services_on_quit"]; hasKeep {
		s.keepServicesOnQuit = s.settings.Effective.KeepServicesOnQuit
	}
	s.ready.Workspace.Path, s.ready.Results.Path, s.ready.Models.Path = value.WorkspaceRoot, resultsRoot, modelsRoot
	if workspaceChanged {
		p.cancelEvents("environment")
		p.resetTask("", "")
		s.progress, s.task.stage = nil, ""
		s.ready.Workspace.Initialized = false
		var roots []string
		if modelsRoot != nil {
			roots = append(roots, *modelsRoot)
		}
		roots = append(roots, value.ExtraModelsRoots...)
		s.registry.ModelRoots = roots
		s.registry.Models = nil
		s.registry.Engines = nil
		s.registry.DefaultEngine = ""
		s.registry.TotalModelBytes = 0
		s.checks.Engines = nil
		s.history = core.RunHistoryResponse{
			ResultsRoot:    resultsRoot,
			Batches:        []core.BatchSummary{},
			StandaloneRuns: []core.RunSummary{},
		}
		s.reports = nil
		s.reportsTab = reportsTabList
		s.closeOpenedReport()
	}
	p.noticeKey = "preview.notice.settings_saved"
}

func (p *previewSession) previewEngineMetadata(state string, pid int64, local bool, candidate string) core.EngineState {
	s := p.state
	family := "llama-cpp"
	executable := "llama-server"
	modelKind := "gguf"
	contextSize := int64(32768)
	if e := s.localEngine(); e != nil {
		family = e.Family
		switch e.Family {
		case "polystrata":
			executable = "polystrata"
		case "mlx-lm":
			executable = "python"
			modelKind = "directory"
		case "vllm":
			executable = "vllm"
			modelKind = "directory"
		case "sglang":
			executable = "sglang"
			modelKind = "directory"
		case "lmstudio":
			executable = "lms"
		}
	}
	if s.speed != nil && s.speed.current != nil && s.speed.current.context > 0 {
		contextSize = int64(s.speed.current.context)
	}
	return core.EngineState{
		State:       state,
		Local:       local,
		Family:      family,
		Executable:  executable,
		Model:       s.model,
		ModelKind:   modelKind,
		ContextSize: contextSize,
		Pid:         pid,
		Endpoint:    "http://127.0.0.1:48857",
		Candidate:   candidate,
	}
}

func (p *previewSession) startService(bind bool, candidate string) {
	s := p.state
	if s.busy || s.running() != nil {
		return
	}
	fail := p.takeFailure()
	s.busy = bind
	s.engineState = p.previewEngineMetadata("starting", 42, false, candidate)
	p.after("service", 800*time.Millisecond, func() {
		if bind {
			s.busy = false
		}
		if fail {
			s.engineState.State, s.engineState.Error = "failed", core.Err("engine_start_failed")
			s.failure = s.engineState.Error
			return
		}
		s.engineState.State = "running"
		if bind && s.progress != nil {
			s.progress.NextAction = "run"
			for i := range s.progress.Bundles {
				b := &s.progress.Bundles[i]
				if b.Phase == "run_complete" || (b.NextAction != "bind" && b.BoundPath != nil) {
					continue
				}
				b.Phase, b.NextAction = "run_missing", "run"
				b.BoundPath = previewValue(filepath.Join(s.workspace, "bound", b.Bundle))
			}
			s.clearRecoveredBinding()
			s.failedBundle = ""
		}
	})
}

func (p *previewSession) activateScene() {
	s := p.state
	if s.detail != nil {
		p.details[s.detailID] = s.detail
	}
	if task := s.running(); task != nil {
		task.Kind, task.RunId = "run", previewValue(task.TaskId)
		detail := previewDetailFixture(task.TaskId)
		detail.Task = *task
		detail.Progress = s.runProgress
		for i := int64(0); i < detail.Progress.Done; i++ {
			detail.Progress.Cases = append(detail.Progress.Cases, previewSimulatedCase(emptyText(task.Bundle), i+1))
		}
		p.details[task.TaskId] = detail
		if s.detailID == task.TaskId {
			s.detail = detail
		}
		if run := p.historyRun(task.TaskId); run != nil {
			run.Status, run.Sealed, run.Completeness = "partial", false, nil
			p.scoreHistoryRun(task.TaskId)
		}
		s.task.bundles = []string{emptyText(task.Bundle)}
		s.busy = true
		p.tickRun(task.TaskId, 0, previewBenchmark{model: s.model, engine: s.engine, bundles: s.task.bundles}, []string{task.TaskId})
	}
	for _, task := range s.downloads {
		if task.State == "downloading" {
			if task.TaskId == "preview-polystrata-model-live" {
				p.tickPolystrataModelPreview(task.TaskId)
			} else if task.Kind == "polystrata" {
				p.tickPolystrataSetup(task.TaskId, false, 1)
			} else {
				p.tickDownload(task.TaskId, false, 0)
			}
		}
	}
	if s.updateState.State == core.UpdateDownloading {
		p.tickUpdateDownload(false, 2, false)
	}
}

func (p *previewSession) startPreviewStandalone(op previewStandaloneStart) {
	s := p.state
	fail := p.takeFailure()
	modelPath := op.model
	modelKind := "gguf"
	for _, m := range s.registry.Models {
		if m.Name == op.model {
			if m.Path != "" {
				modelPath = m.Path
			}
			if m.Format != "" {
				modelKind = m.Format
			}
			break
		}
	}
	family := "llama-cpp"
	localEngine := false
	for _, e := range s.registry.Engines {
		if e.Name == op.engine {
			if e.Family != "" {
				family = e.Family
			}
			localEngine = e.Kind == "local_installation"
			break
		}
	}
	adapter := "prism_llama_server_v1"
	if localEngine {
		adapter = "local_openai_v1"
	}
	executable := "llama-server"
	if family == "polystrata" || family == "vllm" || family == "sglang" {
		executable = "python"
	}
	serviceID := core.ServiceIDFor(op.model, op.engine)
	p.after("service-"+serviceID, 300*time.Millisecond, func() {
		s.standalone.preparing = false
		s.standalone.preparingModel = ""
		if fail {
			err := core.Err("engine_start_failed")
			s.standalone.lastError = err
			s.failure = err
			s.failurePage = s.selected
			s.engineState = core.EngineState{
				ServiceID:   serviceID,
				State:       "failed",
				Local:       localEngine,
				Origin:      core.EngineOriginStandalone,
				Family:      family,
				Adapter:     adapter,
				Executable:  executable,
				Model:       modelPath,
				ModelKind:   modelKind,
				ContextSize: op.contextSize,
				LogPath:     "/preview/logs/standalone.log",
				Candidate:   "preview-standalone",
				Error:       err,
			}
			return
		}
		port := p.previewNextAvailablePort(op.port)
		endpoint := p.previewEndpointURL(op.lanAccess, port)
		budget := s.estimateLocalMemoryBudget(op.model, op.engine, op.contextSize)
		p.clearPreviewExitNoticeFor(serviceID, op.model, op.engine)
		st := core.EngineState{
			ServiceID:         serviceID,
			State:             "starting",
			Local:             localEngine,
			Origin:            core.EngineOriginStandalone,
			Family:            family,
			Adapter:           adapter,
			Executable:        executable,
			EngineName:        op.engine,
			ModelName:         op.model,
			Model:             modelPath,
			ModelKind:         modelKind,
			Port:              port,
			RequestedPort:     op.port,
			LANAccess:         op.lanAccess,
			EstimatedMemoryGB: budget.EstimatedGB,
			ContextSize:       op.contextSize,
			GPULayers:         cloneIntPtrUI(op.gpuLayers),
			Threads:           cloneIntPtrUI(op.threads),
			ExtraArgs:         append([]string(nil), op.extraArgs...),
			StartedAt:         time.Now().UTC().Format(time.RFC3339),
			Pid:               42001 + int64(len(s.managedServices)),
			Endpoint:          endpoint,
			LogPath:           "/preview/logs/standalone.log",
			Candidate:         "preview-standalone",
		}
		p.upsertPreviewManagedService(st)
		s.markServiceExpanded(serviceID)
		p.savePreviewServicePreset(core.ServicePreset{
			Model:       op.model,
			Engine:      op.engine,
			Port:        op.port,
			LANAccess:   op.lanAccess,
			ContextSize: op.contextSize,
			GPULayers:   cloneIntPtrUI(op.gpuLayers),
			Threads:     cloneIntPtrUI(op.threads),
			ExtraArgs:   append([]string(nil), op.extraArgs...),
		})
		p.after("service-ready-"+serviceID, 500*time.Millisecond, func() {
			for i := range s.managedServices {
				if s.managedServices[i].ServiceID == serviceID {
					s.managedServices[i].State = "running"
				}
			}
			if s.engineState.ServiceID == serviceID {
				s.engineState.State = "running"
			}
		})
	})
}

var _ previewMode = (*previewSession)(nil)
