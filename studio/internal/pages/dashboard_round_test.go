package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// runStepState is the run step whose bundle the CLI already bound to an
// external service, with no managed engine running.
func runStepState(bound string) *viewState {
	s := testState()
	s.task.stage = "run"
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/w", NextAction: "run", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_missing", NextAction: "run", BoundPath: ptr(bound), NextCommand: ptr("inferyard run --config " + bound)},
	}}
	return &s
}

// An external/discovered binding that the CLI verified is enough to start the
// run step, without Studio's managed engine.
func TestRunStepStartsFromVerifiedExternalBinding(t *testing.T) {
	s := runStepState("/w/bound/zh-svg-pelican-1/config.toml")
	if s.engineState.State == "running" {
		t.Fatal("fixture manages an engine")
	}
	b := s.active()
	if b == nil || !s.runServiceReady(*b) {
		t.Fatalf("verified binding is not runnable: %+v", b)
	}
	var calls []string
	e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
		calls = append(calls, q.Command)
		return core.WizardResult{}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(calls) != 1 {
		t.Fatalf("unexpected run commands: %v", calls)
	}
	args, err := inferyard.SplitCommand(calls[0])
	if err != nil || len(args) < 2 || args[0] != "run" || args[1] != "--config" || args[2] != "/w/bound/zh-svg-pelican-1/config.toml" {
		t.Fatalf("unexpected run command: %v", args)
	}
}

// The run button itself must be reachable, not only the gate behind it.
func TestRunButtonReachableWithVerifiedBinding(t *testing.T) {
	s := runStepState("/w/bound/zh-svg-pelican-1/config.toml")
	tt := ui.NewTester(s.view, 1120, 2000)
	if !tt.HasText("开始运行") {
		t.Fatalf("run button missing: %v", tt.Texts())
	}
	reachable := false
	for range 60 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("开始运行") {
			reachable = true
			break
		}
	}
	if !reachable {
		t.Fatalf("run button disabled without a managed engine: %v", tt.Texts())
	}
}

// Without a managed engine and without a verified binding, the run step stays
// blocked.
func TestRunStepRequiresServiceOrVerifiedBinding(t *testing.T) {
	s := runStepState("/w/bound/zh-svg-pelican-1/config.toml")
	s.progress.Bundles[0].BoundPath = nil
	s.progress.Bundles[0].Phase = "bound_missing"
	s.progress.Bundles[0].NextAction = "bind"
	b := s.active()
	if b == nil || s.runServiceReady(*b) {
		t.Fatalf("unbound bundle is runnable: %+v", b)
	}
	if e := executeExistingBundle(context.Background(), *b, "/w", s.engineState, func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError) {
		t.Fatal("unbound run executed")
		return core.WizardResult{}, nil
	}); e == nil {
		t.Fatal("unbound run accepted")
	}
}

