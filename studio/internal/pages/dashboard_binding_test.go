package pages

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func bindingFixture() *inferyard.WorkspaceProgress {
	p := &inferyard.WorkspaceProgress{Workspace: "/workspace", NextAction: "bind"}
	for _, name := range []string{"zh-core", "zh-svg-pelican"} {
		candidate := "/workspace/candidates/" + name
		command := commandOf([]string{"config", "bind", "--candidate", candidate + "/candidate.toml", "--out", "/workspace/bound/" + name})
		p.Bundles = append(p.Bundles, inferyard.BundleProgress{Bundle: name, Phase: "bound_missing", NextAction: "bind", CandidatePath: &candidate, NextCommand: &command})
	}
	return p
}

func armedFixture() viewState {
	p := bindingFixture()
	candidate := *p.Bundles[0].CandidatePath
	return viewState{progress: p, candidate: candidate,
		autoBind:    bindingArm{armed: true, workspace: p.Workspace, candidate: candidate, pid: 42},
		engineState: core.EngineState{State: "starting", Pid: 42, Candidate: candidate, Endpoint: "http://127.0.0.1:8080"}}
}

func TestBindingArmWaitsForHealthAndFiresOnce(t *testing.T) {
	s := armedFixture()
	if s.takeAutoBind() || !s.autoBind.armed {
		t.Fatal("fired before health")
	}
	s.engineState.State, s.busy = "running", true
	if s.takeAutoBind() || !s.autoBind.armed {
		t.Fatal("fired while startup work is busy")
	}
	s.busy = false
	if !s.takeAutoBind() || s.autoBind.armed {
		t.Fatal("did not consume arm")
	}
	for range 5 {
		if s.takeAutoBind() {
			t.Fatal("repeated engine event retriggered chain")
		}
	}
}

func TestBindingArmRejectsChangedState(t *testing.T) {
	for name, mutate := range map[string]func(*viewState){
		"phase":        func(s *viewState) { s.progress.NextAction = "prepare_candidate" },
		"workspace":    func(s *viewState) { s.progress.Workspace = "/other" },
		"candidate":    func(s *viewState) { s.candidate = "/other" },
		"pid":          func(s *viewState) { s.engineState.Pid++ },
		"failed":       func(s *viewState) { s.engineState.State = "failed" },
		"orphan":       func(s *viewState) { s.engineState.State = "orphaned" },
		"running task": func(s *viewState) { s.tasks = []core.RunTask{{State: "running"}} },
	} {
		t.Run(name, func(t *testing.T) {
			s := armedFixture()
			s.engineState.State = "running"
			mutate(&s)
			if s.takeAutoBind() || s.autoBind.armed {
				t.Fatal("stale arm survived")
			}
		})
	}
}

func TestBindingChainOrderAndStopOnFailure(t *testing.T) {
	for _, failAt := range []int{-1, 0, 1} {
		s := armedFixture()
		s.engineState.State = "running"
		if !s.takeAutoBind() {
			t.Fatal("not armed")
		}
		p := s.progress
		jobs := bindingJobs(p)
		var seen, announced []string
		get := func(context.Context) (core.ProgressResponse, *core.ApiError) {
			return core.ProgressResponse{Progress: p}, nil
		}
		execute := func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			args, err := inferyard.SplitCommand(q.Command)
			if err != nil {
				t.Fatal(err)
			}
			i := len(seen)
			want := []string{"config", "bind", "--candidate", jobs[i].candidate + "/candidate.toml", "--out", "/workspace/bound/" + jobs[i].bundle, "--pid", "6124", "--endpoint", "http://127.0.0.1:8081"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("%v", args)
			}
			seen = append(seen, jobs[i].bundle)
			if i == failAt {
				return core.WizardResult{ExitCode: 2, Status: inferyard.Blocked, Limitations: []string{"startup_arguments_mismatch"}}, nil
			}
			p.Bundles[i].Phase, p.Bundles[i].NextAction = "run_missing", "run"
			p.NextAction = "run"
			if s.step() != 3 && i == 0 {
				t.Fatal("advanced to run before second bind")
			}
			return core.WizardResult{}, nil
		}
		e := runBindingChain(context.Background(), p.Workspace, jobs, 6124, "http://127.0.0.1:8081", get, execute, func(b string) { announced = append(announced, b) })
		want := []string{"zh-core", "zh-svg-pelican"}
		if failAt == 0 {
			want = want[:1]
		}
		if !reflect.DeepEqual(seen, want) || !reflect.DeepEqual(seen, announced) {
			t.Fatal(seen, announced)
		}
		if failAt < 0 {
			if e != nil || s.step() != 4 {
				t.Fatalf("%v stage=%d", e, s.step())
			}
		} else {
			if e == nil || e.Message != human("startup_arguments_mismatch") || !reflect.DeepEqual(e.Limitations, []string{"startup_arguments_mismatch"}) || s.step() != 3 {
				t.Fatalf("%+v stage=%d", e, s.step())
			}
			s.failure = e
			tt := ui.NewTester(s.feedback, 900, 600)
			if !tt.HasText("startup_arguments_mismatch") || !tt.HasText(human("startup_arguments_mismatch")) {
				t.Fatal(tt.Texts())
			}
		}
		for range 3 {
			if s.takeAutoBind() {
				t.Fatal("success/failure retried automatically")
			}
		}
	}
}

