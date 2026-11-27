package pages

import (
	"fmt"
	"time"

	"inferyard/studio/internal/core"
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
	items             []*studioNotification
	nextID            uint64
	tappedExpanded    bool
	hoverExpanded     bool
	hoverSuppressed   bool
	engagedInExpanded bool
	wasStackHovered   bool
	wasStackFocused   bool
	hoverEnteredAt    time.Time
	focusEnteredAt    time.Time
	graceStartedAt    time.Time
	screenPaused      bool
	bellOpen          bool
	lastWindowTitle   string
}

func (nc *notificationCenter) resetStackExpansion() {
	nc.tappedExpanded = false
	nc.hoverExpanded = false
	nc.hoverSuppressed = false
	nc.engagedInExpanded = false
	nc.wasStackHovered = false
	nc.wasStackFocused = false
	nc.hoverEnteredAt = time.Time{}
	nc.focusEnteredAt = time.Time{}
	nc.graceStartedAt = time.Time{}
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
	nc.resetStackExpansion()
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
	nc.resetStackExpansion()
	nc.screenPaused = false
	s.syncWindowTitle()
}

func (nc *notificationCenter) advanceScreenTimers(now time.Time, maxVisible int, paused bool) time.Duration {
	if maxVisible <= 0 {
		maxVisible = maxVisibleScreenNotifications
	}
	screen := nc.screenItems()
	if len(screen) == 0 {
		nc.resetStackExpansion()
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
		nc.resetStackExpansion()
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
	if s.preview == nil {
		if n := nc.unreadCount(); n > 0 {
			title = fmt.Sprintf("(%d) %s", n, title)
		}
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
	cur := canonicalPageID(s.selected)
	switch event.Type {
	case "report.created":
		return cur == "reports" || cur == "benchmarks"
	case "run.state":
		return cur == "benchmarks"
	case "download.state":
		return cur == "library"
	case "engine.state":
		return cur == "services" || cur == "benchmarks"
	default:
		return false
	}
}
