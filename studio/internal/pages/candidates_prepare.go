package pages

import (
	"context"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"slices"
	"strconv"
)

// prepareCombination is shared by Dashboard candidate creation and Models startup.
func prepareCombination(ctx context.Context, service *core.Service, model, engine string, selection ...[]string) (startChainResult, *core.ApiError) {
	if model == "" {
		current, e := service.Progress(ctx)
		if e != nil {
			return startChainResult{}, e
		}
		if current.ExitCode != 0 || current.Progress == nil || current.Progress.DevicePreflight.Path == nil {
			return startChainResult{}, core.Err("preflight_missing")
		}
		preflight := *current.Progress.DevicePreflight.Path
		for _, asset := range service.Registry().Models {
			if !asset.Available {
				continue
			}
			match, e := service.PreflightMatchesModel(preflight, asset.Name)
			if e != nil {
				return startChainResult{}, e
			}
			if match {
				model = asset.Name
				break
			}
		}
		if model == "" {
			return startChainResult{}, core.Err("device_preflight_blocked")
		}
	}
	progress := service.Progress
	if len(selection) > 0 {
		progress = func(ctx context.Context) (core.ProgressResponse, *core.ApiError) {
			r, e := service.Progress(ctx)
			if e != nil {
				return r, e
			}
			r.Progress = selectedProgress(r.Progress, selection[0])
			return r, nil
		}
	}
	return runStartChain(ctx, model, engine, progress, service.WizardExecute,
		service.PreflightMatchesModel, service.CandidateMatchesCombination, len(selection) > 0)
}

func (s *viewState) startBenchmark() {
	names := s.checkedBundles()
	if s.environmentMissing() || !s.preflightReady() || s.busy || len(names) == 0 || s.model == "" || s.engine == "" || s.engineState.State == "orphaned" {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.taskCancel = cancel
	s.task = benchmarkTask{stage: "prepare_candidate", bundles: names, bindings: &benchmarkBindings{}}
	model, engine := s.model, s.engine
	s.autoBind = bindingArm{}
	s.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		announce := func(stage string) { s.win.Update(func() { s.task.stage = stage }) }
		result, e := prepareCombination(ctx, s.service, model, engine, names)
		if e == nil {
			s.win.Update(func() { s.task.start = result })
			e = s.runBenchmark(ctx, result, names, announce)
		}
		s.win.Update(func() { s.task.failure = e })
		return "任务报告已生成。", e
	})
}

type benchmarkBindings struct {
	pid        int64
	configs    map[string]string
	candidates map[string]string
}

type benchmarkBackend interface {
	StartEngine(string) (core.EngineState, *core.ApiError)
	Engine() core.EngineState
	Progress(context.Context) (core.ProgressResponse, *core.ApiError)
	WizardExecute(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError)
	StartRuns(core.StartRunsRequest) ([]core.RunTask, *core.ApiError)
	RunTasks() []core.RunTask
}

func (s *viewState) runBenchmark(ctx context.Context, result startChainResult, names []string, announce func(string)) *core.ApiError {
	return runBenchmarkFlow(ctx, s.service, result, names, s.task.bindings, announce,
		func(ids []string) { s.win.Update(func() { s.groups = append(s.groups, ids) }) },
		func(paths []string, out string) {
			s.win.Update(func() { s.task.runs = paths; s.task.report = out; s.task.stage = "done" })
		})
}

