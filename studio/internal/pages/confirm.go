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
	destructiveNewTask        destructiveKind = "new_task"
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
	prompt       string
	disabled     bool
	compact      bool
}

// destructiveAction renders a danger-styled action and asks for one more
// click before it reports true. The first click only opens the inline
// confirmation; the prompt states what the action cancels and what stays.
func (s *viewState) destructiveAction(c *ui.Context, option destructiveOption) bool {
	t := tokens(c)
	request := destructiveRequest{kind: string(option.kind), id: option.id}
	if option.kind == destructiveStopEngine {
		request.pid, request.startTicks = s.engineState.Pid, s.engineState.StartTicks
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
		prompt := ui.Text(c, option.prompt)
		if option.compact {
			prompt.FontSize(t.Type.Caption)
		}
		ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
			if primaryButton(c, option.confirmLabel).AlignSelf(ui.Start).Clicked() {
				confirmed = true
			}
			if button(c, "返回").AlignSelf(ui.Start).Clicked() {
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

// destructiveTargetValid re-reads the live state when the confirm click
// arrives. A prompt can stay open across refreshes, so the state that enabled
// the first click may be gone: the run finished, the engine stopped, the
// download completed or the record disappeared. Validity is derived here, not
// at each call site, so every gate gets the same re-check.
func (s *viewState) destructiveTargetValid(request destructiveRequest) bool {
	switch destructiveKind(request.kind) {
	case destructiveNewTask:
		return s.workInFlight()
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

// workInFlight reports whether starting a new task would cancel work the user
// has not finished. Idle preparation screens keep the single-click entry.
func (s *viewState) workInFlight() bool {
	if s.running() != nil || s.busy || s.newTaskPending {
		return true
	}
	if s.speed != nil && s.speed.current != nil && s.speed.current.active() {
		return true
	}
	switch s.task.stage {
	case "", "prepare_candidate", "done":
		return false
	default:
		return true
	}
}
