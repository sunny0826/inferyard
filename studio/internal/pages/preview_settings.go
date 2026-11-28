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

func (p *previewSession) prepareEnvironment() {
	s := p.state
	if s.busy {
		return
	}
	fail := p.takeFailure()
	wasOutdated := s.ready.CLI.Status == "outdated"
	s.busy, s.cliInstallStatus = true, l10n.TR("preview.status.preparing_env")
	if wasOutdated {
		s.busyAction = "upgrade-cli"
	} else {
		s.busyAction = "prepare-env"
	}
	p.after("environment", 600*time.Millisecond, func() {
		s.busy, s.busyAction, s.cliInstallStatus = false, "", ""
		if fail {
			s.failure = core.Err("install_failed")
			if wasOutdated {
				s.cliUpgrade = core.CLIUpgradeState{
					Seq:           s.cliUpgrade.Seq + 1,
					State:         core.CLIUpgradeFailed,
					TargetVersion: previewAppVersion,
					Error:         s.failure,
				}
			}
			return
		}
		s.ready.CLI.Status = "ok"
		s.ready.CLI.Version = previewValue(previewAppVersion)
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
		if _, hasAccent := patch["accent"]; hasAccent {
			s.accentMode = core.NormalizeAccent(p.accentMode)
			setActiveAccent(s.accentMode)
			s.failurePage = s.selected
		}
		if _, hasFontSize := patch["font_size"]; hasFontSize {
			s.fontSizeMode = core.NormalizeFontSize(p.fontSizeMode)
			setActiveFontSize(s.fontSizeMode)
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
		if _, hasSource := patch["download_source"]; hasSource {
			s.downloadSource = downloadSourceFromSettings(s.settings)
			if s.downloadsUI != nil && !s.downloadsUI.sourceCustomized {
				s.downloadsUI.applyDefaultModelSource(s.downloadSource)
			}
			s.failurePage = s.selected
		}
		s.failure = &core.ApiError{Code: "preview_save_failed", Message: l10n.TR("preview.error.save_failed")}
		return
	}
	value.DownloadSource = core.NormalizeDownloadSource(value.DownloadSource)
	value.Theme = core.NormalizeTheme(value.Theme)
	value.Accent = core.NormalizeAccent(value.Accent)
	value.FontSize = core.NormalizeFontSize(value.FontSize)
	value.Language = core.NormalizeLanguage(value.Language)
	value.ReasoningEffort = core.NormalizeReasoningEffort(value.ReasoningEffort)
	value.ReasoningMode = core.NormalizeReasoningMode(value.ReasoningMode)
	var resultsRoot, modelsRoot, reportsRoot *string
	if ws := emptyText(value.WorkspaceRoot); ws != "" {
		resultsRoot = previewValue(filepath.Join(ws, "results"))
		modelsRoot = previewValue(filepath.Join(ws, "models"))
		reportsRoot = previewValue(filepath.Join(ws, "reports"))
	}
	s.settings.Settings = value
	s.settings.Effective = core.EffectiveSettings{
		WorkspaceRoot:      value.WorkspaceRoot,
		ResultsRoot:        resultsRoot,
		ModelsRoot:         modelsRoot,
		ReportsRoot:        reportsRoot,
		InferyardPath:      value.InferyardPath,
		ExtraModelsRoots:   slices.Clone(value.ExtraModelsRoots),
		DownloadSource:     value.DownloadSource,
		DownloadProxy:      value.DownloadProxy,
		Theme:              value.Theme,
		Accent:             value.Accent,
		FontSize:           value.FontSize,
		Language:           value.Language,
		ReasoningEffort:    value.ReasoningEffort,
		ReasoningMode:      value.ReasoningMode,
		MinimizeToTray:     value.MinimizeToTray,
		KeepServicesOnQuit: core.EffectiveKeepServicesOnQuitValue(value),
	}
	s.syncSavedGroups(patch, s.settingsDraft(), s.settings)
	if _, hasSource := patch["download_source"]; hasSource {
		s.downloadSource = value.DownloadSource
		if s.downloadsUI != nil {
			s.downloadsUI.applyDefaultModelSource(value.DownloadSource)
		}
	}
	if _, hasReasoning := patch["reasoning_effort"]; hasReasoning {
		s.reasoningEffort = value.ReasoningEffort
	}
	if _, hasReasoningMode := patch["reasoning_mode"]; hasReasoningMode {
		s.reasoningMode = value.ReasoningMode
	}
	if _, hasTheme := patch["theme"]; hasTheme {
		p.themeMode = previewThemeModeIndex(value.Theme)
		s.themeMode = value.Theme
		s.appliedThemeMode = value.Theme
		p.syncNativeTheme()
	}
	if _, hasAccent := patch["accent"]; hasAccent {
		p.accentMode = value.Accent
		s.accentMode = value.Accent
		setActiveAccent(value.Accent)
	}
	if _, hasFontSize := patch["font_size"]; hasFontSize {
		p.fontSizeMode = value.FontSize
		s.fontSizeMode = value.FontSize
		setActiveFontSize(value.FontSize)
	}
	if _, hasLang := patch["language"]; hasLang {
		p.languageMode = previewLanguageModeIndex(value.Language)
		s.languageMode = value.Language
		s.appliedLanguageMode = value.Language
		p.syncNativeLanguage()
		p.reapplyPreviewLocale(l10n.LocaleForSetting(value.Language))
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
		s.nodes.unifiedReports, s.nodes.reportsLoaded = nil, false
		s.resetReportCompare()
		s.reportsTab = reportsTabList
		s.closeOpenedReport()
	}
	p.noticeKey = "preview.notice.settings_saved"
}

func (p *previewSession) cleanWorkspaceArtifacts() {
	s := p.state
	s.clearCleanedWorkspaceState()
	s.enqueueStatusToast(l10n.TR("settings.workspace.cleanup.done"))
}
