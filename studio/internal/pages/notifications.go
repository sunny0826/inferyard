package pages

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

const (
	maxVisibleScreenNotifications       = 3
	defaultNotificationScreenDuration   = 6 * time.Second
	attentionNotificationScreenDuration = 8 * time.Second
	pausedNotificationMinRemaining      = 500 * time.Millisecond
)

type notificationAction struct {
	Label   string
	OnClick func()
}

type studioNotification struct {
	ID               string
	Key              string
	Title            string
	Body             string
	Kind             stateKind
	CreatedAt        time.Time
	AutoDismissAfter time.Duration
	NeedsDecision    bool
	VisibleOnScreen  bool
	Handled          bool
	Read             bool
	Announced        bool
	Secondary        *notificationAction
	Primary          *notificationAction
	screenElapsed    time.Duration
	screenLastTick   time.Time
}

func (n *studioNotification) screenDuration() time.Duration {
	if n.AutoDismissAfter > 0 {
		return n.AutoDismissAfter
	}
	if n.NeedsDecision || n.Kind == stateDanger || n.Kind == stateWarning {
		return attentionNotificationScreenDuration
	}
	return defaultNotificationScreenDuration
}

type notificationCenter struct {
	items           []*studioNotification
	nextID          uint64
	tappedExpanded  bool
	screenPaused    bool
	bellOpen        bool
	lastWindowTitle string
}

func (s *viewState) notifications() *notificationCenter {
	if s.notifs == nil {
		s.notifs = &notificationCenter{}
	}
	return s.notifs
}

func (s *viewState) pushNotification(n studioNotification) {
	nc := s.notifications()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if n.Key != "" {
		for _, existing := range nc.items {
			if existing.Key == n.Key && !existing.Handled {
				existing.Title = n.Title
				existing.Body = n.Body
				existing.Kind = n.Kind
				existing.CreatedAt = n.CreatedAt
				existing.AutoDismissAfter = n.AutoDismissAfter
				existing.NeedsDecision = n.NeedsDecision
				existing.Primary = n.Primary
				existing.Secondary = n.Secondary
				existing.VisibleOnScreen = true
				existing.Read = false
				existing.Announced = false
				existing.screenElapsed = 0
				existing.screenLastTick = time.Time{}
				s.syncWindowTitle()
				return
			}
		}
	}
	nc.nextID++
	if n.ID == "" {
		n.ID = fmt.Sprintf("notif-%d", nc.nextID)
	}
	n.VisibleOnScreen = true
	n.screenElapsed = 0
	n.screenLastTick = time.Time{}
	nc.items = append([]*studioNotification{&n}, nc.items...)
	if len(nc.items) > 40 {
		nc.items = nc.items[:40]
	}
	s.syncWindowTitle()
}

func (s *viewState) dismissNotificationFromScreen(id string) {
	nc := s.notifications()
	for _, item := range nc.items {
		if item.ID == id {
			item.VisibleOnScreen = false
			item.screenLastTick = time.Time{}
			if !item.NeedsDecision {
				item.Read = true
				item.Handled = true
			}
			break
		}
	}
	s.syncWindowTitle()
}

func (s *viewState) handleNotification(id string) {
	nc := s.notifications()
	for _, item := range nc.items {
		if item.ID == id {
			item.VisibleOnScreen = false
			item.screenLastTick = time.Time{}
			item.Read = true
			item.Handled = true
			break
		}
	}
	s.syncWindowTitle()
}

func (s *viewState) clearScreenNotifications() {
	nc := s.notifications()
	for _, item := range nc.items {
		if item.VisibleOnScreen {
			item.VisibleOnScreen = false
			item.screenLastTick = time.Time{}
			if !item.NeedsDecision {
				item.Read = true
				item.Handled = true
			}
		}
	}
	nc.tappedExpanded = false
	nc.screenPaused = false
	s.syncWindowTitle()
}

func (s *viewState) markAllNotificationsRead() {
	nc := s.notifications()
	for _, item := range nc.items {
		item.Read = true
		if !item.NeedsDecision {
			item.Handled = true
		}
		item.VisibleOnScreen = false
		item.screenLastTick = time.Time{}
	}
	nc.tappedExpanded = false
	nc.screenPaused = false
	s.syncWindowTitle()
}

