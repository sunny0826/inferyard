package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func bindingState(cause *core.ApiError) *viewState {
	s := testState()
	s.task = benchmarkTask{
		stage:   "bind",
		bundles: []string{"zh-svg-pelican"},
		start:   startChainResult{candidate: "/w/candidates/zh-svg-pelican.toml"},
		failure: cause,
	}
	s.failure = cause
	s.engineState = core.EngineState{State: "failed", Error: cause}
	return &s
}

func countText(tt *ui.Tester, want string) int {
	count := 0
	for _, text := range tt.Texts() {
		if strings.Contains(text, want) {
			count++
		}
	}
	return count
}

func TestManualBindingFailureIsOwnedByStepFour(t *testing.T) {
	s := testState()
	s.candidate = "/w/candidates/zh-svg-pelican.toml"
	e := core.Err("candidate_endpoint_mismatch")
	s.executeBinding("inferyard config bind --pid 1 --endpoint http://127.0.0.1:1", e)
	s.failure = e
	if s.bindFailure != e {
		t.Fatalf("binding failure not attributed to the step: %+v", s.bindFailure)
	}
	tt := ui.NewTester(s.view, 1120, 1800)
	if !tt.HasText("candidate_endpoint_mismatch") {
		t.Fatalf("step does not show the cause: %v", tt.Texts())
	}
	if !tt.HasText("服务地址与本次固定的测评配置不一致") {
		t.Fatalf("step does not show the recovery guidance: %v", tt.Texts())
	}
	if n := countText(tt, "candidate_endpoint_mismatch"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}

func TestStartupFailureRendersOnce(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	tt := ui.NewTester(s.view, 1120, 1800)
	if n := countText(tt, "engine_start_failed"); n != 1 {
		t.Fatalf("startup failure rendered %d times: %v", n, tt.Texts())
	}
	if !tt.HasText("连接并核验服务失败") {
		t.Fatalf("step lost its failure summary: %v", tt.Texts())
	}
}

func TestRecoveredBindingAdvancesPastStaleBindStage(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	s.bindingOutcome("", nil)
	if s.task.stage != "" || s.task.failure != nil || s.bindFailure != nil {
		t.Fatalf("stale bind state survived recovery: %+v", s.task)
	}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "run", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_complete", Runs: []inferyard.ProgressRun{{Path: "/w/results/zh-core", Sealed: true}}},
		{Bundle: "zh-svg-pelican", Phase: "run_missing", NextAction: "run"},
	}}
	if got := s.step(); got != 4 {
		t.Fatalf("step %d after recovery", got)
	}
	tt := ui.NewTester(s.stageView, 1120, 1600)
	if tt.HasText("连接并核验服务失败") {
		t.Fatalf("recovered task still shows the old failure: %v", tt.Texts())
	}
}

func TestFreshTaskDoesNotShowPreviousEngineFailure(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	s.clearBenchmark()
	s.task.stage = "prepare_candidate"
	s.progress = &inferyard.WorkspaceProgress{NextAction: "prepare_candidate", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "candidate_missing", NextAction: "prepare_candidate"},
	}}
	if s.step() != 2 {
		t.Fatalf("fresh task is not on the configuration step: %d", s.step())
	}
	tt := ui.NewTester(s.stageView, 1120, 1600)
	if tt.HasText("engine_start_failed") || tt.HasText("未返回原因码。") {
		t.Fatalf("stale engine failure leaked into the new task: %v", tt.Texts())
	}
}

func TestConclusionViewMarksBlockedBundleOnEngineFailure(t *testing.T) {
	s := testState()
	s.engineState = core.EngineState{State: "failed", Error: core.Err("engine_start_failed")}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_complete", Runs: []inferyard.ProgressRun{{Path: "/w/results/zh-core", Sealed: true}}},
		{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind"},
	}}
	tt := ui.NewTester(s.conclusionView, 1120, 1000)
	if !tt.HasText("zh-svg-pelican · 失败") {
		t.Fatalf("blocked bundle not reported: %v", tt.Texts())
	}
	if !tt.HasText("zh-core · 证据已收尾") || tt.HasText("zh-core · 失败") {
		t.Fatalf("completed bundle misreported: %v", tt.Texts())
	}
}

