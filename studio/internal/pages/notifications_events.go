package pages

import (
	"encoding/json"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) ingestCoreEvents(events []core.Event) {
	syncRuns, syncDownloads, syncChecks := false, false, false
	for _, event := range events {
		switch event.Type {
		case "run.progress", "run.resources", "run.state":
			syncRuns = true
		case "download.progress", "download.state":
			syncDownloads = true
		case "engine.check":
			syncChecks = true
		}
		s.ingestCoreEvent(event)
	}
	if syncRuns {
		s.syncLiveRunState()
	}
	if syncDownloads && s.preview == nil && s.service != nil {
		s.downloads = s.service.Downloads()
	}
	if syncChecks && s.preview == nil && s.service != nil {
		s.checks = s.service.EngineChecks()
		s.adoptDownloadedEngine()
		s.adoptOnlyModel()
	}
}

func (s *viewState) syncLiveRunState() {
	if s.preview != nil || s.service == nil {
		return
	}
	tasks := s.service.RunTasks()
	s.tasks = tasks
	s.reconcileLocalPIDs(tasks)
	if active := activeRunTask(tasks); active != nil {
		if d, e := s.service.RunDetail(active.TaskId); e == nil {
			s.runProgress = d.Progress
			if s.detailID == active.TaskId {
				s.detail, s.detailError = &d, nil
				return
			}
		}
	} else {
		s.runProgress = nil
	}
	if s.detailID != "" {
		if d, e := s.service.RunDetail(s.detailID); e == nil {
			s.detail, s.detailError = &d, nil
		} else {
			s.detailError = e
		}
	}
}

