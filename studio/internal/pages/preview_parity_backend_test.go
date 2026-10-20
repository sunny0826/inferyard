//go:build ui_debug && !release

package pages

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type parityBackend struct {
	t         *testing.T
	state     *viewState
	failStart bool
	starts    int
	configs   map[string]string
	report    []string
	onStart   func()
	onNextRun func()
}

func runProductionParity(t *testing.T, scenario parityScenario) []parityFrame {
	t.Helper()
	root := t.TempDir()
	s := testState()
	s.ctx = context.Background()
	s.workspace, s.results = root, filepath.Join(root, "results")
	s.discoveryLoaded = true
	s.progress = &inferyard.WorkspaceProgress{Workspace: root}
	for _, bundle := range []string{"zh-core", "zh-svg-pelican"} {
		s.progress.Bundles = append(s.progress.Bundles, inferyard.BundleProgress{
			Bundle: bundle, Phase: "bound_missing", NextAction: "bind", CandidatePath: ptr(filepath.Join(root, "candidates", bundle)),
		})
	}
	s.tasks = []core.RunTask{{TaskId: "unrelated", State: "done", Bundle: ptr("unrelated"), RunPath: ptr("/unrelated")}}
	s.task = benchmarkTask{stage: "bind", bundles: slices.Clone(scenario.bundles), bindings: &benchmarkBindings{}, start: startChainResult{workspace: root, candidate: *s.progress.Bundles[0].CandidatePath}}
	b := &parityBackend{t: t, state: &s, failStart: scenario.failStart, configs: map[string]string{}}
	var frames []parityFrame
	b.onNextRun = func() { frames = append(frames, observeParity(&s, "next_run", nil)) }
	run := func() *core.ApiError {
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		s.busy, s.failure, s.task.failure = true, nil, nil
		b.onStart = func() {
			frames = append(frames, observeParity(&s, "bind", nil))
			if scenario.cancelAt == "bind" {
				cancel()
			}
		}
		e := runBenchmarkFlow(ctx, b, s.task.start, s.task.bundles, s.task.bindings,
			func(stage string) {
				s.task.stage = stage
				if stage == "report" {
					frames = append(frames, observeParity(&s, "report", nil))
				}
			},
			func([]string) {
				frames = append(frames, observeParity(&s, "run", nil))
				if scenario.cancelAt == "run" {
					cancel()
					if e := cancelBenchmarkRuns(s.ctx, func() []core.RunTask { return s.tasks }, b.cancelRun); e != nil {
						t.Fatal(e)
					}
				}
			},
			func(paths []string, out string) {
				s.task.runs, s.task.report, s.task.stage = paths, out, "done"
			})
		s.busy, s.failure, s.task.failure = false, e, e
		return e
	}
	e := run()
	if scenario.cancelAt != "" {
		if e == nil || s.task.report != "" || len(b.report) != 0 {
			t.Fatal("cancelled production flow succeeded or generated a report", e)
		}
		return append(frames, observeParity(&s, "cancelled", nil))
	}
	if scenario.failStart {
		if e == nil || b.starts != 1 || len(b.configs) != 0 || len(b.report) != 0 {
			t.Fatal("failed production startup continued or retried", e, b)
		}
		frames = append(frames, observeParity(&s, "failed", nil))
		e = run()
	}
	if e != nil {
		t.Fatal(e)
	}
	return append(frames, observeParity(&s, "done", b.report))
}

func (b *parityBackend) StartEngine(candidate string) (core.EngineState, *core.ApiError) {
	b.starts++
	b.state.engineState = core.EngineState{State: "starting", Pid: 71, Endpoint: "http://127.0.0.1:48857", Candidate: candidate}
	b.onStart()
	return b.state.engineState, nil
}

func (b *parityBackend) Engine() core.EngineState {
	s := b.state
	s.engineState.State, s.engineState.Error = "running", nil
	if b.failStart {
		b.failStart = false
		s.engineState.State, s.engineState.Error = "failed", core.Err("engine_start_failed")
	}
	return s.engineState
}

