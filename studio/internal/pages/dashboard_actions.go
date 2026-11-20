package pages

import (
	"context"
	"fmt"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func fresh(root, name string) string {
	return filepath.Join(root, fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
}
func (w *viewState) active() *inferyard.BundleProgress {
	if w.progress != nil {
		for i := range w.progress.Bundles {
			if len(w.task.bundles) > 0 && !slices.Contains(w.task.bundles, w.progress.Bundles[i].Bundle) {
				continue
			}
			if w.progress.Bundles[i].Phase != "run_complete" {
				return &w.progress.Bundles[i]
			}
		}
	}
	return nil
}

// bindTarget is the bundle a manual connection applies to: the first selected
// bundle that still needs binding. An earlier bundle that is already bound and
// merely waits to run must not capture the manual form, or the unbound bundle
// behind it can never leave the bind step.
func (w *viewState) bindTarget() *inferyard.BundleProgress {
	if w.progress == nil {
		return nil
	}
	for i := range w.progress.Bundles {
		b := &w.progress.Bundles[i]
		if len(w.task.bundles) > 0 && !slices.Contains(w.task.bundles, b.Bundle) {
			continue
		}
		if b.Phase == "run_complete" {
			continue
		}
		if b.NextAction == "bind" || b.BoundPath == nil {
			return b
		}
	}
	return w.active()
}
func (w *viewState) running() *core.RunTask {
	for i := range w.tasks {
		if !terminal(w.tasks[i].State) {
			return &w.tasks[i]
		}
	}
	return nil
}

// suggestInitDir proposes the init target. A fresh child is suggested only
// for an existing directory that is not already an initialized workspace;
// nesting workspaces is never the right next step.
func suggestInitDir(path string, exists, initialized bool) string {
	if exists && !initialized && path != "" {
		return fresh(path, "workspace")
	}
	return path
}

// executeMessage separates queued runs (work has just started) from commands
// that are already done when they return.
func executeMessage(r core.WizardResult) string {
	if len(r.Tasks) > 0 {
		return "运行已开始，证据收尾完成后进入下一步。"
	}
	return "操作完成，已更新工作区进度。"
}
func (w *viewState) baseCommand() []string {
	if w.progress == nil {
		return nil
	}
	b := w.active()
	command := emptyText(w.progress.NextCommand)
	if b != nil && b.NextAction == "resume" {
		command = emptyText(b.NextCommand)
	}
	args, e := inferyard.SplitCommand(command)
	if e != nil {
		return nil
	}
	// progress is the single source of truth: its next_command already picks a
	// fresh, non-existing --out via the CLI's new_output(); never rewrite it.
	if w.progress.NextAction == "prepare_candidate" {
		args = setArg(args, "--engine", w.engine)
	}
	if w.progress.NextAction == "device_check" && emptyText(w.ready.Models.Path) != "" {
		args = setArg(args, "--models-root", *w.ready.Models.Path)
	}
	return args
}
func (w *viewState) nextCommand() string {
	args := w.baseCommand()
	if args == nil {
		return ""
	}
	if w.progress.NextAction == "bind" {
		pid, _, _ := strings.Cut(w.process, " · ")
		args = setArg(args, "--pid", pid)
		args = setArg(args, "--endpoint", w.bindEndpoint)
	}
	return commandOf(args)
}

// managedBindCommand binds the Studio-managed engine: PID and endpoint come
// from the supervised process, not from a manual process pick.
func (w *viewState) managedBindCommand() (string, *core.ApiError) {
	if w.engineState.State != "running" {
		return "", core.Err("invalid_request")
	}
	return w.bindCommand(strconv.FormatInt(w.engineState.Pid, 10), w.engineState.Endpoint)
}

func (w *viewState) bindCommand(pid, endpoint string) (string, *core.ApiError) {
	if w.preview != nil {
		// The preview never builds a real argv: a modeled bind is dispatched in
		// memory, and anything the simulator does not represent stays an error.
		if w.progress == nil || w.bindTarget() == nil {
			return "", core.Err("preview_unsupported")
		}
		w.simulate(previewBind)
		return "", nil
	}
	b := w.bindTarget()
	if b == nil || w.progress == nil {
		return "", core.Err("invalid_request")
	}
	if b.Phase == "bound_missing" {
		args, err := inferyard.SplitCommand(emptyText(b.NextCommand))
		if err != nil {
			args = w.baseCommand()
		}
		if len(args) < 2 || args[0] != "config" || args[1] != "bind" {
			return "", core.Err("invalid_request")
		}
		return commandOf(setArg(setArg(args, "--pid", pid), "--endpoint", endpoint)), nil
	}
	value, err := strconv.ParseInt(pid, 10, 64)
	if err != nil {
		return "", core.Err("invalid_request")
	}
	out, e := core.NextOutput(w.progress.Workspace, "bound", b.Bundle)
	if e != nil {
		return "", e
	}
	args, e := core.BindArgs(w.candidate, out, value, endpoint)
	if e != nil {
		return "", e
	}
	return commandOf(args), nil
}

func (w *viewState) startEngine() {
	if w.simulate(previewStartEngine) {
		return
	}
	if w.task.stage == "bind" && w.task.start.candidate != "" {
		w.retryBenchmark()
		return
	}
	if !w.beginStart() {
		return
	}
	b := w.active()
	candidate := w.candidate
	bundle, workspace := *b, w.progress.Workspace
	ctx, cancel := context.WithCancel(w.ctx)
	w.taskCancel = cancel
	w.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		state, e := w.service.StartEngine(candidate)
		if e != nil {
			return "", e
		}
		for state.State == "starting" || state.State == "stopping" {
			if !taskWait(ctx) {
				return "", core.Err("cancelled")
			}
			state = w.service.Engine()
		}
		if state.State != "running" {
			if state.Error != nil {
				return "", state.Error
			}
			return "", core.Err("engine_start_failed")
		}
		if ctx.Err() != nil {
			return "", core.Err("cancelled")
		}
		if bundle.NextAction == "resume" || blockedPlan(bundle) {
			return "服务已就绪。", nil
		}
		command, _, e := benchmarkBindCommand(workspace, bundle, state)
		if e != nil {
			return "", e
		}
		r, e := w.service.WizardExecute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return "", e
		}
		if r.ExitCode != 0 {
			return "", wizardFailure(r)
		}
		return "服务已启动并重新绑定。", nil
	})
}
func (w *viewState) stopEngine() {
	if w.simulate(previewStopEngine) {
		return
	}
	w.guideWork(func() (string, *core.ApiError) {
		_, e := w.stopEngineCall()
		return "已请求停止服务。", e
	})
}
func setArg(args []string, flag, value string) []string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			args[i+1] = value
			return args
		}
	}
	return append(args, flag, value)
}
func commandOf(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = quote(a)
	}
	return "inferyard " + strings.Join(out, " ")
}