func (nc *notificationCenter) advanceScreenTimers(now time.Time, maxVisible int, paused bool) time.Duration {
	if maxVisible <= 0 {
		maxVisible = maxVisibleScreenNotifications
	}
	screen := nc.screenItems()
	if len(screen) == 0 {
		nc.tappedExpanded = false
		nc.screenPaused = false
		return 0
	}
	visible := screen
	var hidden []*studioNotification
	if len(visible) > maxVisible {
		hidden = visible[maxVisible:]
		visible = visible[:maxVisible]
	}
	for _, item := range hidden {
		if !nc.screenPaused && !item.screenLastTick.IsZero() {
			if delta := now.Sub(item.screenLastTick); delta > 0 {
				item.screenElapsed += delta
			}
		}
		item.screenLastTick = time.Time{}
		if item.screenElapsed >= item.screenDuration() {
			item.VisibleOnScreen = false
		}
	}
	if paused {
		for _, item := range visible {
			if !nc.screenPaused && !item.screenLastTick.IsZero() {
				if delta := now.Sub(item.screenLastTick); delta > 0 {
					item.screenElapsed += delta
					if dur := item.screenDuration(); item.screenElapsed >= dur {
						item.screenElapsed = max(0, dur-pausedNotificationMinRemaining)
					}
				}
			}
			item.screenLastTick = time.Time{}
		}
		nc.screenPaused = true
		return 0
	}

	nc.screenPaused = false
	for _, item := range visible {
		if item.screenLastTick.IsZero() {
			item.screenLastTick = now
		} else {
			if delta := now.Sub(item.screenLastTick); delta > 0 {
				item.screenElapsed += delta
			}
			item.screenLastTick = now
		}
		if item.screenElapsed >= item.screenDuration() {
			item.VisibleOnScreen = false
			item.screenLastTick = time.Time{}
		}
	}

	remainingScreen := nc.screenItems()
	if len(remainingScreen) == 0 {
		nc.tappedExpanded = false
		return 0
	}
	if len(remainingScreen) > maxVisible {
		remainingScreen = remainingScreen[:maxVisible]
	}
	var nextWake time.Duration
	for _, item := range remainingScreen {
		if item.screenLastTick.IsZero() {
			item.screenLastTick = now
		}
		rem := item.screenDuration() - item.screenElapsed
		if rem <= 0 {
			item.VisibleOnScreen = false
			item.screenLastTick = time.Time{}
			continue
		}
		if nextWake == 0 || rem < nextWake {
			nextWake = rem
		}
	}
	return nextWake
}

func (nc *notificationCenter) screenItems() []*studioNotification {
	out := []*studioNotification{}
	for _, item := range nc.items {
		if item.VisibleOnScreen && !item.Handled {
			out = append(out, item)
		}
	}
	return out
}

func (nc *notificationCenter) inboxItems() []*studioNotification {
	out := []*studioNotification{}
	for _, item := range nc.items {
		if !item.Handled || item.NeedsDecision {
			out = append(out, item)
		}
	}
	return out
}

func (nc *notificationCenter) unreadCount() int {
	count := 0
	for _, item := range nc.items {
		if (!item.Read && !item.Handled) || (item.NeedsDecision && !item.Handled) {
			count++
		}
	}
	return count
}

func (s *viewState) baseWindowTitle() string {
	if s.windowTitle != "" {
		return s.windowTitle
	}
	return "InferYard Studio"
}

func (s *viewState) syncWindowTitle() {
	nc := s.notifications()
	title := s.baseWindowTitle()
	if n := nc.unreadCount(); n > 0 {
		title = fmt.Sprintf("(%d) %s", n, title)
	}
	if title == nc.lastWindowTitle {
		return
	}
	nc.lastWindowTitle = title
	if s.win != nil {
		s.win.SetTitle(title)
	}
}

func (s *viewState) shouldSuppressOSNotification(event core.Event) bool {
	if s.win == nil || !s.win.IsFocused() {
		return false
	}
	switch event.Type {
	case "report.created":
		return s.selected == "reports" || s.selected == "dashboard"
	case "run.state":
		return s.selected == "dashboard"
	case "download.state":
		return s.selected == "downloads"
	case "engine.state":
		return s.selected == "dashboard"
	default:
		return false
	}
}

func (s *viewState) ingestCoreEvents(events []core.Event) {
	for _, event := range events {
		s.ingestCoreEvent(event)
	}
}

func (s *viewState) ingestCoreEvent(event core.Event) {
	switch event.Type {
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
							s.navigateTo("dashboard", "")
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
						s.navigateTo("dashboard", "")
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
						OnClick: func() { s.navigateTo("downloads", "") },
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
					OnClick: func() { s.navigateTo("downloads", "") },
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
					OnClick: func() { s.navigateTo("downloads", "") },
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
			primaryLabel := l10n.TR("notif.action.go_tasks")
			primaryTarget := "dashboard"
			if payload.Engine.Origin == core.EngineOriginStandalone {
				primaryLabel = l10n.TR("models.serve.action.go_models")
				primaryTarget = "models"
			}
			s.pushNotification(studioNotification{
				Key:           "engine:failed",
				Title:         l10n.TR("notif.engine.failed.title"),
				Body:          l10n.TR("notif.engine.failed.body"),
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   primaryLabel,
					OnClick: func() { s.navigateTo(primaryTarget, "") },
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
					OnClick: func() { s.navigateTo("dashboard", "") },
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
					Label:   l10n.TR("notif.action.view_engines"),
					OnClick: func() { s.navigateTo("models", "") },
				},
			})
		}
	}
}
