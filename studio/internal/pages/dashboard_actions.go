package pages

import (
	"errors"
	"fmt"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
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

func (w *viewState) installCLI() {
	w.guideWork(func() (string, *core.ApiError) {
		defer w.win.Update(func() { w.cliInstallStatus = "" })
		path, e := w.service.InstallCLI(w.ctx, func(status string) {
			w.win.Update(func() { w.cliInstallStatus = status })
		})
		if e != nil {
			return "", e
		}
		return "CLI 安装完成：" + displayName(path), nil
	})
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
	if w.busy || w.running() != nil || w.candidate == "" {
		return
	}
	candidate := w.candidate
	w.autoBind = bindingArm{}
	message := "服务启动中，等待健康检查。"
	if w.bindingPhase() {
		w.autoBind = bindingArm{armed: true, candidate: candidate, workspace: w.progress.Workspace}
		message = "服务启动中，健康通过后依次绑定全部待绑题包。"
	}
	w.guideWork(func() (string, *core.ApiError) {
		state, e := w.service.StartEngine(candidate)
		w.win.Update(func() {
			if e != nil {
				w.autoBind = bindingArm{}
			} else {
				w.autoBind.pid = state.Pid
				w.engineState = state
			}
		})
		return message, e
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

func uniqueID() int64 { return time.Now().UnixNano() }

func (w *viewState) executeBinding(command string, e *core.ApiError) {
	if e != nil {
		w.submissionFailed(e)
		return
	}
	w.execute(command, false)
}
