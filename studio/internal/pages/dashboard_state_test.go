package pages

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestGuidedCommandsPreserveOutputAndResume(t *testing.T) {
	s := testState()
	s.engine = "/engine path/it's server"
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", NextCommand: ptr("inferyard config create --out '/frozen/new output' --engine old")}
	args, err := inferyard.SplitCommand(s.nextCommand())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, []string{"config", "create", "--out", "/frozen/new output", "--engine", s.engine}) {
		t.Fatal(args)
	}
	s.progress.NextAction = "device_check"
	s.progress.NextCommand = ptr("inferyard device-check --out /frozen/device")
	s.ready.Models.Path = ptr("")
	if strings.Contains(s.nextCommand(), "--models-root") {
		t.Fatal("empty models root injected")
	}
	s.ready.Models.Path = ptr("/local models")
	args, err = inferyard.SplitCommand(s.nextCommand())
	if err != nil || !slices.Equal(args, []string{"device-check", "--out", "/frozen/device", "--models-root", "/local models"}) {
		t.Fatal(args, err)
	}
	s.progress.NextAction = "run"
	s.progress.NextCommand = ptr("inferyard run --out /wrong")
	s.progress.Bundles = []inferyard.BundleProgress{{Phase: "run_incomplete", NextAction: "resume", NextCommand: ptr("inferyard resume --from /previous --out /resume-output")}}
	args, err = inferyard.SplitCommand(s.nextCommand())
	if err != nil || !slices.Equal(args, []string{"resume", "--from", "/previous", "--out", "/resume-output"}) {
		t.Fatal(args, err)
	}
}

func TestDashboardRefreshClearsOnlyRefreshFailure(t *testing.T) {
	s := testState()
	s.failure = &core.ApiError{Code: "invalid_request", Message: human("invalid_request")}
	snapshot := dashboardSnapshot{candidate: "/candidate/one", startup: core.Startup{Args: []string{"--host", "::1", "--port", "48857"}}}
	s.applyDashboard(snapshot, core.Err("cli_error"))
	if s.refreshFailure != human("cli_error") || s.bindEndpoint != "http://[::1]:48857" {
		t.Fatalf("failure=%q endpoint=%q", s.refreshFailure, s.bindEndpoint)
	}
	s.process, s.bindEndpoint, s.endpoint = "12 · custom", "http://127.0.0.1:1234", "http://127.0.0.1:9999"
	s.applyDashboard(snapshot, nil)
	if s.refreshFailure != "" || s.failure == nil || s.process != "12 · custom" || s.bindEndpoint != "http://127.0.0.1:1234" {
		t.Fatal("refresh discarded edits or operation error")
	}
	snapshot.candidate = "/candidate/two"
	s.applyDashboard(snapshot, nil)
	if s.process != "" || s.bindEndpoint != "http://[::1]:48857" || s.endpoint != "http://127.0.0.1:9999" {
		t.Fatal("candidate transition mixed binding and resume inputs")
	}
	s.failure = nil
	tt := ui.NewTester(s.view, 1120, 1600)
	if tt.HasText(human("cli_error")) {
		t.Fatal("recovered refresh failure still visible")
	}
}

func TestAssetDefaultsApplyOnlyOnce(t *testing.T) {
	s := testState()
	s.engine = "/manual/engine"
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != core.DefaultModelURL() || s.engineURL != core.DefaultEngineURL() || s.engine != "/manual/engine" {
		t.Fatal("initial defaults")
	}
	s.modelURL, s.engineURL, s.engine = "", "", ""
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != "" || s.engineURL != "" || s.engine != "" {
		t.Fatal("refresh overwrote manual clearing")
	}
	s = testState()
	s.modelURL, s.engineURL, s.engine = "manual-model", "manual-engine", "/manual"
	s.applyDashboard(dashboardSnapshot{}, nil)
	if s.modelURL != "manual-model" || s.engineURL != "manual-engine" {
		t.Fatal("refresh overwrote manual URLs")
	}
}

func TestReportGuidanceRequiresAllBundlesSealed(t *testing.T) {
	s := testState()
	s.selected = "reports"
	tt := ui.NewTester(s.view, 1120, 1600)
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("premature verify")
	}
	s.progress.NextAction = "verify"
	tt.Frame()
	if _, ok := tt.Find("核验"); !ok {
		t.Fatal(tt.Texts())
	}
	s.progress.Report.Status = "verified"
	tt.Frame()
	if !tt.HasText("已核验") {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("verified report offers verify")
	}
	s.progress.Report.Status = "generated"
	s.progress.NextAction = "report"
	tt.Frame()
	if _, ok := tt.Find("核验"); ok {
		t.Fatal("report action offers verify")
	}
}

