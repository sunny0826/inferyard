package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

type destructiveKind string

const (
	destructiveCancelRun          destructiveKind = "cancel_run"
	destructiveStopEngine         destructiveKind = "stop_engine"
	destructiveStopOrphan         destructiveKind = "stop_orphan"
	destructiveDiscardOrphan      destructiveKind = "discard_orphan"
	destructiveCancelDownload     destructiveKind = "cancel_download"
	destructiveRemoveInstallation destructiveKind = "remove_installation"
)

type destructiveRequest struct {
	kind, id        string
	pid, startTicks int64
}

type engineSnapshot struct {
	pid, startTicks int64
}

func (s *viewState) currentEngine() engineSnapshot {
	return engineSnapshot{pid: s.engineState.Pid, startTicks: s.engineState.StartTicks}
}

func (r destructiveRequest) matchesEngine(current engineSnapshot) bool {
	return r.pid == current.pid && r.startTicks == current.startTicks
}

type destructiveOption struct {
	kind         destructiveKind
	id           string
	label        string
	confirmLabel string
	dismissLabel string
	prompt       string
	disabled     bool
}

func destructiveDismissLabel(option destructiveOption) string {
	if option.dismissLabel != "" {
		return option.dismissLabel
	}
	return "暂不操作"
}

func (s *viewState) destructiveAction(c *ui.Context, option destructiveOption) bool {
	return s.destructiveActionWithTrigger(c, option, false, func(bool) ui.Element {
		return dangerButton(c, option.label).AlignSelf(ui.Start).Disabled(option.disabled)
	})
}

func (s *viewState) destructiveActionWithTrigger(c *ui.Context, option destructiveOption, alignRight bool, renderTrigger func(open bool) ui.Element) bool {
	t := tokens(c)
	request := destructiveRequest{kind: string(option.kind), id: option.id}
	if option.kind == destructiveStopEngine {
		request.pid, request.startTicks = s.engineState.Pid, s.engineState.StartTicks
		s.dropStaleStopPrompt()
	}
	c.Key("popconfirm-" + string(option.kind) + "-" + option.id)
	open := s.pendingDestructive == request
	trigger := renderTrigger(open)
	if trigger.Clicked() {
		if s.pendingDestructive == request {
			s.pendingDestructive = destructiveRequest{}
		} else {
			s.pendingDestructive = request
		}
		open = s.pendingDestructive == request
	}
	confirmed := false
	components.Popover(c, trigger, &open, func(panel ui.Element) {
		if alignRight {
			panel.AttachTo(trigger, ui.AnchorBottomRight, ui.AnchorTopRight)
		}
		panel.Width(c.Theme().Space(78)).MaxWidthPercent(100).Label(option.prompt)
		panel.Children(func() {
			ui.Column(c).Gap(t.Density.Row).Children(func() {
				ui.Text(c, option.prompt)
				ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
					if primaryButton(c, option.confirmLabel).AlignSelf(ui.Start).Clicked() {
						confirmed = true
						open = false
					}
					if button(c, destructiveDismissLabel(option)).AlignSelf(ui.Start).Clicked() {
						open = false
					}
				})
			})
		})
	})
	if !open && s.pendingDestructive == request {
		s.pendingDestructive = destructiveRequest{}
	}
	if confirmed && (option.disabled || !s.destructiveTargetValid(request)) {
		s.pendingDestructive = destructiveRequest{}
		return false
	}
	if confirmed {
		s.pendingDestructive = destructiveRequest{}
	}
	return confirmed
}

func (s *viewState) destructiveDialog(c *ui.Context, option destructiveOption) bool {
	request := s.destructiveRequestFor(option)
	c.Key("alertdialog-" + string(option.kind) + "-" + option.id)
	if dangerButton(c, option.label).AlignSelf(ui.Start).Disabled(option.disabled).Clicked() {
		s.pendingDestructive = request
	}
	return s.destructiveDialogModal(c, option)
}

func (s *viewState) destructiveRequestFor(option destructiveOption) destructiveRequest {
	request := destructiveRequest{kind: string(option.kind), id: option.id}
	if option.kind == destructiveStopEngine {
		request.pid, request.startTicks = s.engineState.Pid, s.engineState.StartTicks
		s.dropStaleStopPrompt()
	}
	return request
}

