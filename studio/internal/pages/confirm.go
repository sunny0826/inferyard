package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// destructiveKind names an action that discards in-flight work. The same
// builder serves the production window and the preview, so a confirmation is
// never an unmodeled operation: the preview only changes window memory after
// the confirmed action dispatches its usual preview intent.
type destructiveKind string

const (
	destructiveCancelRun      destructiveKind = "cancel_run"
	destructiveStopEngine     destructiveKind = "stop_engine"
	destructiveStopOrphan     destructiveKind = "stop_orphan"
	destructiveCancelDownload destructiveKind = "cancel_download"
)

type destructiveRequest struct {
	kind, id string
	// pid and startTicks snapshot the engine process the prompt was opened
	// for; a confirm whose snapshot no longer matches must not stop anything.
	pid, startTicks int64
}

// engineSnapshot records enough identity to distinguish one engine process
// from a replacement while a stop prompt stays open.
type engineSnapshot struct {
	pid, startTicks int64
}

func (s *viewState) currentEngine() engineSnapshot {
	return engineSnapshot{pid: s.engineState.Pid, startTicks: s.engineState.StartTicks}
}

// matchesEngine reports whether the recorded process is still the one running.
func (r destructiveRequest) matchesEngine(current engineSnapshot) bool {
	return r.pid == current.pid && r.startTicks == current.startTicks
}

type destructiveOption struct {
	kind         destructiveKind
	id           string
	label        string
	confirmLabel string
	// dismissLabel names the way out of the prompt in the words of the action
	// it would discard ("保持运行", "继续下载"). Empty falls back to a neutral
	// "暂不操作": an inline confirmation never navigated anywhere, so a
	// "返回" button would describe a move the user did not make.
	dismissLabel string
	prompt       string
	disabled     bool
}

// destructiveDismissLabel is the cancel copy for one confirmation.
func destructiveDismissLabel(option destructiveOption) string {
	if option.dismissLabel != "" {
		return option.dismissLabel
	}
	return "暂不操作"
}

// destructiveAction renders a danger-styled action and asks for one more
// click before it reports true. The first click only opens the inline
// confirmation; the prompt states what the action cancels and what stays.
func (s *viewState) destructiveAction(c *ui.Context, option destructiveOption) bool {
	t := tokens(c)
	request := destructiveRequest{kind: string(option.kind), id: option.id}
	if option.kind == destructiveStopEngine {
		request.pid, request.startTicks = s.engineState.Pid, s.engineState.StartTicks
		s.dropStaleStopPrompt()
	}
	pending := s.pendingDestructive == request
	if !pending {
		if dangerButton(c, option.label).AlignSelf(ui.Start).Disabled(option.disabled).Clicked() {
			s.pendingDestructive = request
		}
		return false
	}
	confirmed, cancelled := false, false
	ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Inset).Background(c.Theme().SurfaceHover).Radius(t.Radius.Inset).Children(func() {
		ui.Text(c, option.prompt)
		ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
			if primaryButton(c, option.confirmLabel).AlignSelf(ui.Start).Clicked() {
				confirmed = true
			}
			if button(c, destructiveDismissLabel(option)).AlignSelf(ui.Start).Clicked() {
				cancelled = true
			}
		})
	})
	if confirmed && (option.disabled || !s.destructiveTargetValid(request)) {
		// The target changed after the prompt opened: drop the request and
		// dispatch nothing, so a stale click cannot act on another target.
		s.pendingDestructive = destructiveRequest{}
		return false
	}
	if confirmed || cancelled {
		s.pendingDestructive = destructiveRequest{}
	}
	return confirmed
}

// dropStaleStopPrompt forgets an engine-stop prompt whose process is gone:
// the engine stopped or failed, or a replacement instance took over. Keeping
// the request would only leave a dead prompt armed, ready to reappear if an
// engine with the same snapshot comes back. contentView calls it every frame,
// because once the engine is gone no stop entry renders to notice the change.
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

