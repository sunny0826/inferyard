package pages

import (
	"context"
	"errors"
	"fmt"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
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

func (w *viewState) execute(command string, report bool) {
	w.autoBind = bindingArm{}
	model, engine := w.model, w.engine
	w.guideWork(func() (string, *core.ApiError) {
		r, e := w.service.WizardExecute(w.ctx, core.WizardRequest{Command: command, ReportVerify: report, ModelName: model, EngineName: engine})
		if e != nil {
			return "", e
		}
		if r.ExitCode != 0 {
			return "", wizardFailure(r)
		}
		return executeMessage(r), nil
	})
}

// executeMessage separates queued runs (work has just started) from commands
// that are already done when they return.
func executeMessage(r core.WizardResult) string {
	if len(r.Tasks) > 0 {
		return "运行已开始，封存后进入下一步。"
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
		return "", nil
	}
	b := w.active()
	if b == nil || w.progress == nil {
		return "", core.Err("invalid_request")
	}
	var args []string
	if b.Phase == "bound_missing" {
		var err error
		args, err = inferyard.SplitCommand(emptyText(b.NextCommand))
		if err != nil {
			args = w.baseCommand()
		}
	} else {
		out, err := newBoundOutput(w.progress.Workspace, b.Bundle)
		if err != nil {
			var api *core.ApiError
			if errors.As(err, &api) {
				return "", api
			}
			return "", core.Err("internal_error")
		}
		candidate := w.candidate
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			candidate = filepath.Join(candidate, "candidate.toml")
		}
		args = []string{"config", "bind", "--candidate", candidate, "--out", out}
	}
	if len(args) < 2 || args[0] != "config" || args[1] != "bind" {
		return "", core.Err("invalid_request")
	}
	args = setArg(args, "--pid", pid)
	args = setArg(args, "--endpoint", endpoint)
	return commandOf(args), nil
}

func newBoundOutput(workspace, bundle string) (string, error) {
	if !validName(bundle) {
		return "", os.ErrInvalid
	}
	for n := 1; n <= 1000; n++ {
		path := filepath.Join(workspace, "bound", fmt.Sprintf("%s-%d", bundle, n))
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", core.Err("output_name_exhausted")
}

func (w *viewState) startEngine() {
	if w.previewAction() {
		return
	}
	if w.task.stage == "bind" && w.task.start.candidate != "" {
		w.retryBenchmark()
		return
	}
	if w.busy || w.running() != nil || w.candidate == "" {
		return
	}
	candidate := w.candidate
	b := w.active()
	if b == nil {
		return
	}
	bundle, workspace := *b, w.progress.Workspace
	ctx, cancel := context.WithCancel(w.ctx)
	w.taskCancel = cancel
	w.autoBind = bindingArm{}
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
	w.autoBind = bindingArm{}
	w.guideWork(func() (string, *core.ApiError) {
		_, e := w.service.StopEngine()
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

func (w *viewState) executeBinding(command string, e *core.ApiError) {
	if w.previewAction() {
		return
	}
	if e != nil {
		w.submissionFailed(e)
		return
	}
	if w.busy {
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.taskCancel = cancel
	w.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		r, e := w.service.WizardExecute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return "", e
		}
		if r.ExitCode != 0 {
			return "", wizardFailure(r)
		}
		return "绑定完成。", nil
	})
}

func executeExistingBundle(ctx context.Context, b inferyard.BundleProgress, workspace string, state core.EngineState, execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError)) *core.ApiError {
	if blockedPlan(b) || b.NextAction == "none" || state.State != "running" {
		return core.Err("invalid_request")
	}
	args, err := inferyard.SplitCommand(emptyText(b.NextCommand))
	if err != nil {
		return core.Err("invalid_request")
	}
	if b.NextAction == "resume" {
		args = setArg(args, "--endpoint-url", state.Endpoint)
		args = setArg(args, "--server-pid", strconv.FormatInt(state.Pid, 10))
	} else {
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
	s.guideWork(func() (string, *core.ApiError) {
		e := prepareEnvironmentSteps(s.ctx, s.service.Readiness, func(ctx context.Context) (string, *core.ApiError) {
			return s.service.InstallCLI(ctx, func(message string) { s.win.Update(func() { s.cliInstallStatus = message }) })
		}, func() *core.ApiError { _, e := s.service.Provision(); return e }, s.service.WizardExecute)
		return "环境已准备。", e
	})
}
func (s *viewState) checkWorkspaceDevice() {
	if s.progress == nil || s.progress.DevicePreflight.Status != "missing" || s.busy || s.service == nil || s.ready.CLI.Status != "ok" {
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