func TestConclusionViewKeepsCompletedBundleOnSecondBindingFailure(t *testing.T) {
	s := testState()
	s.task = benchmarkTask{stage: "bind", bundles: []string{"zh-core", "zh-svg-pelican"}, failure: core.Err("candidate_endpoint_mismatch")}
	s.progress = &inferyard.WorkspaceProgress{NextAction: "bind", Bundles: []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "run_complete", Runs: []inferyard.ProgressRun{{Path: "/w/results/zh-core", Sealed: true}}},
		{Bundle: "zh-svg-pelican", Phase: "bound_missing", NextAction: "bind"},
	}}
	tt := ui.NewTester(s.conclusionView, 1120, 1000)
	if !tt.HasText("zh-svg-pelican · 失败") {
		t.Fatalf("affected bundle not marked: %v", tt.Texts())
	}
	if tt.HasText("zh-core · 失败") || !tt.HasText("zh-core · 证据已收尾") {
		t.Fatalf("completed first bundle downgraded: %v", tt.Texts())
	}
}

func TestLocalServiceChoicesExcludeUsedAndMismatched(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	s.discoveryLoaded = true
	s.discovery = inferyard.ServiceDiscovery{Services: []inferyard.DiscoveredService{
		{PID: 4001, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:48857", Models: []string{"local.gguf"}}}},
		{PID: 4002, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:49999", Models: []string{"local.gguf"}}}},
		{PID: 4003, ProcessName: "LM Studio", Endpoints: []inferyard.DiscoveredEndpoint{{URL: "http://127.0.0.1:48857", Models: []string{"local.gguf"}}}},
	}}
	s.localUsedPIDs = map[int64]bool{4001: true}
	choices := s.localServiceChoices()
	if len(choices) != 2 || choices[0].pid != 4002 || choices[1].pid != 4003 {
		t.Fatalf("step-3 choices: %+v", choices)
	}
	s.tasks = []core.RunTask{{TaskId: "second", Kind: "run", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	choices = s.localServiceChoices()
	if len(choices) != 1 || choices[0].pid != 4003 {
		t.Fatalf("handoff choices: %+v", choices)
	}
}

func TestDiscoveredBindingFailureIsOwnedByStepFour(t *testing.T) {
	s := testState()
	s.progress.NextAction = "bind"
	e := core.Err("service_process_unavailable")
	s.bindingOutcome("zh-svg-pelican", e)
	tt := ui.NewTester(s.view, 1120, 1800)
	if !tt.HasText("service_process_unavailable") || !tt.HasText("已连接的服务进程不可用") {
		t.Fatalf("discovered failure not shown in the step: %v", tt.Texts())
	}
	if n := countText(tt, "service_process_unavailable"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}

func TestRecoveryClearsOnlyTheResolvedEngineFailure(t *testing.T) {
	s := bindingState(core.Err("engine_start_failed"))
	s.bindingOutcome("", nil)
	if s.stepFailed() {
		t.Fatal("recovered failure still blocks the step")
	}
	s.engineState = core.EngineState{State: "failed", Pid: 900, Error: core.Err("engine_port_in_use")}
	if !s.stepFailed() || s.activeTaskFailure().Code != "engine_port_in_use" {
		t.Fatalf("new engine failure hidden: %+v", s.activeTaskFailure())
	}
}

func TestLocalHandoffOffersRefreshAndRejectsReuse(t *testing.T) {
	s := testState()
	s.task.start = startChainResult{localRequest: &core.LocalCandidateRequest{Endpoint: "http://127.0.0.1:48857"}}
	s.localUsedPIDs = map[int64]bool{4001: true}
	s.tasks = []core.RunTask{{TaskId: "second", State: "starting", Bundle: ptr("zh-svg-pelican"), AwaitingService: true}}
	s.localBindingTask, s.localPID, s.localEndpoint = "second", "4001", "http://127.0.0.1:48857"
	tt := ui.NewTester(s.awaitingLocalServiceView, 1120, 900)
	if !tt.HasText("刷新服务列表") {
		t.Fatalf("handoff card has no refresh: %v", tt.Texts())
	}
	if !tt.HasText("此服务已被上一测评题集使用") && !tt.HasText("不能重复使用") {
		t.Fatalf("used PID is not flagged: %v", tt.Texts())
	}
	if !s.localSelectionInvalid(4001, nil) {
		t.Fatal("used PID is still accepted")
	}
	s.localPID, s.localEndpoint = "4002", "http://127.0.0.1:49999"
	if !s.localSelectionInvalid(4002, nil) {
		t.Fatal("mismatched endpoint is still accepted")
	}
	s.localEndpoint = "http://127.0.0.1:48857"
	if s.localSelectionInvalid(4002, nil) {
		t.Fatal("fresh service was rejected")
	}
}