func TestDashboardPrepareSkipsInitializedWorkspace(t *testing.T) {
	for _, initialized := range []bool{true, false} {
		r := core.Readiness{CLI: core.InferyardProbe{Status: "ok"}, Workspace: core.DirectoryReadiness{Path: ptr(t.TempDir()), Exists: true, Initialized: initialized}}
		installs, provisions, inits := 0, 0, 0
		e := prepareEnvironmentSteps(context.Background(), func() core.Readiness { return r }, func(context.Context) (string, *core.ApiError) { installs++; return "", nil }, func() *core.ApiError { provisions++; r.Results.Exists = true; r.Models.Exists = true; return nil }, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			inits++
			args, _ := inferyard.SplitCommand(q.Command)
			if len(args) != 3 || args[0] != "init" || args[1] != "--out" || args[2] == *r.Workspace.Path {
				t.Fatal(args)
			}
			r.Workspace.Initialized = true
			return core.WizardResult{}, nil
		})
		want := 0
		if !initialized {
			want = 1
		}
		if e != nil || installs != 0 || provisions != 1 || inits != want {
			t.Fatal(e, installs, provisions, inits)
		}
	}
}

func TestDashboardNewTaskClearsDetailKeepsSelection(t *testing.T) {
	s := testState()
	s.registry = modelRegistryFixture()
	s.model, s.engine = "model", "engine"
	s.detailID = "old"
	s.resumeSource = &core.RunSummary{Path: "old"}
	s.task.report = "old-report"
	ctx, cancel := context.WithCancel(context.Background())
	s.taskCancel = cancel
	s.newBenchmark()
	if ctx.Err() == nil || s.detailID != "" || s.resumeSource != nil || s.task.report != "" || s.model != "model" || s.engine != "engine" {
		t.Fatal(s.task)
	}
	tt := ui.NewTester(s.dashboardView, 1120, 1800)
	if !tt.HasText("运行 benchmark") || !s.selectedBundles["zh-core"] {
		t.Fatal(tt.Texts())
	}
}

func TestDashboardCancelWaitsForRunningAndQueued(t *testing.T) {
	tasks := []core.RunTask{{TaskId: "running", State: "running"}, {TaskId: "queued", State: "queued"}}
	called := map[string]bool{}
	reads := 0
	e := cancelBenchmarkRuns(context.Background(), func() []core.RunTask {
		reads++
		if reads > 2 {
			tasks[0].State = "cancelled"
		}
		return tasks
	}, func(id string) (core.RunTask, *core.ApiError) {
		called[id] = true
		if id == "running" {
			tasks[0].State = "cancelling"
		} else {
			tasks[1].State = "cancelled"
		}
		return core.RunTask{}, nil
	})
	if e != nil || !called["running"] || !called["queued"] || reads < 3 {
		t.Fatal(e, called, reads)
	}
}

func TestDashboardPlanOnlyShowsBlocker(t *testing.T) {
	for _, reason := range []string{"duration_resume_requires_new_window", "batch_plan_not_finished"} {
		s := testState()
		s.progress.Bundles = []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_incomplete", NextAction: "run", NextCommand: ptr("inferyard run --plan /plan"), BlockedReason: &reason}}
		tt := ui.NewTester(s.runView, 1000, 800)
		if !tt.HasText(human(reason)) {
			t.Fatal(tt.Texts())
		}
		if _, found := tt.Find("开始运行"); found {
			t.Fatal("plan execution offered")
		}
	}
}

func TestDashboardFirstDeviceCheckHasNoModelAndRunsOnce(t *testing.T) {
	s := testState()
	s.model = "chosen.gguf"
	s.ready.Models.Path = ptr("/models")
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/workspace", NextAction: "device_check", NextCommand: ptr("inferyard device-check --json --out /preflight"), DevicePreflight: inferyard.ProgressPreflight{Status: "missing"}}
	q, ok := s.takeWorkspaceDeviceRequest()
	args, _ := inferyard.SplitCommand(q.Command)
	if !ok || q.ModelName != "" || !slices.Contains(args, "--models-root") || !slices.Contains(args, "/models") || slices.Contains(args, "--model") {
		t.Fatal(q, args)
	}
	if _, ok = s.takeWorkspaceDeviceRequest(); ok {
		t.Fatal("repeated workspace check")
	}
	s.deviceChecked = ""
	s.progress.DevicePreflight.Status = "ok"
	if _, ok = s.takeWorkspaceDeviceRequest(); ok {
		t.Fatal("checked an existing preflight")
	}
}

func TestDashboardCancelTerminalRaceStillCancelsRemaining(t *testing.T) {
	tasks := []core.RunTask{{TaskId: "finishing", State: "running"}, {TaskId: "queued", State: "starting"}}
	calls := []string{}
	e := cancelBenchmarkRuns(context.Background(), func() []core.RunTask { return append([]core.RunTask(nil), tasks...) }, func(id string) (core.RunTask, *core.ApiError) {
		calls = append(calls, id)
		if id == "finishing" {
			tasks[0].State = "done"
			return core.RunTask{}, core.Conflict("invalid_state")
		}
		tasks[1].State = "cancelled"
		return tasks[1], nil
	})
	if e != nil || !slices.Equal(calls, []string{"finishing", "queued"}) || tasks[1].State != "cancelled" {
		t.Fatal(e, calls, tasks)
	}
}
