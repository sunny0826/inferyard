package pages

import (
	"context"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func twoBundlePlan() *viewState {
	s := testState()
	s.task = benchmarkTask{stage: "bind", bundles: []string{"zh-core", "zh-svg-pelican"}}
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/w", NextAction: "bind", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_missing", NextAction: "run", BoundPath: ptr("/w/bound/zh-core-1/config.toml"), CandidatePath: ptr("/w/c/zh-core.toml")},
		{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind", CandidatePath: ptr("/w/c/zh-svg-pelican.toml"),
			NextCommand: ptr("inferyard config bind --candidate /w/c/zh-svg-pelican.toml --pid 1 --endpoint http://127.0.0.1:1")},
	}}
	return &s
}

func TestManualBindTargetsUnboundBundle(t *testing.T) {
	s := twoBundlePlan()
	if got := s.active().Bundle; got != "zh-core" {
		t.Fatalf("fixture active bundle: %q", got)
	}
	target := s.bindTarget()
	if target == nil || target.Bundle != "zh-svg-pelican" {
		t.Fatalf("manual bind targets %+v", target)
	}
	command, e := s.bindCommand("4001", "http://127.0.0.1:48857")
	if e != nil {
		t.Fatal(e)
	}
	args, err := inferyard.SplitCommand(command)
	if err != nil {
		t.Fatal(err, command)
	}
	if !slices.Contains(args, "/w/c/zh-svg-pelican.toml") || slices.Contains(args, "/w/c/zh-core.toml") {
		t.Fatalf("bind command targets the wrong bundle: %v", args)
	}
	if !slices.Contains(args, "--pid") || !slices.Contains(args, "4001") {
		t.Fatalf("bind command lost the chosen process: %v", args)
	}
}

func TestRunKeepsVerifiedBindingOverManagedEngine(t *testing.T) {
	s := twoBundlePlan()
	s.progress.NextAction = "run"
	s.progress.Bundles[1] = inferyard.BundleProgress{Bundle: "zh-svg-pelican", Phase: "run_missing", NextAction: "run",
		BoundPath: ptr("/w/bound/zh-svg-pelican-1/config.toml"), CandidatePath: ptr("/w/c/zh-svg-pelican.toml"),
		NextCommand: ptr("inferyard run --config /w/bound/zh-svg-pelican-1/config.toml")}
	s.engineState = core.EngineState{State: "running", Pid: 9001, Endpoint: "http://127.0.0.1:48857"}
	b := s.bindTarget()
	if b == nil {
		t.Fatal("no run target")
	}
	s.progress.Bundles[0].Phase = "run_complete"
	for i := range s.progress.Bundles {
		if s.progress.Bundles[i].Bundle == "zh-core" {
			s.progress.Bundles[i].BoundPath = ptr("/w/bound/zh-core-1/config.toml")
		}
	}
	b = s.bundleFor("zh-svg-pelican", s.progress.Bundles)
	var ran []string
	e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		ran = append(ran, q.Command)
		return core.WizardResult{}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(ran) != 1 {
		t.Fatalf("expected one command, got %v", ran)
	}
	if slices.Contains(ran, "config") {
		t.Fatalf("run rebound to the managed engine: %v", ran)
	}
	args, err := inferyard.SplitCommand(ran[0])
	if err != nil || !slices.Equal(args, []string{"run", "--config", "/w/bound/zh-svg-pelican-1/config.toml"}) {
		t.Fatalf("unexpected run command: %v / %v", args, err)
	}
}

func TestFreshAttemptClearsStaleBindFailure(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	stale := core.Err("service_identity_unavailable")
	s.failedBundle = "zh-svg-pelican"
	s.failBinding(stale)
	if cause, failed := s.stepDiagnosis(); !failed || cause != stale {
		t.Fatalf("fixture does not show the stale cause: %+v", cause)
	}
	s.beginAttempt()
	if s.bindFailure != nil || s.failedBundle != "" || s.handoffFailure != nil {
		t.Fatalf("stale attempt survived: %+v %q", s.bindFailure, s.failedBundle)
	}
	fresh := core.Err("engine_start_timeout")
	s.task.failure = fresh
	s.engineState = core.EngineState{State: "failed", Error: fresh}
	cause, failed := s.stepDiagnosis()
	if !failed || cause != fresh {
		t.Fatalf("stale cause still wins: %+v", cause)
	}
	tt := ui.NewTester(s.view, 1120, 1800)
	if tt.HasText("service_identity_unavailable") || tt.HasText("无法核验后台进程的启动身份") {
		t.Fatalf("stale cause still rendered: %v", tt.Texts())
	}
	if !tt.HasText("模型服务启动超时（模型加载未完成）") {
		t.Fatalf("fresh cause missing: %v", tt.Texts())
	}
	click(t, tt, "错误详情")
	if tt.HasText("service_identity_unavailable") || !tt.HasText("错误码：engine_start_timeout") {
		t.Fatalf("fresh cause code missing or stale code leaked: %v", tt.Texts())
	}
}

