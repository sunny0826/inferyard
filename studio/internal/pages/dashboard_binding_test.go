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