func TestBindingChainStopsWhenProgressChanges(t *testing.T) {
	p := bindingFixture()
	jobs := bindingJobs(p)
	p.Bundles[0].Phase = "run_missing"
	calls := 0
	e := runBindingChain(context.Background(), p.Workspace, jobs, 42, "http://127.0.0.1:8080",
		func(context.Context) (core.ProgressResponse, *core.ApiError) {
			return core.ProgressResponse{Progress: p}, nil
		},
		func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError) {
			calls++
			return core.WizardResult{}, nil
		}, func(string) {})
	if calls != 0 || e == nil || e.Code != "binding_progress_changed" {
		t.Fatal(calls, e)
	}
}

func TestDiscoveryRenderedOnBothPages(t *testing.T) {
	for _, modelPage := range []bool{false, true} {
		s := armedFixture()
		s.autoBind = bindingArm{}
		s.discoveryLoaded = true
		s.discovery = inferyard.ServiceDiscovery{Status: "partial", Limitations: []string{"listener_table_incomplete"}, Services: []inferyard.DiscoveredService{
			{PID: 6124, ProcessName: "/private/bin/inference-engine", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:8081", Health: ptr("2xx"), Models: []string{"/private/models/Qwen.gguf"}, AmbiguousListener: true, Reason: ptr("health_timeout")}}},
			// Listeners without a model directory do not appear in the compatible list.
			{PID: 667, ProcessName: "WeChat", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:14013", Health: nil, Models: nil, Reason: ptr("models_unreachable")}}},
			{PID: 39779, ProcessName: "python3.14", Endpoints: []inferyard.DiscoveredEndpoint{
				{URL: "http://127.0.0.1:8769", Health: nil, Models: nil, Reason: ptr("models_unreachable")},
				// Process names do not determine compatibility or ownership.
				{URL: "http://127.0.0.1:8770", Health: ptr("2xx"), Models: []string{"/private/models/Llama.gguf"}},
			}},
		}}
		render := s.serviceView
		if modelPage {
			render = s.modelsView
		}
		tt := ui.NewTester(render, 1100, 2400)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		if tt.HasText("PID 6124") || tt.HasText("Qwen.gguf") || tt.HasText("Llama.gguf") {
			t.Fatal("external interfaces must start collapsed", tt.Texts())
		}
		click(t, tt, "待确认的兼容接口")
		tt.Frame()
		for _, want := range []string{"本机运行的服务", "inference-engine", "PID 6124 · http://127.0.0.1:8081", "健康检查：2xx", "ambiguous_listener", "health_timeout", "listener_table_incomplete", "python3.14", "PID 39779 · http://127.0.0.1:8770", "另有 1 个未提供模型目录的进程未列出。"} {
			if !tt.HasText(want) {
				t.Fatal(want, tt.Texts())
			}
		}
		for _, gone := range []string{"WeChat", "http://127.0.0.1:14013", "http://127.0.0.1:8769", "模型列表：缺测", "来自 discover", "Qwen.gguf", "Llama.gguf"} {
			if tt.HasText(gone) {
				t.Fatal("non-model noise rendered: "+gone, tt.Texts())
			}
		}
		if modelPage {
			// The models page services card is read-only: no binding UI of any
			// kind, and no bind-related explanation text either.
			if len(modelUINodes(tt, "text", "绑定")) != 0 || tt.HasText("绑定操作在 Dashboard 进行") {
				t.Fatal("models page must not offer binding", tt.Texts())
			}
		} else if len(modelUINodes(tt, "text", "绑定")) == 0 {
			t.Fatal("dashboard keeps the bind action", tt.Texts())
		}
		for _, text := range tt.Texts() {
			if strings.Contains(text, "/private/") {
				t.Fatal("path leaked", text)
			}
		}
	}
}

func TestWizardFailureFallback(t *testing.T) {
	r := core.WizardResult{ExitCode: 2, Status: inferyard.Blocked}
	if e := wizardFailure(r); e.Message != human("blocked") {
		t.Fatal(e)
	}
	r.Limitations = []string{"startup_arguments_mismatch", "service_process_unavailable"}
	if e := wizardFailure(r); e.Message != human("startup_arguments_mismatch") || len(e.Limitations) != 2 {
		t.Fatal(e)
	}
}

type taskBackendFixture struct {
	p          *inferyard.WorkspaceProgress
	requests   []core.WizardRequest
	runRequest core.StartRunsRequest
	tasks      []core.RunTask
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
	e := runBenchmarkFlow(context.Background(), f, startChainResult{workspace: root, candidate: root + "/candidate"}, []string{"zh-core"}, func(stage string) { s.task.stage = stage }, func([]string) {}, func(paths []string, out string) { s.task.runs = paths; s.task.report = out; s.task.stage = "done" })
	if e != nil || len(f.requests) != 2 || len(f.runRequest.ConfigPaths) != 1 || f.runRequest.AllowSingleBundle == nil || !*f.runRequest.AllowSingleBundle {
		t.Fatal(e, f.requests, f.runRequest)
	}
	report := f.requests[1]
	args, err := inferyard.SplitCommand(report.Command)
	if err != nil || args[0] != "report" || args[1] != "--runs" || args[2] != run || report.ReportVerify {
		t.Fatal(report)
	}
	if strings.Contains(report.Command, "pelican") || strings.Contains(f.requests[0].Command, "pelican") {
		t.Fatal(f.requests)
	}
	tt := ui.NewTester(s.stageView, 1120, 1000)
	if !tt.HasText("任务报告已生成") || !tt.HasText(s.task.report) || tt.HasText("已核验") || tt.HasText("missing") {
		t.Fatal(tt.Texts())
	}
	f.p.Bundles[0].Runs[0].Sealed = false
	f.requests = nil
	e = runBenchmarkFlow(context.Background(), f, startChainResult{workspace: root}, []string{"zh-core"}, func(string) {}, func([]string) {}, func([]string, string) { t.Fatal("unsealed report") })
	if e == nil || e.Code != "run_incomplete" || len(f.requests) != 1 {
		t.Fatal(e, f.requests)
	}
}

func TestBindingExistingRunRebindsButResumeDoesNot(t *testing.T) {
	for _, action := range []string{"run", "resume"} {
		root := t.TempDir()
		b := inferyard.BundleProgress{Bundle: "zh-core", Phase: "run_incomplete", CandidatePath: ptr(root + "/candidate"), NextAction: action, NextCommand: ptr("inferyard run --config /old/config.toml")}
		if action == "resume" {
			b.NextCommand = ptr("inferyard resume --from-run /source")
			b.CandidatePath = nil
		}
		var requests []core.WizardRequest
		e := executeExistingBundle(context.Background(), b, root, core.EngineState{State: "running", Pid: 81, Endpoint: "http://127.0.0.1:8080"}, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			requests = append(requests, q)
			return core.WizardResult{}, nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if action == "run" {
			if len(requests) != 2 || !strings.Contains(requests[0].Command, "'--pid' '81'") || strings.Contains(requests[1].Command, "/old/config.toml") {
				t.Fatal(requests)
			}
		} else {
			if len(requests) != 1 || !strings.Contains(requests[0].Command, "'--from-run' '/source'") || !strings.Contains(requests[0].Command, "'--server-pid' '81'") {
				t.Fatal(requests)
			}
		}
	}
}