// executeBinding runs a bind command for the bundle the manual form currently
// targets.
func (w *viewState) executeBinding(command string, e *core.ApiError) {
	w.executeBindingFor(w.bindTargetBundle(), command, e)
}

// bindTargetBundle names the bundle a manual connection would apply to.
func (w *viewState) bindTargetBundle() string {
	if b := w.bindTarget(); b != nil {
		return b.Bundle
	}
	return ""
}

// executeBindingFor runs a manual or managed bind command for an explicit target.
// The caller captures the bundle before the command is built, so a rejection
// marks that bundle instead of whichever bundle refreshed progress lists first.
func (w *viewState) executeBindingFor(target, command string, e *core.ApiError) {
	if w.preview != nil {
		// bindCommand already dispatched a modeled bind in memory. Only a
		// refused attempt arrives here with an error.
		if e != nil {
			w.previewAction()
		}
		return
	}
	if e != nil {
		w.failBindingFor(target, e)
		return
	}
	if w.busy {
		return
	}
	w.stepConnection()
	ctx, cancel := context.WithCancel(w.ctx)
	w.taskCancel = cancel
	w.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		r, e := w.service.WizardExecute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			w.updateView(func() { w.failBindingFor(target, e) })
			return "", e
		}
		if r.ExitCode != 0 {
			e := wizardFailure(r)
			w.updateView(func() { w.failBindingFor(target, e) })
			return "", e
		}
		w.updateView(w.clearRecoveredBinding)
		return "绑定完成。", nil
	})
}

