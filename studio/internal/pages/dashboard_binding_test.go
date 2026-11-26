package pages

import (
	"context"

	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

type taskBackendFixture struct {
	failBind     bool
	failBindExit bool
	failBundle   string
	p            *inferyard.WorkspaceProgress
	requests     []core.WizardRequest
	runRequest   core.StartRunsRequest
	tasks        []core.RunTask
}

func (f *taskBackendFixture) StartEngine(string) (core.EngineState, *core.ApiError) {
	return f.Engine(), nil
}
func (f *taskBackendFixture) Engine() core.EngineState {
	return core.EngineState{State: "running", Pid: 71, Endpoint: "http://127.0.0.1:8080"}
}
func (f *taskBackendFixture) Progress(context.Context) (core.ProgressResponse, *core.ApiError) {
	return core.ProgressResponse{Progress: f.p}, nil
}
func (f *taskBackendFixture) WizardExecute(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
	f.requests = append(f.requests, q)
	if f.failBind && strings.Contains(q.Command, "zh-svg-pelican") {
		f.failBind = false
		return core.WizardResult{}, core.Err("cli_error")
	}
	if f.failBindExit && strings.Contains(q.Command, emptyText(&f.failBundle)) {
		f.failBindExit = false
		return core.WizardResult{ExitCode: 2, Status: "blocked"}, nil
	}
	return core.WizardResult{}, nil
}
func (f *taskBackendFixture) StartRuns(q core.StartRunsRequest) ([]core.RunTask, *core.ApiError) {
	f.runRequest = q
	return f.tasks, nil
}
func (f *taskBackendFixture) RunTasks() []core.RunTask { return f.tasks }

func TestDashboardSelectedRunReportIgnoresUnfinishedPelican(t *testing.T) {
	root := t.TempDir()
	run := root + "/results/new-run"
	f := &taskBackendFixture{p: &inferyard.WorkspaceProgress{Workspace: root, NextAction: "prepare_candidate", Report: inferyard.ProgressReport{Status: "missing"}, Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_complete", CandidatePath: ptr(root + "/candidate"), Runs: []inferyard.ProgressRun{{Path: run, Sealed: true}}},
		{Bundle: "zh-svg-pelican", Phase: "candidate_missing"},
	}}, tasks: []core.RunTask{{TaskId: "new", State: "done", RunPath: &run}}}
	s := testState()
	s.task.stage = "bind"
	e := runBenchmarkFlow(context.Background(), f, startChainResult{workspace: root, candidate: root + "/candidate"}, []string{"zh-core"}, nil, func(stage string) { s.task.stage = stage }, func([]string) {}, func(paths []string, out string) { s.task.runs = paths; s.task.report = out; s.task.stage = "done" })
	if e != nil || len(f.requests) != 2 || len(f.runRequest.ConfigPaths) != 1 || f.runRequest.AllowSingleBundle == nil || !*f.runRequest.AllowSingleBundle {
		t.Fatal(e, f.requests, f.runRequest)
	}
	report := f.requests[1]
	args, err := inferyard.SplitCommand(report.Command)
	if err != nil || args[0] != "report" || args[1] != "--runs" || args[2] != run {
		t.Fatal(report)
	}
	if strings.Contains(report.Command, "pelican") || strings.Contains(f.requests[0].Command, "pelican") {
		t.Fatal(f.requests)
	}
	tt := ui.NewTester(s.stageView, 1120, 1000)
	if !tt.HasText("查看报告") || tt.HasText(s.task.report) || tt.HasText("已核验") || tt.HasText("missing") {
		t.Fatal(tt.Texts())
	}
	f.p.Bundles[0].Runs[0].Sealed = false
	f.requests = nil
	e = runBenchmarkFlow(context.Background(), f, startChainResult{workspace: root}, []string{"zh-core"}, nil, func(string) {}, func([]string) {}, func([]string, string) { t.Fatal("unsealed report") })
	if e == nil || e.Code != "run_incomplete" || len(f.requests) != 1 {
		t.Fatal(e, f.requests)
	}
}

func TestDashboardRetryReplacesFailedTaskNotice(t *testing.T) {
	s := readyState("benchmarks")
	s.task.stage = "run"
	failed := core.RunTask{TaskId: "old", State: "failed", Bundle: ptr("zh-core"), CreatedAt: "2026-01-01T00:00:00Z", Error: core.Err("cli_blocked")}
	s.tasks = []core.RunTask{failed}
	s.task.failure = failed.Error
	s.task.start.localCandidates = []core.CandidateRun{{Bundle: "zh-core"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.dashboardView(c) }) }, 1120, 1200)
	if !tt.HasText(failed.Error.Message) || !tt.HasText(l10n.TR("dashboard.retry.button")) {
		t.Fatalf("failed attempt lost diagnosis or retry action: %v", tt.Texts())
	}
	s.task.failure = nil
	retry := core.RunTask{TaskId: "retry", State: "starting", AwaitingService: true, Bundle: failed.Bundle, CreatedAt: "2026-01-01T00:00:01Z"}
	s.tasks = append(s.tasks, retry)
	for _, stage := range []string{"awaiting_service", "binding", "running", "done"} {
		s.tasks[1].PreparationStage = stage
		s.tasks[1].AwaitingService = stage == "awaiting_service"
		if stage == "running" || stage == "done" {
			s.tasks[1].State = stage
		}
		tt.Frame()
		if tt.HasText(failed.Error.Message) || tt.HasText(taskLabel(failed)) || tt.HasText(l10n.TR("dashboard.retry.button")) {
			t.Fatalf("%s shows previous failure: %v", stage, tt.Texts())
		}
	}
	s.tasks[0], s.tasks[1] = s.tasks[1], s.tasks[0]
	tt.Frame()
	if tt.HasText(failed.Error.Message) {
		t.Fatal("newest-first task snapshot revived the previous failure")
	}
	s.tasks[0].CreatedAt = "2025-12-31T23:59:59Z"
	tt.Frame()
	if !tt.HasText(failed.Error.Message) {
		t.Fatal("an earlier success hid a newer undiagnosed failure")
	}
}
