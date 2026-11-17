package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// runStateFixture reaches the configuration stage with a selected pair; the
// page never reads the filesystem because candidate matching is not part of the
// task page's selection state.
func runStateFixture() *viewState {
	s := testState()
	s.model, s.engine = "model.gguf", "engine-2 · llama-server"
	s.registry = modelRegistryFixture()
	s.checks = eligibleChecks()
	s.progress = &inferyard.WorkspaceProgress{
		NextAction:      "bind",
		DevicePreflight: inferyard.ProgressPreflight{Status: "ok", Path: ptr("/w/preflight/device-1/device-preflight.json")},
		Bundles:         []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "bound_missing"}},
	}
	return &s
}

func TestEngineFailureShowsFixedReason(t *testing.T) {
	for _, code := range []string{"engine_exited", "engine_start_timeout", "engine_port_in_use"} {
		state := core.EngineState{State: "failed", Error: &core.ApiError{Code: code}}
		if got := failureBadge(state); got != "失败："+code+" · "+human(code) {
			t.Fatalf("badge %q for %s", got, code)
		}
	}
	if failureBadge(core.EngineState{State: "failed"}) != "失败" {
		t.Fatal("missing error invented a cause")
	}
}

func TestRunPanelOrphan(t *testing.T) {
	s := runStateFixture()
	s.engineState = core.EngineState{State: "orphaned", Error: core.Err("engine_owner_invalid")}
	tt := ui.NewTester(s.candidateView, 1120, 1200)
	if !tt.HasText("遗留服务") || !tt.HasText("停止遗留服务") {
		t.Fatal(tt.Texts())
	}
}

func TestRunPanelHidesNonLoopbackEndpoint(t *testing.T) {
	if loopbackEndpoint("http://192.168.1.5:8080") == "http://192.168.1.5:8080" || loopbackEndpoint("http://127.0.0.1:8080") != "http://127.0.0.1:8080" {
		t.Fatal("loopback filter changed")
	}
	s := runStateFixture()
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://192.168.1.5:8080"}
	tt := ui.NewTester(s.candidateView, 1120, 900)
	if tt.HasText("192.168.1.5") {
		t.Fatal("non-loopback endpoint displayed", tt.Texts())
	}
}

func TestRunPanelEngineVerdictWarning(t *testing.T) {
	s := runStateFixture()
	s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"}
	tt := ui.NewTester(s.candidateView, 1120, 900)
	if !tt.HasText("当前选择不可用") || !tt.HasText("引擎运行库不完整或库链接无效") {
		t.Fatal(tt.Texts())
	}
}