// The latest connection rejection wins over the older auto-start failure.
func TestStepDiagnosisPrefersLatestBindingRejection(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	latest := core.Err("service_identity_unavailable")
	s.failBinding(latest)
	cause, failed := s.stepDiagnosis()
	if !failed || cause != latest {
		t.Fatalf("stale auto-start error still shown: %+v", cause)
	}
	tt := ui.NewTester(s.view, 1120, 1800)
	if tt.HasText("engine_start_failed") {
		t.Fatalf("stale cause still visible: %v", tt.Texts())
	}
	if !tt.HasText("service_identity_unavailable") {
		t.Fatalf("latest cause missing: %v", tt.Texts())
	}
	if n := countText(tt, "service_identity_unavailable"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}

// A handoff rejection is rendered by the handoff card and never swallowed: the
// page banner suppresses only the copy the card shows.
func TestHandoffFailureStaysVisible(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	s.tasks = []core.RunTask{{TaskId: "second", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	s.localBindingTask = "second"
	s.busy = true
	e := core.Err("service_identity_unavailable")
	s.handoffFailure, s.failure = e, e
	tt := ui.NewTester(s.view, 1120, 1800)
	if n := countText(tt, "service_identity_unavailable"); n != 1 {
		t.Fatalf("handoff error rendered %d times: %v", n, tt.Texts())
	}
	if !tt.HasText("连接此测评题集失败") {
		t.Fatalf("handoff card does not render its error: %v", tt.Texts())
	}
}

// A new round clears the rejected connection, its bundle and the used services.
func TestClearBenchmarkDropsBindingState(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	s.failedBundle, s.handoffFailure = "zh-svg-pelican", core.Err("service_identity_unavailable")
	s.localUsedPIDs = map[int64]bool{4001: true}
	s.localBindingTask, s.localPID = "second", "4001"
	// No service is mounted, so starting a new round resets synchronously, as it
	// does before the first round.
	s.newBenchmark()
	if s.bindFailure != nil || s.failedBundle != "" || s.handoffFailure != nil {
		t.Fatalf("binding state survived reset: %+v", s.bindFailure)
	}
	if len(s.localUsedPIDs) != 0 || s.localBindingTask != "" || s.localPID != "" {
		t.Fatalf("service tracking survived reset: %v %q", s.localUsedPIDs, s.localPID)
	}
	if s.stepFailed() {
		t.Fatal("fresh round reports a failure")
	}
}

// Only the bundle whose connection failed is marked, even when an earlier bundle
// is still awaiting its binding.
func TestFailedBundleNamesTheActualFailure(t *testing.T) {
	s := testState()
	s.task = benchmarkTask{stage: "bind", bundles: []string{"zh-core", "zh-svg-pelican"}}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "bound_missing", NextAction: "bind"},
		{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind"},
	}}
	s.bindingOutcome("zh-svg-pelican", core.Err("candidate_endpoint_mismatch"))
	tt := ui.NewTester(s.conclusionView, 1120, 1000)
	if !tt.HasText("zh-svg-pelican · 失败") {
		t.Fatalf("failed bundle not marked: %v", tt.Texts())
	}
	if tt.HasText("zh-core · 失败") {
		t.Fatalf("waiting bundle marked failed: %v", tt.Texts())
	}
}

// The handoff refresh works while the queue is busy awaiting the next service.
func TestHandoffRefreshAvailableWhileBusy(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857", ServicePID: 4001}}
	s.tasks = []core.RunTask{{TaskId: "second", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	s.localBindingTask, s.busy = "second", true
	tt := ui.NewTester(s.awaitingLocalServiceView, 1120, 900)
	if !tt.HasText("刷新服务列表") {
		t.Fatalf("refresh missing: %v", tt.Texts())
	}
	// A disabled button cannot take focus, so focus reaching it while busy proves
	// the queue does not block discovery.
	focused := false
	for range 40 {
		tt.Key(0, ui.KeyTab)
		if tt.Focused("刷新服务列表") {
			focused = true
			break
		}
	}
	if !focused {
		t.Fatalf("refresh unreachable while busy: %v", tt.Texts())
	}
	if !s.localSelectionInvalid(4001, nil) {
		t.Fatal("first bundle's PID is offered again")
	}
	if choices := s.localServiceChoices(); len(choices) != 0 {
		t.Fatalf("first bundle's PID listed: %+v", choices)
	}
}

// A blocked run step whose model-service card is on screen renders the cause
// once: the card owns it and the run step does not repeat it.
func TestRunStepDoesNotDuplicateServiceFailure(t *testing.T) {
	s := testState()
	s.progress = &inferyard.WorkspaceProgress{Workspace: "/w", NextAction: "run", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_missing", NextAction: "run", BoundPath: ptr("/w/bound/zh-core-1/config.toml"), CandidatePath: ptr("/w/candidates/zh-core-1/candidate.toml")},
	}}
	e := core.Err("service_identity_unavailable")
	s.engineState = core.EngineState{State: "failed", Error: e}
	if !s.serviceCardVisible() {
		t.Fatalf("fixture hides the service card: step=%d", s.step())
	}
	tt := ui.NewTester(s.view, 1120, 2200)
	if !tt.HasText("service_identity_unavailable") {
		t.Fatalf("step lost the cause: %v", tt.Texts())
	}
	if n := countText(tt, "service_identity_unavailable"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}

// A rejected handoff does not consume the PID, so the same service can be retried.
func TestRejectedHandoffKeepsPIDAvailable(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	// No service is mounted, so the attempt is rejected; it must still leave the
	// PID available for a retry once the service is ready.
	s.connectHandoffService("second", 4002)
	if s.handoffFailure == nil {
		t.Fatal("rejected attempt recorded no failure")
	}
	if s.localPIDUsed(4002) {
		t.Fatal("rejected attempt consumed the PID")
	}
	s.handoffFailure, s.failure = nil, nil
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("PID is not retryable after a rejection")
	}
}