func runBenchmarkFlow(ctx context.Context, backend benchmarkBackend, result startChainResult, names []string, bindings *benchmarkBindings, announce func(string), queued func([]string), report func([]string, string)) *core.ApiError {
	if ctx.Err() != nil {
		return core.Err("cancelled")
	}
	announce("bind")
	state, e := backend.StartEngine(result.candidate)
	if e != nil {
		return e
	}
	for state.State == "starting" || state.State == "stopping" {
		if !taskWait(ctx) {
			return core.Err("cancelled")
		}
		state = backend.Engine()
	}
	if state.State != "running" {
		if state.Error != nil {
			return state.Error
		}
		return core.Err("engine_start_failed")
	}
	if ctx.Err() != nil {
		return core.Err("cancelled")
	}
	current, e := backend.Progress(ctx)
	if e != nil {
		return e
	}
	if current.ExitCode != 0 {
		return core.Err("cli_error")
	}
	p := selectedProgress(current.Progress, names)
	if p == nil || len(p.Bundles) != len(names) {
		return core.Err("binding_progress_changed")
	}
	if bindings == nil {
		bindings = &benchmarkBindings{}
	}
	if bindings.pid != state.Pid {
		bindings.pid = state.Pid
		bindings.configs = map[string]string{}
		bindings.candidates = map[string]string{}
	}
	var configs []string
	for _, b := range p.Bundles {
		if ctx.Err() != nil {
			return core.Err("cancelled")
		}
		if config := bindings.configs[b.Bundle]; config != "" && bindings.candidates[b.Bundle] == emptyText(b.CandidatePath) {
			configs = append(configs, config)
			continue
		}
		command, out, e := benchmarkBindCommand(p.Workspace, b, state)
		if e != nil {
			return e
		}
		r, e := backend.WizardExecute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return e
		}
		if r.ExitCode != 0 {
			return wizardFailure(r)
		}
		bindings.configs[b.Bundle] = filepath.Join(out, "config.toml")
		bindings.candidates[b.Bundle] = emptyText(b.CandidatePath)
		configs = append(configs, bindings.configs[b.Bundle])
	}
	if ctx.Err() != nil {
		return core.Err("cancelled")
	}
	announce("run")
	tasks, e := backend.StartRuns(core.StartRunsRequest{ConfigPaths: configs, AllowSingleBundle: boolPointer(len(configs) == 1)})
	if e != nil {
		return e
	}
	ids := make(map[string]bool)
	var group []string
	for _, task := range tasks {
		ids[task.TaskId] = true
		group = append(group, task.TaskId)
	}
	queued(group)
	var paths []string
	for {
		paths = nil
		remaining := len(ids)
		for _, task := range backend.RunTasks() {
			if !ids[task.TaskId] {
				continue
			}
			if terminal(task.State) {
				if task.Error != nil {
					return task.Error
				}
				if task.State != "done" || emptyText(task.RunPath) == "" {
					return core.Err("run_incomplete")
				}
				paths = append(paths, *task.RunPath)
				remaining--
			}
		}
		if remaining == 0 {
			break
		}
		if !taskWait(ctx) {
			return core.Err("cancelled")
		}
	}
	current, e = backend.Progress(ctx)
	if e != nil {
		return e
	}
	if current.ExitCode != 0 || !selectedRunsSealed(current.Progress, names, paths) {
		return core.Err("run_incomplete")
	}
	announce("report")
	out, e := newNumberedOutput(result.workspace, "reports", "summary")
	if e != nil {
		return e
	}
	args := append([]string{"report", "--runs"}, paths...)
	args = append(args, "--out", out)
	r, e := backend.WizardExecute(ctx, core.WizardRequest{Command: commandOf(args), ReportVerify: false})
	if e != nil {
		return e
	}
	if r.ExitCode != 0 {
		return wizardFailure(r)
	}
	report(paths, out)
	return nil
}

func boolPointer(v bool) *bool { return &v }

func selectedProgress(p *inferyard.WorkspaceProgress, names []string) *inferyard.WorkspaceProgress {
	if p == nil {
		return nil
	}
	copy := *p
	copy.Bundles = nil
	for _, b := range p.Bundles {
		if slices.Contains(names, b.Bundle) {
			copy.Bundles = append(copy.Bundles, b)
		}
	}
	return &copy
}

func selectedRunsSealed(p *inferyard.WorkspaceProgress, names, paths []string) bool {
	p = selectedProgress(p, names)
	if p == nil || len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		sealed := false
		for _, b := range p.Bundles {
			for _, r := range b.Runs {
				if r.Path == path && r.Sealed {
					sealed = true
				}
			}
		}
		if !sealed {
			return false
		}
	}
	return true
}

func benchmarkBindCommand(workspace string, b inferyard.BundleProgress, state core.EngineState) (string, string, *core.ApiError) {
	if b.CandidatePath == nil || state.Pid <= 0 || state.Endpoint == "" {
		return "", "", core.Err("invalid_request")
	}
	out, e := newNumberedOutput(workspace, "bound", b.Bundle)
	if e != nil {
		return "", "", e
	}
	candidate := *b.CandidatePath
	if filepath.Ext(candidate) != ".toml" {
		candidate = filepath.Join(candidate, "candidate.toml")
	}
	return commandOf([]string{"config", "bind", "--candidate", candidate, "--pid", strconv.FormatInt(state.Pid, 10), "--endpoint", state.Endpoint, "--out", out}), out, nil
}

func (s *viewState) retryBenchmark() {
	if s.busy || s.task.stage != "bind" || s.task.failure == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.taskCancel = cancel
	result, names := s.task.start, s.task.bundles
	s.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		e := s.runBenchmark(ctx, result, names, func(stage string) { s.win.Update(func() { s.task.stage = stage }) })
		s.win.Update(func() { s.task.failure = e })
		return "任务报告已生成。", e
	})
}
