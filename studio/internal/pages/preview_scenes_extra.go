//go:build ui_debug && !release

package pages

import (
	"encoding/json"
	"strings"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func previewCLIOutdatedScene(s *viewState) {
	s.selected = "benchmarks"
	s.benchFlowOpen = true
	s.ready.CLI = core.InferyardProbe{
		Status:  "outdated",
		Version: previewValue("0.0.12"),
		Source:  "path",
	}
	s.serveSupported = false
	s.ready.Workspace.Exists, s.ready.Workspace.Initialized = true, true
	s.ready.Results.Exists, s.ready.Models.Exists = true, true
	s.cliUpgrade = core.CLIUpgradeState{State: core.CLIUpgradeIdle}
}

func previewLibraryEdgeStatesScene(s *viewState) {
	s.selected = "library"
	s.libraryTab = 0
	s.downloads = nil
	extraRoot := "/preview/extra-models"
	s.registry.ModelRoots = append(s.registry.ModelRoots, extraRoot)
	s.settings.Settings.ExtraModelsRoots = []string{extraRoot}
	s.settings.Effective.ExtraModelsRoots = []string{extraRoot}
	s.registry.Models = append(s.registry.Models, core.ModelAsset{
		Name:         "DeepSeek-R1-Distill-32B-Q4_K_M.gguf",
		Format:       "gguf",
		Architecture: "qwen2",
		Available:    false,
		SizeBytes:    previewValue(int64(19 << 30)),
		Path:         "/Volumes/ExternalSSD/models/DeepSeek-R1-Distill-32B-Q4_K_M.gguf",
		Deletable:    false,
	})
	brokenName := l10n.TRf("preview.engine.local_name", "family", "vllm")
	s.registry.Engines = append(s.registry.Engines, core.EngineAsset{
		Name:   brokenName,
		Family: "vllm",
		Kind:   "local_installation",
		Source: l10n.TR("preview.source.local_manifest"),
		Path:   "/preview/vllm.json",
	})
	s.settings.Settings.EngineInstallations = append(s.settings.Settings.EngineInstallations, "/preview/vllm.json")
	if s.checks.Engines == nil {
		s.checks.Engines = map[string]core.EngineEligibility{}
	}
	s.checks.Engines[brokenName] = core.EngineEligibility{
		State:             core.EngineCheckIneligible,
		Family:            "vllm",
		VerificationScope: "installation",
		Reason:            "local_engine_python_venv_required",
	}
}

func seedPreviewSpeedHistory(s *viewState) {
	if s.speed == nil {
		s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	}
	doneTask := &speedTask{
		id:              "preview-speed-64k",
		model:           s.model,
		engine:          s.engine,
		context:         inferyard.ContextSpeed64K,
		stage:           speedDone,
		completed:       5,
		startedAt:       "2026-10-16T23:40:00Z",
		selectionModel:  s.model,
		selectionEngine: s.engine,
	}
	for i := range 5 {
		doneTask.samples = append(doneTask.samples, previewSpeedSample(inferyard.ContextSpeed64K, i))
	}
	applySpeedResult(doneTask, previewSpeedResult(doneTask, 5, false))

	missingTask := &speedTask{
		id:              "preview-speed-128k-gap",
		model:           s.model,
		engine:          s.engine,
		context:         inferyard.ContextSpeed128K,
		stage:           speedDone,
		completed:       5,
		startedAt:       "2026-10-16T23:48:00Z",
		selectionModel:  s.model,
		selectionEngine: s.engine,
	}
	for i := range 5 {
		sample := previewSpeedSample(inferyard.ContextSpeed128K, i)
		if i == 4 {
			sample.seconds, sample.prefill, sample.decode = nil, nil, nil
			sample.reason = l10n.TR("preview.speed.missing_timing_reason")
		}
		missingTask.samples = append(missingTask.samples, sample)
	}
	applySpeedResult(missingTask, previewSpeedResult(missingTask, 5, true))

	cancelledTask := &speedTask{
		id:              "preview-speed-256k-cancelled",
		model:           "Preview-Small.gguf",
		engine:          s.engine,
		context:         inferyard.ContextSpeed256K,
		stage:           speedCancelled,
		completed:       2,
		reason:          l10n.TR("preview.speed.cancelled_reason"),
		startedAt:       "2026-10-16T23:52:00Z",
		selectionModel:  "Preview-Small.gguf",
		selectionEngine: s.engine,
	}
	for i := range 2 {
		cancelledTask.samples = append(cancelledTask.samples, previewSpeedSample(inferyard.ContextSpeed256K, i))
	}
	applySpeedResult(cancelledTask, previewSpeedResult(cancelledTask, 2, false))

	s.speed.history = []*speedTask{doneTask, missingTask, cancelledTask}
}

func previewSpeedHistoryScene(s *viewState) {
	previewCompleted(s)
	s.selected = "reports"
	s.reportsTab = reportsTabSpeed
}

func previewOrphanedServiceScene(s *viewState) {
	s.selected = "services"
	orphanErr := &core.ApiError{
		Code:    "service_identity_unreadable",
		Message: l10n.TR("engine.notice.orphan_unconfirmed"),
	}
	orphan := core.EngineState{
		ServiceID:         core.ServiceIDFor(s.model, s.engine),
		State:             "orphaned",
		Origin:            core.EngineOriginStandalone,
		Family:            "llama-cpp",
		Adapter:           "prism_llama_server_v1",
		Executable:        "llama-server",
		EngineName:        s.engine,
		ModelName:         s.model,
		Model:             s.model,
		ModelKind:         "gguf",
		Port:              8080,
		RequestedPort:     8080,
		EstimatedMemoryGB: 5.9,
		ContextSize:       32768,
		StartedAt:         "2026-11-25T03:20:00Z",
		Pid:               41990,
		Endpoint:          "http://127.0.0.1:8080",
		LogPath:           "/preview/logs/orphan-service.log",
		Error:             orphanErr,
	}
	s.managedServices = []core.EngineState{orphan}
	s.engineState = orphan
	s.memorySummary = s.currentMemorySummary()
}

func previewNotificationsScene(s *viewState) {
	previewCompleted(s)
	s.selected = "benchmarks"
	nc := s.notifications()
	nc.items = nil
	nc.nextID = 0
	baseTime := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	hold := 10 * time.Minute

	s.pushNotification(studioNotification{
		Key:              "dl:preview-polystrata-blocked:failed",
		Title:            l10n.TR("notif.download.polystrata.failed.title"),
		Body:             l10n.TRf("notif.download.polystrata.failed.body", "label", "PolyStrata · coder · IQ1_M"),
		Kind:             stateDanger,
		CreatedAt:        baseTime.Add(-4 * time.Minute),
		AutoDismissAfter: hold,
		NeedsDecision:    true,
		Primary: &notificationAction{
			Label: l10n.TR("notif.action.view_and_retry"),
			OnClick: func() {
				s.libraryTab = 1
				s.navigateTo("library", "")
			},
		},
	})
	s.pushNotification(studioNotification{
		Key:              "engine-updates:preview",
		Title:            l10n.TR("notif.engine_updates.available.title"),
		Body:             l10n.TRf("notif.engine_updates.available.body", "count", 1, "engines", "llama.cpp (b11146)"),
		Kind:             stateInfo,
		CreatedAt:        baseTime.Add(-2 * time.Minute),
		AutoDismissAfter: hold,
		Primary: &notificationAction{
			Label: l10n.TR("notif.action.view_engines"),
			OnClick: func() {
				s.libraryTab = 1
				s.navigateTo("library", "")
			},
		},
	})
	s.pushNotification(studioNotification{
		Key:              "report:preview-report",
		Title:            l10n.TR("notif.report_created.title"),
		Body:             l10n.TRf("notif.report_created.body_id", "id", "preview-report"),
		Kind:             stateSuccess,
		CreatedAt:        baseTime.Add(-30 * time.Second),
		AutoDismissAfter: hold,
		Primary: &notificationAction{
			Label:   l10n.TR("notif.action.view_report"),
			OnClick: func() { s.openReport("preview-report") },
		},
	})
	// Seed one older center-only item so the bell drawer shows history beyond the 3-card screen stack.
	s.pushNotification(studioNotification{
		Key:       "run:preview-run:done",
		Title:     l10n.TRf("notif.run.done.title", "name", l10n.TRf("notif.run.name_bundle", "bundle", inferyard.DefaultBundleCore)),
		Body:      l10n.TR("notif.run.done.body"),
		Kind:      stateSuccess,
		CreatedAt: baseTime.Add(-8 * time.Minute),
		Primary: &notificationAction{
			Label: l10n.TR("notif.action.view_details"),
			OnClick: func() {
				s.navigateTo("benchmarks", "")
				s.selectTask("preview-run")
			},
		},
	})
	if len(nc.items) > 0 {
		nc.items[0].VisibleOnScreen = false
		// Reorder so the 3 screen-visible items sit at the front in chronological order.
		oldestCenter := nc.items[0]
		nc.items = append(nc.items[1:], oldestCenter)
	}
}

func (p *previewSession) recordPreviewEvent(eventType string, payload any) {
	if p == nil || p.state == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	p.state.ingestCoreEvent(core.Event{Type: eventType, Data: raw})
	// Keep automated preview event notifications in the bell center (unread badge)
	// without covering header popovers during fast simulated transitions.
	if nc := p.state.notifs; nc != nil && len(nc.items) > 0 {
		nc.items[0].VisibleOnScreen = false
	}
}

func previewKnownSourceKey(val string) string {
	for _, key := range []string{
		"preview.source.engine_dir",
		"preview.source.local_manifest",
		"preview.source.guided_install",
		"preview.source.download",
	} {
		if val == l10n.T(l10n.LanguageZhHans, key) || val == l10n.T(l10n.LanguageEn, key) {
			return key
		}
	}
	return ""
}

func previewKnownStageKey(val string) string {
	for _, key := range []string{
		"preview.polystrata.stage.preflight",
		"preview.polystrata.stage.build_metal",
		"preview.polystrata.stage.build_metal_inference",
		"preview.polystrata.stage.download_model",
	} {
		if val == l10n.T(l10n.LanguageZhHans, key) || val == l10n.T(l10n.LanguageEn, key) {
			return key
		}
	}
	return ""
}

func (p *previewSession) reapplyPreviewLocale(locale string) {
	if p == nil || p.state == nil || locale == "" {
		return
	}
	s := p.state
	s.windowTitle = l10n.T(locale, "app.title.preview")
	engineRenames := map[string]string{}
	for i := range s.registry.Engines {
		eng := &s.registry.Engines[i]
		if srcKey := previewKnownSourceKey(eng.Source); srcKey != "" {
			eng.Source = l10n.T(locale, srcKey)
		}
		if eng.Family != "" {
			zhLocal := l10n.Tf(l10n.LanguageZhHans, "preview.engine.local_name", "family", eng.Family)
			enLocal := l10n.Tf(l10n.LanguageEn, "preview.engine.local_name", "family", eng.Family)
			if eng.Name == zhLocal || eng.Name == enLocal {
				nextName := l10n.Tf(locale, "preview.engine.local_name", "family", eng.Family)
				if nextName != eng.Name {
					engineRenames[eng.Name] = nextName
					eng.Name = nextName
				}
			}
		}
	}
	if len(engineRenames) > 0 {
		if next, ok := engineRenames[s.engine]; ok {
			s.engine = next
		}
		if next, ok := engineRenames[s.registry.DefaultEngine]; ok {
			s.registry.DefaultEngine = next
		}
		for oldName, nextName := range engineRenames {
			if elig, ok := s.checks.Engines[oldName]; ok {
				delete(s.checks.Engines, oldName)
				s.checks.Engines[nextName] = elig
			}
		}
	}

	for i := range s.downloads {
		dl := &s.downloads[i]
		if dl.Detail != nil {
			if stageKey := previewKnownStageKey(*dl.Detail); stageKey != "" {
				*dl.Detail = l10n.T(locale, stageKey)
			}
		}
		if dl.Error != nil && dl.Error.Details != nil {
			if blockers, ok := dl.Error.Details["blockers"].([]map[string]any); ok {
				for j := range blockers {
					switch blockers[j]["code"] {
					case "polystrata_build_tools_missing", "cmake_missing":
						blockers[j]["message"] = l10n.T(locale, "preview.polystrata.blocker.build_tools")
					case "polystrata_xcode_clt_missing", "xcode_clt_missing":
						blockers[j]["message"] = l10n.T(locale, "preview.polystrata.blocker.xcode_clt")
					}
				}
			}
		}
	}

	if s.updateState.State == core.UpdateError {
		switch s.updateState.ErrorCode {
		case "update_path_unwritable":
			s.updateState.ErrorMessage = l10n.T(locale, "preview.update.error.app_dir_unwritable")
		case "update_checksum_mismatch":
			s.updateState.ErrorMessage = l10n.T(locale, "preview.update.error.checksum_mismatch")
		case "update_network_error":
			s.updateState.ErrorMessage = l10n.T(locale, "preview.update.error.network")
		}
	}

	if s.engineState.State == "orphaned" && s.engineState.Error != nil {
		s.engineState.Error.Message = l10n.T(locale, "engine.notice.orphan_unconfirmed")
		for i := range s.managedServices {
			if s.managedServices[i].State == "orphaned" && s.managedServices[i].Error != nil {
				s.managedServices[i].Error.Message = s.engineState.Error.Message
			}
		}
	}

	if s.speed != nil {
		for _, item := range s.speed.history {
			if item.stage == speedCancelled {
				item.reason = l10n.T(locale, "preview.speed.cancelled_reason")
			}
			for j := range item.samples {
				if item.samples[j].reason != "" {
					item.samples[j].reason = l10n.T(locale, "preview.speed.missing_timing_reason")
				}
			}
		}
	}

	if s.openedReport != nil && s.openedReport.index != nil {
		for i := range s.openedReport.index.Runs {
			localizePreviewReportRun(&s.openedReport.index.Runs[i], locale)
		}
	}
	for _, idx := range p.indexes {
		if idx == nil {
			continue
		}
		for i := range idx.Runs {
			localizePreviewReportRun(&idx.Runs[i], locale)
		}
	}
	for _, detail := range p.details {
		if detail == nil || detail.Progress == nil {
			continue
		}
		for i := range detail.Progress.Cases {
			c := &detail.Progress.Cases[i]
			if c.Category != nil && (*c.Category == l10n.T(l10n.LanguageZhHans, "preview.fixture.category") || *c.Category == l10n.T(l10n.LanguageEn, "preview.fixture.category")) {
				*c.Category = l10n.T(locale, "preview.fixture.category")
			}
		}
	}

	if p.scene >= 0 && p.scene < len(previewScenes) && strings.HasSuffix(previewScenes[p.scene].labelKey, "notifications_center") && s.notifs != nil {
		wasBellOpen := s.notifs.bellOpen
		previewNotificationsScene(s)
		s.notifs.bellOpen = wasBellOpen
	}
}