func (s *viewState) ingestCoreEvent(event core.Event) {
	switch event.Type {
	case "services.state", "services.memory":
		var payload struct {
			Services []core.EngineState `json:"services"`
		}
		if json.Unmarshal(event.Data, &payload) == nil {
			s.applyServicesStateEvent(payload.Services)
		}
	case "service.exited":
		var payload struct {
			Notice *core.ServiceExitNotice `json:"notice"`
		}
		if json.Unmarshal(event.Data, &payload) == nil && payload.Notice != nil {
			s.appendExitedNotice(*payload.Notice)
		}
	case "service.exit_notice_cleared":
		var payload struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(event.Data, &payload) == nil && strings.TrimSpace(payload.ID) != "" {
			s.removeExitedNotice(strings.TrimSpace(payload.ID))
		} else {
			s.syncExitedNotices(nil)
		}
	case "report.created":
		var payload struct {
			Report struct {
				ReportId string `json:"report_id"`
			} `json:"report"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		reportID := strings.TrimSpace(payload.Report.ReportId)
		if reportID != "" && s.lastManualReportID == reportID {
			s.lastManualReportID = ""
			return
		}
		body := l10n.TR("notif.report_created.body")
		if reportID != "" {
			body = l10n.TRf("notif.report_created.body_id", "id", reportID)
		}
		s.pushNotification(studioNotification{
			Key:   "report:" + reportID,
			Title: l10n.TR("notif.report_created.title"),
			Body:  body,
			Kind:  stateSuccess,
			Primary: &notificationAction{
				Label: l10n.TR("notif.action.view_report"),
				OnClick: func() {
					if reportID != "" {
						s.openReport(reportID)
					} else {
						s.navigateTo("reports", "")
					}
				},
			},
		})
	case "run.state":
		var payload struct {
			Task struct {
				TaskId string  `json:"task_id"`
				Kind   string  `json:"kind"`
				State  string  `json:"state"`
				Bundle *string `json:"bundle"`
			} `json:"task"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		task := payload.Task
		if !terminal(task.State) {
			return
		}
		name := l10n.TR("notif.run.name_default")
		if task.Kind == "context_speed" {
			name = l10n.TR("notif.run.name_context_speed")
		} else if task.Bundle != nil && strings.TrimSpace(*task.Bundle) != "" {
			name = l10n.TRf("notif.run.name_bundle", "bundle", *task.Bundle)
		}
		switch task.State {
		case "done":
			s.pushNotification(studioNotification{
				Key:   "run:" + task.TaskId + ":done",
				Title: l10n.TRf("notif.run.done.title", "name", name),
				Body:  l10n.TR("notif.run.done.body"),
				Kind:  stateSuccess,
				Primary: &notificationAction{
					Label: l10n.TR("notif.action.view_details"),
					OnClick: func() {
						if task.Kind == "context_speed" {
							s.openSpeed(task.TaskId)
						} else {
							s.navigateTo("benchmarks", "")
							s.selectTask(task.TaskId)
						}
					},
				},
			})
		case "failed":
			s.pushNotification(studioNotification{
				Key:           "run:" + task.TaskId + ":failed",
				Title:         l10n.TRf("notif.run.failed.title", "name", name),
				Body:          l10n.TR("notif.run.failed.body"),
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label: l10n.TR("notif.action.troubleshoot"),
					OnClick: func() {
						s.navigateTo("benchmarks", "")
						if task.Kind != "context_speed" {
							s.selectTask(task.TaskId)
						}
					},
				},
			})
		case "cancelled":
			s.pushNotification(studioNotification{
				Key:   "run:" + task.TaskId + ":cancelled",
				Title: l10n.TRf("notif.run.cancelled.title", "name", name),
				Body:  l10n.TR("notif.run.cancelled.body"),
				Kind:  stateInfo,
			})
		}
	case "download.state":
		var payload struct {
			Task core.DownloadTask `json:"task"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		task := payload.Task
		if task.Kind == "engine_update" {
			target := strings.TrimSpace(task.DisplayName)
			if target == "" {
				target = strings.TrimSpace(task.OutName)
			}
			switch task.State {
			case "done":
				s.pushNotification(studioNotification{
					Key:   "dl:" + task.TaskId + ":done",
					Title: l10n.TR("notif.engine_update.done.title"),
					Body:  l10n.TRf("notif.engine_update.done.body", "target", target),
					Kind:  stateSuccess,
					Primary: &notificationAction{
						Label: l10n.TR("notif.action.view_engines"),
						OnClick: func() {
							s.libraryTab = 1
							s.navigateTo("library", "")
						},
					},
				})
			case "failed":
				s.pushNotification(studioNotification{
					Key:           "dl:" + task.TaskId + ":failed",
					Title:         l10n.TR("notif.engine_update.failed.title"),
					Body:          l10n.TRf("notif.engine_update.failed.body", "target", target),
					Kind:          stateDanger,
					NeedsDecision: true,
					Primary: &notificationAction{
						Label: l10n.TR("notif.action.view_engines"),
						OnClick: func() {
							s.libraryTab = 1
							s.navigateTo("library", "")
						},
					},
				})
			}
			return
		}
		if task.Kind == "polystrata" {
			label := strings.TrimSpace(task.DisplayName)
			if label == "" {
				label = "PolyStrata"
			}
			switch task.State {
			case "done":
				s.pushNotification(studioNotification{
					Key:   "dl:" + task.TaskId + ":done",
					Title: l10n.TR("notif.download.polystrata.done.title"),
					Body:  l10n.TRf("notif.download.polystrata.done.body", "label", label),
					Kind:  stateSuccess,
					Primary: &notificationAction{
						Label:   l10n.TR("notif.action.go_benchmark"),
						OnClick: func() { s.startBenchmarkFromPolystrataTask(task) },
					},
				})
			case "failed":
				s.pushNotification(studioNotification{
					Key:           "dl:" + task.TaskId + ":failed",
					Title:         l10n.TR("notif.download.polystrata.failed.title"),
					Body:          l10n.TRf("notif.download.polystrata.failed.body", "label", label),
					Kind:          stateDanger,
					NeedsDecision: true,
					Primary: &notificationAction{
						Label:   l10n.TR("notif.action.view_and_retry"),
						OnClick: func() { s.navigateTo("library", "") },
					},
				})
			}
			return
		}
		kind := l10n.TR("notif.download.kind_model")
		if task.Kind == "engine" {
			kind = l10n.TR("notif.download.kind_engine")
		}
		target := kind
		if strings.TrimSpace(task.OutName) != "" {
			target = l10n.TRf("notif.download.target", "kind", kind, "name", task.OutName)
		}
		switch task.State {
		case "done":
			s.pushNotification(studioNotification{
				Key:   "dl:" + task.TaskId + ":done",
				Title: l10n.TRf("notif.download.done.title", "kind", kind),
				Body:  l10n.TRf("notif.download.done.body", "target", target),
				Kind:  stateSuccess,
				Primary: &notificationAction{
					Label:   l10n.TR("notif.action.go_downloads"),
					OnClick: func() { s.navigateTo("library", "") },
				},
			})
		case "failed":
			s.pushNotification(studioNotification{
				Key:           "dl:" + task.TaskId + ":failed",
				Title:         l10n.TRf("notif.download.failed.title", "kind", kind),
				Body:          l10n.TRf("notif.download.failed.body", "target", target),
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   l10n.TR("notif.action.view_and_retry"),
					OnClick: func() { s.navigateTo("library", "") },
				},
			})
		}
	case "engine.state":
		var payload struct {
			Engine struct {
				State  string `json:"state"`
				Origin string `json:"origin"`
			} `json:"engine"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		switch payload.Engine.State {
		case "running":
			if s.engineStartToastID != "" {
				s.resolveProgressToast(s.engineStartToastID, l10n.TR("notif.engine.ready"), "", nil)
				s.engineStartToastID = ""
			}
		case "failed":
			if s.engineStartToastID != "" {
				s.dismissToast(s.engineStartToastID)
				s.engineStartToastID = ""
			}
			primaryLabel := l10n.TR("sidebar.status.open_services")
			primaryTarget := "services"
			if payload.Engine.Origin == core.EngineOriginStandalone {
				primaryLabel = l10n.TR("models.serve.action.go_models")
				primaryTarget = "library"
			}
			s.pushNotification(studioNotification{
				Key:           "engine:failed",
				Title:         l10n.TR("notif.engine.failed.title"),
				Body:          l10n.TR("notif.engine.failed.body"),
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label: primaryLabel,
					OnClick: func() {
						if primaryTarget == "library" {
							s.libraryTab = 0
						}
						s.navigateTo(primaryTarget, "")
					},
				},
			})
		case "orphaned":
			s.pushNotification(studioNotification{
				Key:           "engine:orphaned",
				Title:         l10n.TR("notif.engine.orphaned.title"),
				Body:          l10n.TR("notif.engine.orphaned.body"),
				Kind:          stateWarning,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   l10n.TR("notif.action.resolve"),
					OnClick: func() { s.navigateTo("services", "") },
				},
			})
		}
	case "update.state":
		var payload struct {
			Update core.UpdateState `json:"update"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		s.applyUpdateState(payload.Update)
	case "cli.upgrade":
		var payload struct {
			CLIUpgrade core.CLIUpgradeState `json:"cli_upgrade"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		s.applyCLIUpgradeState(payload.CLIUpgrade)
	case "engine.updates":
		var payload struct {
			Status core.EngineUpdatesStatus `json:"status"`
			Manual bool                     `json:"manual"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		s.applyEngineUpdatesStatus(payload.Status, payload.Manual)
	case "engine.check":
		var payload struct {
			Engine      string                 `json:"engine"`
			Eligibility core.EngineEligibility `json:"eligibility"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		if payload.Engine != "" && payload.Engine == s.engine && payload.Eligibility.State == core.EngineCheckIneligible {
			s.pushNotification(studioNotification{
				Key:           "engine-check:" + payload.Engine,
				Title:         l10n.TR("notif.engine_check.ineligible.title"),
				Body:          l10n.TRf("notif.engine_check.ineligible.body", "engine", payload.Engine, "reason", human(payload.Eligibility.Reason)),
				Kind:          stateWarning,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label: l10n.TR("notif.action.view_engines"),
					OnClick: func() {
						s.libraryTab = 1
						s.navigateTo("library", "")
					},
				},
			})
		}
	}
}
