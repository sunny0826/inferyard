package pages

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"inferyard/studio/internal/core"
)

type notificationAction struct {
	Label   string
	OnClick func()
}

type studioNotification struct {
	ID              string
	Key             string
	Title           string
	Body            string
	Kind            stateKind
	CreatedAt       time.Time
	NeedsDecision   bool
	VisibleOnScreen bool
	Handled         bool
	Read            bool
	Announced       bool
	Secondary       *notificationAction
	Primary         *notificationAction
}

type notificationCenter struct {
	items           []*studioNotification
	nextID          uint64
	tappedExpanded  bool
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
				existing.NeedsDecision = n.NeedsDecision
				existing.Primary = n.Primary
				existing.Secondary = n.Secondary
				existing.VisibleOnScreen = true
				existing.Read = false
				existing.Announced = false
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
			if !item.NeedsDecision {
				item.Read = true
				item.Handled = true
			}
		}
	}
	nc.tappedExpanded = false
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
	}
	s.syncWindowTitle()
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
		body := "新测评报告已生成，可随时打开查看。"
		if reportID != "" {
			body = "测评报告 " + reportID + " 已生成，可随时打开查看。"
		}
		s.pushNotification(studioNotification{
			Key:   "report:" + reportID,
			Title: "报告已生成",
			Body:  body,
			Kind:  stateSuccess,
			Primary: &notificationAction{
				Label: "查看报告",
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
		name := "测评任务"
		if task.Kind == "context_speed" {
			name = "长上下文专项"
		} else if task.Bundle != nil && strings.TrimSpace(*task.Bundle) != "" {
			name = "测评题集 " + *task.Bundle
		}
		switch task.State {
		case "done":
			s.pushNotification(studioNotification{
				Key:   "run:" + task.TaskId + ":done",
				Title: name + "已完成",
				Body:  "结果已保存，可查看实时详情或生成报告。",
				Kind:  stateSuccess,
				Primary: &notificationAction{
					Label: "查看详情",
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
				Title:         name + "失败",
				Body:          "运行未正常完成，请检查停止原因与错误详情后重试。",
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label: "前往排查",
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
				Title: name + "已取消",
				Body:  "已完成进度与结果已保留。",
				Kind:  stateInfo,
			})
		}
	case "download.state":
		var payload struct {
			Task struct {
				TaskId  string `json:"task_id"`
				Kind    string `json:"kind"`
				State   string `json:"state"`
				OutName string `json:"out_name"`
			} `json:"task"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		task := payload.Task
		kind := "模型"
		if task.Kind == "engine" {
			kind = "引擎"
		}
		target := kind
		if strings.TrimSpace(task.OutName) != "" {
			target = kind + " " + task.OutName
		}
		switch task.State {
		case "done":
			s.pushNotification(studioNotification{
				Key:   "dl:" + task.TaskId + ":done",
				Title: kind + "下载完成",
				Body:  target + " 已就绪，可返回任务页开始测评。",
				Kind:  stateSuccess,
				Primary: &notificationAction{
					Label:   "前往下载",
					OnClick: func() { s.navigateTo("downloads", "") },
				},
			})
		case "failed":
			s.pushNotification(studioNotification{
				Key:           "dl:" + task.TaskId + ":failed",
				Title:         kind + "下载失败",
				Body:          target + " 下载失败，请检查网络或来源后重新创建任务。",
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   "查看并重试",
					OnClick: func() { s.navigateTo("downloads", "") },
				},
			})
		}
	case "engine.state":
		var payload struct {
			Engine struct {
				State string `json:"state"`
			} `json:"engine"`
		}
		if json.Unmarshal(event.Data, &payload) != nil {
			return
		}
		switch payload.Engine.State {
		case "running":
			if s.engineStartToastID != "" {
				s.resolveProgressToast(s.engineStartToastID, "引擎已就绪", "", nil)
				s.engineStartToastID = ""
			}
		case "failed":
			if s.engineStartToastID != "" {
				s.dismissToast(s.engineStartToastID)
				s.engineStartToastID = ""
			}
			s.pushNotification(studioNotification{
				Key:           "engine:failed",
				Title:         "托管引擎异常退出",
				Body:          "模型服务未正常保持运行，请检查引擎状态并重新启动。",
				Kind:          stateDanger,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   "前往任务",
					OnClick: func() { s.navigateTo("dashboard", "") },
				},
			})
		case "orphaned":
			s.pushNotification(studioNotification{
				Key:           "engine:orphaned",
				Title:         "检测到遗留模型服务",
				Body:          "上次运行的托管服务或记录尚未清理，请先处理后再继续测评。",
				Kind:          stateWarning,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   "前往处理",
					OnClick: func() { s.navigateTo("dashboard", "") },
				},
			})
		}
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
				Title:         "当前所选引擎不可用",
				Body:          payload.Engine + " 未通过核验：" + human(payload.Eligibility.Reason) + "。",
				Kind:          stateWarning,
				NeedsDecision: true,
				Primary: &notificationAction{
					Label:   "查看引擎",
					OnClick: func() { s.navigateTo("models", "") },
				},
			})
		}
	}
}
