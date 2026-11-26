package pages

import (
	"context"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

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
func activeRunTask(tasks []core.RunTask) *core.RunTask {
	var fallback *core.RunTask
	for i := range tasks {
		if terminal(tasks[i].State) {
			continue
		}
		if !tasks[i].Queued {
			return &tasks[i]
		}
		fallback = &tasks[i]
	}
	return fallback
}

func (w *viewState) running() *core.RunTask {
	return activeRunTask(w.tasks)
}

func executeMessage(r core.WizardResult) string {
	if len(r.Tasks) > 0 {
		return l10n.TR("dashboard.toast.run_started")
	}
	return l10n.TR("dashboard.toast.action_done")
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

func (w *viewState) managedBindCommand() (string, *core.ApiError) {
	if w.engineState.State != "running" {
		return "", core.Err("invalid_request")
	}
	return w.bindCommand(strconv.FormatInt(w.engineState.Pid, 10), w.engineState.Endpoint)
}

func (w *viewState) bindCommand(pid, endpoint string) (string, *core.ApiError) {
	if w.preview != nil {
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
			return l10n.TR("dashboard.toast.service_ready"), nil
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
		return l10n.TR("dashboard.toast.service_started_bound"), nil
	})
}
func (w *viewState) stopEngine() {
	if w.simulate(previewStopEngine) {
		return
	}
	w.guideWork(func() (string, *core.ApiError) {
		_, e := w.stopEngineCall()
		return l10n.TR("dashboard.toast.stop_requested"), e
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

func (w *viewState) executeBinding(command string, e *core.ApiError) {
	w.executeBindingFor(w.bindTargetBundle(), command, e)
}

func (w *viewState) bindTargetBundle() string {
	if b := w.bindTarget(); b != nil {
		return b.Bundle
	}
	return ""
}

func (w *viewState) executeBindingFor(target, command string, e *core.ApiError) {
	if w.preview != nil {
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
		return l10n.TR("dashboard.toast.bind_done"), nil
	})
}

func (w *viewState) updateView(fn func()) {
	if w.updateWindow != nil {
		w.updateWindow(fn)
		return
	}
	if w.win == nil {
		fn()
		return
	}
	w.win.Update(fn)
}

func (w *viewState) stepConnection() {
	w.bindFailure, w.failedBundle = nil, ""
}

func (w *viewState) beginStart() bool {
	if w.busy || w.running() != nil || w.candidate == "" || w.active() == nil {
		return false
	}
	w.beginAttempt()
	return true
}

func (w *viewState) beginAttempt() {
	if w.bindFailure != nil && w.failure != nil && w.failure.Code == w.bindFailure.Code {
		w.failure = nil
	}
	w.bindFailure, w.failedBundle, w.handoffFailure = nil, "", nil
}

func (w *viewState) failBinding(e *core.ApiError) {
	if e == nil {
		return
	}
	w.bindFailure = e
	w.failure = e
}

func (w *viewState) failBindingFor(bundle string, e *core.ApiError) {
	if e == nil {
		return
	}
	if bundle != "" {
		w.failedBundle = bundle
	}
	w.failBinding(e)
}

func (w *viewState) clearRecoveredBinding() {
	w.engineFailureCleared = w.engineFailureKey()
	w.bindFailure = nil
	if w.task.stage == "bind" {
		w.task.stage = ""
	}
	w.task.failure = nil
	w.failure = nil
}

func managedOwnsBundle(b inferyard.BundleProgress, state core.EngineState) bool {
	if state.State != "running" || state.Candidate == "" || b.CandidatePath == nil {
		return false
	}
	return state.Candidate == *b.CandidatePath
}

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
	if r.CLI.Status == "outdated" {
		return core.Err("cli_outdated")
	}
	if r.CLI.Status != "ok" {
		return core.Err("cli_unavailable")
	}
	if !r.Workspace.Initialized {
		out := emptyText(r.Workspace.Path)
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
	if !r.Results.Exists || !r.Models.Exists {
		if e := provision(); e != nil {
			return e
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
	s.cliInstallStatus = ""
	s.guideWorkAs("prepare-env", func() (string, *core.ApiError) {
		defer s.updateView(func() { s.cliInstallStatus = "" })
		if paths := s.service.BoundPaths(); paths != nil && !s.service.Readiness().Workspace.Initialized {
			result, e := s.service.WizardExecute(s.ctx, core.WizardRequest{Command: commandOf([]string{"init", "--out", paths.Workspace})})
			if e != nil {
				return "", e
			}
			if result.ExitCode != 0 {
				return "", wizardFailure(result)
			}
		}
		upgradedOutdated := false
		e := prepareEnvironmentSteps(s.ctx, s.service.Readiness, func(ctx context.Context) (string, *core.ApiError) {
			if s.service.Readiness().CLI.Status == "outdated" {
				upgradedOutdated = true
				return s.service.UpgradeCLI(ctx, func(message string) { s.updateView(func() { s.cliInstallStatus = message }) })
			}
			return s.service.InstallCLI(ctx, func(message string) { s.updateView(func() { s.cliInstallStatus = message }) })
		}, func() *core.ApiError { _, e := s.service.Provision(); return e }, s.service.WizardExecute)
		if upgradedOutdated {
			st := s.service.CLIUpgradeStatus()
			s.updateView(func() { s.applyCLIUpgradeState(st) })
			if e != nil && st.State == core.CLIUpgradeFailed && st.Error != nil {
				return "", nil
			}
		}
		if e == nil && s.service != nil {
			_ = s.service.RefreshAssets()
			_ = s.service.RefreshDevice()
		}
		return l10n.TR("dashboard.toast.env_ready"), e
	})
}