// updateView applies a state change on the window's update queue when the view
// is mounted; in-memory tests apply it directly.
func (w *viewState) updateView(fn func()) {
	if w.win == nil {
		fn()
		return
	}
	w.win.Update(fn)
}

// stepConnection marks the task as actively connecting, so the step does not
// show the previous rejected connection while the new attempt runs.
func (w *viewState) stepConnection() {
	w.bindFailure, w.failedBundle = nil, ""
}

// beginStart gates a managed-engine start and, when it can proceed, begins a
// fresh attempt. A refused click must not discard the cause the user sees.
func (w *viewState) beginStart() bool {
	if w.busy || w.running() != nil || w.candidate == "" || w.active() == nil {
		return false
	}
	w.beginAttempt()
	return true
}

// beginAttempt starts a fresh start, retry or reconnect attempt. The previous
// attempt's rejected connection must not outrank whatever this one reports, so
// its cause, its bundle and its handoff error are dropped first. The page banner
// may carry the same rejection; that copy goes with it, while an unrelated page
// error stays.
func (w *viewState) beginAttempt() {
	if w.bindFailure != nil && w.failure != nil && w.failure.Code == w.bindFailure.Code {
		w.failure = nil
	}
	w.bindFailure, w.failedBundle, w.handoffFailure = nil, "", nil
}

// failBinding attributes a rejected connection to the bind step: the step shows
// the cause and its recovery guidance instead of leaving it only in the page
// banner, and the banner carries the same newest cause rather than an older one.
func (w *viewState) failBinding(e *core.ApiError) {
	if e == nil {
		return
	}
	w.bindFailure = e
	w.failure = e
}

// failBindingFor attributes a rejection to the bundle the attempt targeted. The
// progress card then marks that bundle instead of whichever bundle is first.
func (w *viewState) failBindingFor(bundle string, e *core.ApiError) {
	if e == nil {
		return
	}
	if bundle != "" {
		w.failedBundle = bundle
	}
	w.failBinding(e)
}

// clearRecoveredBinding drops the stale bind stage and failure after a
// successful connection, so refreshed workspace progress decides the step again
// instead of the old failure being repeated forever.
func (w *viewState) clearRecoveredBinding() {
	w.engineFailureCleared = w.engineFailureKey()
	w.bindFailure = nil
	if w.task.stage == "bind" {
		w.task.stage = ""
	}
	w.task.failure = nil
	w.failure = nil
}

// managedOwnsBundle reports whether the running managed engine is the service
// for this bundle. Studio records the candidate it started, so a restart for the
// same candidate must re-verify the binding instead of trusting an older config;
// an engine started for another candidate belongs elsewhere and must not hijack
// this bundle.
func managedOwnsBundle(b inferyard.BundleProgress, state core.EngineState) bool {
	if state.State != "running" || state.Candidate == "" || b.CandidatePath == nil {
		return false
	}
	return state.Candidate == *b.CandidatePath
}

// executeExistingBundle reuses the workspace's own next command. A fresh run
// needs only the frozen binding when the CLI verified an external service; a
// managed engine is still required to resume or to (re)bind.
func executeExistingBundle(ctx context.Context, b inferyard.BundleProgress, workspace string, state core.EngineState, execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError)) *core.ApiError {
	if blockedPlan(b) || b.NextAction == "none" {
		return core.Err("invalid_request")
	}
	args, err := inferyard.SplitCommand(emptyText(b.NextCommand))
	if err != nil {
		return core.Err("invalid_request")
	}
	switch {
	case b.NextAction == "resume":
		if state.State != "running" {
			return core.Err("invalid_request")
		}
		args = setArg(args, "--endpoint-url", state.Endpoint)
		args = setArg(args, "--server-pid", strconv.FormatInt(state.Pid, 10))
	case managedOwnsBundle(b, state):
		// Studio's own engine is the service for this bundle (for example after a
		// restart), so the binding must be re-verified against it rather than
		// reusing a config that may name an earlier process.
		command, out, e := benchmarkBindCommand(workspace, b, state)
		if e != nil {
			return e
		}
		r, e := execute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return e
		}
		if r.ExitCode != 0 {
			return wizardFailure(r)
		}
		args = []string{"run", "--config", filepath.Join(out, "config.toml")}
	case b.NextAction == "run" && b.BoundPath != nil:
		// The binding already names the verified external service; run its frozen
		// config as-is. A managed engine that is not running belongs to no bundle,
		// or one that names a different candidate, must not rebind this one.
	default:
		return core.Err("invalid_request")
	}
	if ctx.Err() != nil {
		return core.Err("cancelled")
	}
	r, e := execute(ctx, core.WizardRequest{Command: commandOf(args)})
	if e != nil {
		return e
	}
	if r.ExitCode != 0 {
		return wizardFailure(r)
	}
	return nil
}