func (s *viewState) destructiveDialogModal(c *ui.Context, option destructiveOption) bool {
	request := s.destructiveRequestFor(option)
	open := s.pendingDestructive == request
	result := components.AlertDialog(c, &open, components.AlertDialogOptions{
		DialogOptions: components.DialogOptions{Label: option.label},
		Title:         option.label,
		Message:       option.prompt,
		ConfirmLabel:  option.confirmLabel,
		CancelLabel:   destructiveDismissLabel(option),
	})
	if !open && s.pendingDestructive == request {
		s.pendingDestructive = destructiveRequest{}
	}
	if result != components.DialogConfirmed {
		return false
	}
	if option.disabled || !s.destructiveTargetValid(request) {
		s.pendingDestructive = destructiveRequest{}
		return false
	}
	s.pendingDestructive = destructiveRequest{}
	return true
}

func (s *viewState) dropStaleStopPrompt() {
	pending := s.pendingDestructive
	if destructiveKind(pending.kind) != destructiveStopEngine {
		return
	}
	if live := s.engineState.State == "starting" || s.engineState.State == "running"; live && pending.matchesEngine(s.currentEngine()) {
		return
	}
	s.pendingDestructive = destructiveRequest{}
}

func (s *viewState) destructiveTargetValid(request destructiveRequest) bool {
	switch destructiveKind(request.kind) {
	case destructiveCancelRun:
		return s.cancelTargetActive(request.id)
	case destructiveStopEngine:
		if !s.engineStoppable() {
			return false
		}
		return request.matchesEngine(s.currentEngine())
	case destructiveStopOrphan, destructiveDiscardOrphan:
		return s.engineState.State == "orphaned" && s.idleForEngineStop()
	case destructiveCancelDownload:
		return s.downloadDownloading(request.id) && !s.busy
	}
	return true
}

func (s *viewState) cancelTargetActive(id string) bool {
	if s.taskStillRunning(id) {
		return true
	}
	speed := s.speed
	return speed != nil && speed.current != nil && speed.current.id == id && speed.current.active()
}

func (s *viewState) idleForEngineStop() bool {
	return !s.busy && !s.newTaskPending && s.running() == nil
}

func (s *viewState) engineStoppable() bool {
	state := s.engineState.State
	return (state == "starting" || state == "running") && s.idleForEngineStop()
}

func cancelRunPrompt(lastRound bool) string {
	if lastRound {
		return "将取消本轮所有未结束的运行并等待安全收尾，已完成的进度会保留。"
	}
	return "将取消当前测评题集的运行并等待安全收尾，已完成的进度会保留。"
}

func (w *viewState) cancelRunGate(c *ui.Context, id, state string) bool {
	wholeRound := w.task.stage != ""
	option := destructiveOption{
		kind: destructiveCancelRun, id: id, label: "取消运行", confirmLabel: "确认取消运行",
		dismissLabel: "保持运行",
		prompt:       cancelRunPrompt(wholeRound),
		disabled:     (w.busy && w.task.stage == "") || state == "cancelling",
	}
	if wholeRound {
		return w.destructiveDialog(c, option)
	}
	return w.destructiveAction(c, option)
}

func (w *viewState) stopEngineGate(c *ui.Context) bool {
	return w.destructiveDialog(c, destructiveOption{
		kind: destructiveStopEngine, label: "停止服务", confirmLabel: "确认停止服务",
		dismissLabel: "保持服务运行",
		prompt:       "将停止 Studio 启动的模型服务；正在加载的模型会中断，之后需要重新启动并绑定。",
		disabled:     w.busy || w.running() != nil,
	})
}

func (s *viewState) stopEngineCall() (core.EngineState, *core.ApiError) {
	if s.stopEngineAPI != nil {
		return s.stopEngineAPI()
	}
	if s.service == nil {
		return core.EngineState{}, core.Err("internal_error")
	}
	return s.service.StopEngine()
}

func (s *viewState) cancelRunTask(id string) (core.RunTask, *core.ApiError) {
	if s.cancelRun != nil {
		return s.cancelRun(id)
	}
	if s.service == nil {
		return core.RunTask{}, core.Err("internal_error")
	}
	return s.service.CancelRun(id)
}

func (s *viewState) taskStillRunning(id string) bool {
	for i := range s.tasks {
		if s.tasks[i].TaskId == id {
			return !terminal(s.tasks[i].State) && s.tasks[i].State != "cancelling"
		}
	}
	return false
}

func (s *viewState) downloadDownloading(id string) bool {
	for _, task := range s.downloads {
		if task.TaskId == id {
			return task.State == "downloading"
		}
	}
	return false
}

func (s *viewState) clearPromptOnNavigation() {
	page := s.selected
	if s.selected == "dashboard" && s.detailID != "" {
		page += "/" + s.detailID
	}
	if page == s.promptPage {
		return
	}
	s.promptPage = page
	s.pendingDestructive = destructiveRequest{}
}
