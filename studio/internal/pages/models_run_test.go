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

func TestRunPanelNewTaskPreservesPair(t *testing.T) {
	s := runStateFixture()
	s.selected = "models"
	s.progress = &inferyard.WorkspaceProgress{NextAction: "device_check"}
	s.detailID = "old-run"
	s.task.stage = "report"
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	assertModelsReadOnly(t, tt)
	if tt.HasText("请先到 Dashboard 完成设备检查") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "用这个组合新开任务")
	if s.selected != "dashboard" || s.task.stage != "prepare_candidate" || s.detailID != "" {
		t.Fatal("new task entry did not reset task", s.selected, s.task.stage, s.detailID)
	}
	if s.model != "model.gguf" || s.engine != "engine-2 · llama-server" || s.busy {
		t.Fatal("selection changed or service started", s.model, s.engine, s.busy)
	}
}

func assertModelsReadOnly(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for _, label := range []string{"启动", "停止", "绑定", "停止遗留服务", "仅删除记录（不终止进程）", "到 Dashboard 完成设备检查"} {
		if len(modelUINodes(tt, "text", label)) != 0 {
			t.Fatal("models page exposes action", label, tt.Texts())
		}
	}
}

func TestRunPanelAttributionStates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		match, known bool
		badge, hint  string
	}{
		{"own", true, true, "运行中 · PID 42 · http://127.0.0.1:8080", ""},
		{"other", false, true, "运行中 · 另一个组合的服务（候选 zh-core-1）", ""},
		{"unknown", false, false, "运行中 · 无法确认属于当前组合（候选 zh-core-1）", ""},
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
			assertModelsReadOnly(t, tt)
			if s.busy {
				t.Fatal("start must stay disabled while a service is active")
			}
		})
	}
}

func TestRunPanelRunActiveLocksSelection(t *testing.T) {
	for _, state := range []string{"queued", "running", "cancelling"} {
		t.Run(state, func(t *testing.T) {
			s := runStateFixture()
			s.selected = "models"
			s.tasks = []core.RunTask{{State: state}}
			tt := ui.NewTester(s.view, 1120, 2400)
			assertModelsReadOnly(t, tt)
			for _, want := range []string{"引擎：engine-2 · llama-server", "模型：model.gguf"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if len(designFieldInputs(tt, "模型")) != 0 || len(designFieldInputs(tt, "引擎")) != 0 || len(modelUINodes(tt, "text", s.model)) != 0 || len(modelUINodes(tt, "text", s.engine)) != 0 {
				t.Fatal("active run renders pickers", tt.Texts())
			}
		})
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
		if !tt.HasText(tc.badge) || !tt.HasText("请到任务页查看并处理服务失败。") {
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
	assertModelsReadOnly(t, tt)
	if !tt.HasText("遗留引擎") || !tt.HasText("请到任务页处理遗留服务") || tt.HasText("用这个组合新开任务") {
		t.Fatal(tt.Texts())
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

func TestRunPanelEngineVerdictWarning(t *testing.T) {
	s := runStateFixture()
	s.checks.Engines[s.engine] = core.EngineEligibility{State: core.EngineCheckIneligible, Reason: "invalid_library_manifest"}
	tt := ui.NewTester(s.runPanelView, 1120, 900)
	assertModelsReadOnly(t, tt)
	if !tt.HasText(s.engine + " · " + human("invalid_library_manifest") + "。请重新选择。") {
		t.Fatal(tt.Texts())
	}
}