func (b *parityBackend) Progress(context.Context) (core.ProgressResponse, *core.ApiError) {
	return core.ProgressResponse{Progress: b.state.progress}, nil
}

func (b *parityBackend) WizardExecute(_ context.Context, request core.WizardRequest) (core.WizardResult, *core.ApiError) {
	args, err := inferyard.SplitCommand(request.Command)
	if err != nil {
		b.t.Fatal(err)
	}
	value := func(key string) string {
		i := slices.Index(args, key)
		if i < 0 || i+1 >= len(args) {
			b.t.Fatalf("missing %s in %v", key, args)
		}
		return args[i+1]
	}
	switch args[0] {
	case "config":
		bundle := filepath.Base(filepath.Dir(value("--candidate")))
		b.configs[filepath.Join(value("--out"), "config.toml")] = bundle
	case "report":
		if request.ReportVerify {
			b.t.Fatal("task reports must not verify implicitly")
		}
		paths := args[slices.Index(args, "--runs")+1 : slices.Index(args, "--out")]
		for _, path := range paths {
			found := false
			for _, task := range b.state.tasks {
				if emptyText(task.RunPath) == path {
					b.report = append(b.report, emptyText(task.Bundle))
					found = true
				}
			}
			if !found {
				b.t.Fatal("report requested an unknown run", path)
			}
		}
	default:
		b.t.Fatal("unexpected service command", args)
	}
	return core.WizardResult{}, nil
}

func (b *parityBackend) StartRuns(request core.StartRunsRequest) ([]core.RunTask, *core.ApiError) {
	if request.AllowSingleBundle == nil || *request.AllowSingleBundle != (len(request.ConfigPaths) == 1) {
		b.t.Fatal("wrong single-bundle declaration", request)
	}
	var tasks []core.RunTask
	for i, config := range request.ConfigPaths {
		bundle, ok := b.configs[config]
		if !ok {
			b.t.Fatal("run requested an unbound config", config)
		}
		state := "starting"
		if i == 0 {
			state = "running"
		}
		tasks = append(tasks, core.RunTask{TaskId: bundle, Kind: "run", State: state, Queued: i > 0, Bundle: ptr(bundle), RunPath: ptr(filepath.Join(b.state.results, bundle))})
	}
	b.state.tasks = append(b.state.tasks, tasks...)
	for i := range b.state.progress.Bundles {
		bundle := &b.state.progress.Bundles[i]
		if slices.Contains(b.state.task.bundles, bundle.Bundle) {
			bundle.Phase = "run_incomplete"
			bundle.BlockedReason = ptr("single_run_resume_not_supported")
		}
	}
	return tasks, nil
}

func (b *parityBackend) RunTasks() []core.RunTask {
	completed := false
	for i := range b.state.tasks {
		task := &b.state.tasks[i]
		if terminal(task.State) {
			continue
		}
		if completed {
			task.State, task.Queued = "running", false
			b.onNextRun()
			break
		}
		task.State, task.Queued = "done", false
		completed = true
		for j := range b.state.progress.Bundles {
			bundle := &b.state.progress.Bundles[j]
			if bundle.Bundle == emptyText(task.Bundle) {
				bundle.Runs = []inferyard.ProgressRun{{Path: *task.RunPath, Sealed: true}}
				bundle.Phase, bundle.BlockedReason = "run_complete", nil
			}
		}
	}
	return b.state.tasks
}

func (b *parityBackend) cancelRun(id string) (core.RunTask, *core.ApiError) {
	for i := range b.state.tasks {
		if b.state.tasks[i].TaskId == id {
			b.state.tasks[i].State = "cancelled"
			return b.state.tasks[i], nil
		}
	}
	return core.RunTask{}, core.Err("not_found")
}
