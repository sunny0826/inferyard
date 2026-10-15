package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// runStateFixture selects a combination on a workspace that reached the
// candidate stage; candidate matching is injected so no filesystem is needed.
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
	s.candidateMatches = func(candidate, model, engine string) (bool, bool) { return true, true }
	return &s
}

func TestRunPanelStartStopEnableMatrix(t *testing.T) {
	s := runStateFixture()
	s.engineState = core.EngineState{State: "stopped"}
	if !s.startEnabled(false, false, false, false, false) || s.stopEnabled(false, false) {
		t.Fatal("idle combination must be startable")
	}
	for name, mutate := range map[string]func(*viewState){
		"gated":     func(s *viewState) {},
		"runActive": func(s *viewState) {},
		"busy":      func(s *viewState) { s.busy = true },
		"cli":       func(s *viewState) { s.ready.CLI.Status = "not_found" },
		"orphan":    func(s *viewState) { s.engineState.State = "orphaned" },
	} {
		t.Run(name, func(t *testing.T) {
			s := runStateFixture()
			s.engineState = core.EngineState{State: "stopped"}
			mutate(s)
			gated := name == "gated"
			runActive := name == "runActive"
			if s.startEnabled(gated, false, false, false, runActive) {
				t.Fatal("start must be disabled")
			}
		})
	}
	s = runStateFixture()
	s.engineState = core.EngineState{State: "running"}
	if !s.stopEnabled(true, false) {
		t.Fatal("active engine must be stoppable")
	}
	if s.stopEnabled(true, true) || s.stopEnabled(false, false) {
		t.Fatal("stop lock matrix")
	}
}

func TestRunPanelGateDisablesStart(t *testing.T) {
	s := runStateFixture()
	s.progress = &inferyard.WorkspaceProgress{NextAction: "device_check"}
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	for _, want := range []string{"设备检查未通过：请先到 Dashboard 完成设备检查，再启动服务。", "到 Dashboard 完成设备检查"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	click(t, tt, "启动")
	if s.busy {
		t.Fatal("gated start reached the chain")
	}
	click(t, tt, "到 Dashboard 完成设备检查")
	if s.selected != "dashboard" {
		t.Fatal(s.selected)
	}
}

func TestRunPanelAttributionStates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		match, known bool
		badge, hint  string
	}{
		{"own", true, true, "运行中 · PID 42 · http://127.0.0.1:8080", ""},
		{"other", false, true, "运行中 · 另一个组合的服务（候选 zh-core-1）", "当前选定组合没有运行中的服务；停止将终止另一个组合正在运行的服务。"},
		{"unknown", false, false, "运行中 · 无法确认属于当前组合（候选 zh-core-1）", "无法确认运行中服务属于当前选定组合；停止将终止正在运行的服务。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := runStateFixture()
			s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/w/candidates/zh-core-1"}
			s.candidateMatches = func(candidate, model, engine string) (bool, bool) { return tc.match, tc.known }
			tt := ui.NewTester(s.runPanelView, 1120, 900)
			if !tt.HasText(tc.badge) {
				t.Fatal(tt.Texts())
			}
			if tc.hint != "" && !tt.HasText(tc.hint) {
				t.Fatal(tt.Texts())
			}
			if tc.name == "own" && (tt.HasText("另一个组合") || tt.HasText("无法确认")) {
				t.Fatal(tt.Texts())
			}
			click(t, tt, "启动")
			if s.busy {
				t.Fatal("start must stay disabled while a service is active")
			}
		})
	}
}

func TestRunPanelRunActiveLocksStop(t *testing.T) {
	s := runStateFixture()
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:8080", Candidate: "/w/candidates/zh-core-1"}
	s.tasks = []core.RunTask{{State: "running"}}
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	if !tt.HasText("测评运行期间不能停止或重新启动服务。") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "停止")
	if s.busy {
		t.Fatal("stop reached the service during a run")
	}
}

func TestRunPanelFailureShowsFixedReason(t *testing.T) {
	for _, tc := range []struct{ code, badge string }{
		{"engine_exited", "失败：engine_exited · 模型服务启动后退出"},
		{"engine_start_timeout", "失败：engine_start_timeout · 模型服务启动超时（模型加载未完成）"},
		{"engine_port_in_use", "失败：engine_port_in_use · 端口已被占用：该端口可能已有模型服务"},
	} {
		s := runStateFixture()
		s.engineState = core.EngineState{State: "failed", Error: &core.ApiError{Code: tc.code}}
		tt := ui.NewTester(s.runPanelView, 1120, 900)
		if !tt.HasText(tc.badge) || !tt.HasText("查看日志后可重新启动。") {
			t.Fatal(tc.code, tt.Texts())
		}
	}
	s := runStateFixture()
	s.engineState = core.EngineState{State: "failed"}
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	if !tt.HasText("失败") {
		t.Fatal(tt.Texts())
	}
}

func TestRunPanelOrphan(t *testing.T) {
	s := runStateFixture()
	s.engineState = core.EngineState{State: "orphaned", Error: core.Err("engine_owner_invalid")}
	tt := ui.NewTester(s.runPanelView, 1120, 1200)
	for _, want := range []string{"遗留引擎", "停止遗留服务", "仅删除记录（不终止进程）"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	click(t, tt, "启动")
	if s.busy {
		t.Fatal("orphan allowed a new startup")
	}
}

func TestRunPanelHidesNonLoopbackEndpoint(t *testing.T) {
	s := runStateFixture()
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://192.168.1.5:8080", Candidate: "/w/candidates/zh-core-1"}
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	if tt.HasText("192.168.1.5") {
		t.Fatal("non-loopback endpoint displayed", tt.Texts())
	}
	if loopbackEndpoint("http://192.168.1.5:8080") == "http://192.168.1.5:8080" || loopbackEndpoint("http://127.0.0.1:8080") != "http://127.0.0.1:8080" {
		t.Fatal("loopback filter changed")
	}
}

func TestRunPanelEngineVerdictControlsStart(t *testing.T) {
	for _, state := range []string{core.EngineCheckEligible, core.EngineCheckChecking, core.EngineCheckUnknown, core.EngineCheckIneligible} {
		t.Run(state, func(t *testing.T) {
			s := runStateFixture()
			s.checks.Engines[s.engine] = core.EngineEligibility{State: state, Reason: "invalid_library_manifest"}
			if s.startEnabled(false, false, false, false, false) != (state != core.EngineCheckIneligible) {
				t.Fatal("incorrect start gate", state)
			}
			if state == core.EngineCheckIneligible {
				tt := ui.NewTester(s.runPanelView, 1120, 900)
				if !tt.HasText(s.engine + " · " + human("invalid_library_manifest") + "。请重新选择。") {
					t.Fatal(tt.Texts())
				}
			}
		})
	}
}