func prepareEnvironmentSteps(ctx context.Context, readiness func() core.Readiness, install func(context.Context) (string, *core.ApiError), provision func() *core.ApiError, execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError)) *core.ApiError {
	r := readiness()
	if r.CLI.Status != "ok" {
		if _, e := install(ctx); e != nil {
			return e
		}
		r = readiness()
	}
	if r.CLI.Status != "ok" {
		return core.Err("cli_unavailable")
	}
	if !r.Results.Exists || !r.Models.Exists {
		if e := provision(); e != nil {
			return e
		}
		r = readiness()
	}
	if !r.Workspace.Initialized {
		out := suggestInitDir(emptyText(r.Workspace.Path), r.Workspace.Exists, r.Workspace.Initialized)
		if pathError(out) != "" {
			return core.Err("invalid_request")
		}
		result, e := execute(ctx, core.WizardRequest{Command: commandOf([]string{"init", "--out", out})})
		if e != nil {
			return e
		}
		if result.ExitCode != 0 {
			return wizardFailure(result)
		}
		r = readiness()
	}
	if !r.Workspace.Initialized || !r.Results.Exists || !r.Models.Exists {
		return core.Err("settings_incomplete")
	}
	return nil
}
func (s *viewState) prepareEnvironment() {
	if !s.ready.Workspace.Initialized && s.ready.Workspace.Path != nil && pathError(*s.ready.Workspace.Path) != "" {
		s.submissionFailed(core.Err("invalid_request"))
		return
	}
	if s.simulate(previewPrepareEnvironment) {
		return
	}
	s.guideWork(func() (string, *core.ApiError) {
		if paths := s.service.BoundPaths(); paths != nil && !s.service.Readiness().Workspace.Initialized {
			result, e := s.service.WizardExecute(s.ctx, core.WizardRequest{Command: commandOf([]string{"init", "--out", paths.Workspace})})
			if e != nil {
				return "", e
			}
			if result.ExitCode != 0 {
				return "", wizardFailure(result)
			}
		}
		e := prepareEnvironmentSteps(s.ctx, s.service.Readiness, func(ctx context.Context) (string, *core.ApiError) {
			return s.service.InstallCLI(ctx, func(message string) { s.win.Update(func() { s.cliInstallStatus = message }) })
		}, func() *core.ApiError { _, e := s.service.Provision(); return e }, s.service.WizardExecute)
		return "环境已准备。", e
	})
}
func (s *viewState) checkWorkspaceDevice() {
	if s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.busy || s.ready.CLI.Status != "ok" || s.needsTaskAssets() {
		return
	}
	if s.simulate(previewCheckDevice) || s.service == nil {
		return
	}
	request, ok := s.takeWorkspaceDeviceRequest()
	if !ok {
		return
	}
	s.guideWork(func() (string, *core.ApiError) {
		r, e := s.service.WizardExecute(s.ctx, request)
		if e != nil {
			return "", e
		}
		if missingModelResult(r) {
			return "请先准备本地模型，再继续测评。", nil
		}
		if r.ExitCode != 0 {
			return "", wizardFailure(r)
		}
		return "设备检查完成。", nil
	})
}
func (s *viewState) taskBundles() []inferyard.BundleProgress {
	if s.progress == nil {
		return nil
	}
	if len(s.task.bundles) == 0 {
		return s.progress.Bundles
	}
	return selectedProgress(s.progress, s.task.bundles).Bundles
}

func (s *viewState) takeWorkspaceDeviceRequest() (core.WizardRequest, bool) {
	if s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.deviceChecked == s.progress.Workspace {
		return core.WizardRequest{}, false
	}
	s.deviceChecked = s.progress.Workspace
	return core.WizardRequest{Command: s.nextCommand(), ModelName: ""}, true
}