// destructiveTargetValid re-reads the live state when the confirm click
// arrives. A prompt can stay open across refreshes, so the state that enabled
// the first click may be gone: the run finished, the engine stopped, the
// download completed or the record disappeared. Validity is derived here, not
// at each call site, so every gate gets the same re-check.
func (s *viewState) destructiveTargetValid(request destructiveRequest) bool {
	switch destructiveKind(request.kind) {
	case destructiveCancelRun:
		return s.cancelTargetActive(request.id)
	case destructiveStopEngine:
		if !s.engineStoppable() {
			return false
		}
		return request.matchesEngine(s.currentEngine())
	case destructiveStopOrphan:
		return s.engineState.State == "orphaned" && s.idleForEngineStop()
	case destructiveCancelDownload:
		return s.downloadDownloading(request.id) && !s.busy
	}
	return true
}

// cancelTargetActive covers both a queued run and the specialty measurement,
// which share the cancel kind but keep their own state.
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

// cancelRunPrompt states the real scope of a cancel: the whole round stops when
// the task page owns it, the CLI still seals the cancelled evidence, and no
// complete report is produced.
func cancelRunPrompt(lastRound bool) string {
	scope := "将取消本测评题集的运行并等待安全收尾；已写入结果目录的进度和已取消的证据会被保存。"
	if lastRound {
		scope = "将取消本轮所有未结束的运行并等待安全收尾；已写入结果目录的进度和已取消的证据会被保存。"
	}
	return scope + "本轮不会生成完整报告；取消证据不等同于完整测评结果。"
}

// cancelRunGate renders the run-cancel confirmation for one active task.
func (w *viewState) cancelRunGate(c *ui.Context, id, state string) bool {
	return w.destructiveAction(c, destructiveOption{
		kind: destructiveCancelRun, id: id, label: "取消运行", confirmLabel: "确认取消运行",
		dismissLabel: "保持运行",
		prompt:       cancelRunPrompt(w.task.stage != ""),
		disabled:     (w.busy && w.task.stage == "") || state == "cancelling",
	})
}

// stopEngineGate renders the engine-stop confirmation; a replacement process
// opened while the prompt was pending is rejected by the identity check.
func (w *viewState) stopEngineGate(c *ui.Context) bool {
	return w.destructiveAction(c, destructiveOption{
		kind: destructiveStopEngine, label: "停止服务", confirmLabel: "确认停止服务",
		dismissLabel: "保持服务运行",
		prompt:       "将停止 Studio 启动的模型服务；正在加载的模型会中断，之后需要重新启动并绑定。",
		disabled:     w.busy || w.running() != nil,
	})
}

// stopEngineCall is the engine-stop boundary; tests inject a recorder to prove
// a stale confirmation never stops a replacement process.
func (s *viewState) stopEngineCall() (core.EngineState, *core.ApiError) {
	if s.stopEngineAPI != nil {
		return s.stopEngineAPI()
	}
	if s.service == nil {
		return core.EngineState{}, core.Err("internal_error")
	}
	return s.service.StopEngine()
}

// cancelRunTask is the service boundary for a run cancel. Tests inject a
// recorder here to observe whether a dismissed or confirmed prompt cancels.
func (s *viewState) cancelRunTask(id string) (core.RunTask, *core.ApiError) {
	if s.cancelRun != nil {
		return s.cancelRun(id)
	}
	if s.service == nil {
		return core.RunTask{}, core.Err("internal_error")
	}
	return s.service.CancelRun(id)
}

// taskStillRunning re-reads the queue so a confirmation opened before a state
// change cannot act on a finished run.
func (s *viewState) taskStillRunning(id string) bool {
	for i := range s.tasks {
		if s.tasks[i].TaskId == id {
			return !terminal(s.tasks[i].State) && s.tasks[i].State != "cancelling"
		}
	}
	return false
}

// downloadDownloading re-reads downloads for the same reason.
func (s *viewState) downloadDownloading(id string) bool {
	for _, task := range s.downloads {
		if task.TaskId == id {
			return task.State == "downloading"
		}
	}
	return false
}

// clearPromptOnNavigation drops an armed confirmation when the visible page
// changes. A prompt belongs to the page that opened it: a sidebar click, a
// header popover jump or a run-detail switch must not leave it clickable on
// whatever page appears next.
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