func TestStartAndRetryBeginFreshAttempt(t *testing.T) {
	prepare := func() *viewState {
		s := bindingState(core.Err("engine_start_failed"))
		s.failBinding(core.Err("service_identity_unavailable"))
		s.failedBundle, s.handoffFailure = "zh-svg-pelican", core.Err("cli_blocked")
		return s
	}
	blocked := prepare()
	blocked.busy = true
	if blocked.beginStart() {
		t.Fatal("busy start accepted")
	}
	if blocked.bindFailure == nil || blocked.failedBundle == "" {
		t.Fatal("refused start discarded the visible cause")
	}
	s := prepare()
	s.candidate = "/w/candidate.toml"
	if !s.beginStart() {
		t.Fatal("start refused")
	}
	if s.bindFailure != nil || s.failedBundle != "" || s.handoffFailure != nil {
		t.Fatalf("previous attempt survived the start: %+v %q %+v", s.bindFailure, s.failedBundle, s.handoffFailure)
	}
	r := prepare()
	if !r.beginRetry() {
		t.Fatal("retry refused")
	}
	if r.bindFailure != nil || r.failedBundle != "" || r.handoffFailure != nil {
		t.Fatalf("previous attempt survived the retry: %+v %q %+v", r.bindFailure, r.failedBundle, r.handoffFailure)
	}
}

func TestAutoBindingFailureNamesFailedBundle(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		fixture func(root string) *taskBackendFixture
	}{
		{name: "error", fixture: func(root string) *taskBackendFixture { return &taskBackendFixture{failBind: true} }},
		{name: "exit_code", fixture: func(root string) *taskBackendFixture { return &taskBackendFixture{failBindExit: true} }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			f := scenario.fixture(root)
			f.p = &inferyard.WorkspaceProgress{Workspace: root, NextAction: "bind", Bundles: []inferyard.BundleProgress{
				{Bundle: "zh-core", Phase: "bound_missing", NextAction: "bind", CandidatePath: ptr(root + "/c/zh-core.toml")},
				{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind", CandidatePath: ptr(root + "/c/zh-svg-pelican.toml")},
			}}
			f.failBundle = "zh-svg-pelican"
			bindings := &benchmarkBindings{}
			e := runBenchmarkFlow(context.Background(), f, startChainResult{workspace: root, candidate: root + "/candidate"}, []string{"zh-core", "zh-svg-pelican"}, bindings,
				func(string) {}, func([]string) {}, func([]string, string) { t.Fatal("failed binding produced a report") })
			if e == nil {
				t.Fatalf("flow result: %+v", e)
			}
			if bindings.failedBundle != "zh-svg-pelican" {
				t.Fatalf("failed bundle: %q", bindings.failedBundle)
			}
			s := testState()
			s.task = benchmarkTask{stage: "bind", bundles: []string{"zh-core", "zh-svg-pelican"}}
			s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: f.p.Bundles}
			s.failedBundle = bindings.failedBundle
			s.failBinding(e)
			tt := ui.NewTester(s.conclusionView, 1120, 1000)
			if !tt.HasText("zh-svg-pelican · 失败") {
				t.Fatalf("failed bundle not marked: %v", tt.Texts())
			}
			if tt.HasText("zh-core · 失败") {
				t.Fatalf("bound bundle marked failed: %v", tt.Texts())
			}
		})
	}
}

func (s *viewState) bundleFor(name string, bundles []inferyard.BundleProgress) *inferyard.BundleProgress {
	for i := range bundles {
		if bundles[i].Bundle == name {
			return &bundles[i]
		}
	}
	return nil
}
