//go:build ui_debug && !release

package pages

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type localParityFrame struct {
	Stage, Awaiting, Failure string
	Completed, Report        []string
	Continue, Retry          bool
}

func localFrame(stage string, tasks []core.RunTask, paths []string, failure *core.ApiError) localParityFrame {
	f := localParityFrame{Stage: stage, Completed: []string{}, Report: []string{}}
	for _, task := range tasks {
		if task.AwaitingService && !terminal(task.State) {
			f.Awaiting = emptyText(task.Bundle)
			f.Continue = true
		}
		if task.State == "done" && emptyText(task.RunPath) != "" {
			f.Completed = append(f.Completed, emptyText(task.Bundle))
		}
		if slices.Contains(paths, emptyText(task.RunPath)) && emptyText(task.RunPath) != "" {
			f.Report = append(f.Report, emptyText(task.Bundle))
		}
	}
	if failure != nil {
		f.Failure = failure.Code
		f.Retry = true
	}
	slices.Sort(f.Completed)
	slices.Sort(f.Report)
	return f
}

func (f *localParityService) frame() localParityFrame {
	f.mu.Lock()
	stage, paths, failure := f.stage, append([]string{}, f.report...), f.failure
	f.mu.Unlock()
	return localFrame(stage, f.service.RunTasks(), paths, failure)
}

func previewLocalFrame(p *previewSession) localParityFrame {
	paths := []string{}
	if p.state.task.report != "" {
		paths = p.state.task.runs
	}
	return localFrame(p.state.task.stage, p.state.tasks, paths, p.state.task.failure)
}

func waitLocalParity(t *testing.T, condition func() bool, advance func()) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		advance()
	}
	t.Fatal("local workflow did not reach expected checkpoint")
}

func assertLocalFrames(t *testing.T, f *localParityService, p *previewSession, want localParityFrame) {
	t.Helper()
	production, preview := f.frame(), previewLocalFrame(p)
	if !reflect.DeepEqual(production, want) {
		t.Fatalf("production=%+v want=%+v", production, want)
	}
	if !reflect.DeepEqual(preview, want) {
		t.Fatalf("preview=%+v want=%+v", preview, want)
	}
	if !reflect.DeepEqual(production, preview) {
		t.Fatal("production and preview diverged")
	}
	if want.Continue {
		tt := ui.NewTester(p.state.awaitingLocalServiceView, 1120, 1000)
		if !tt.HasText("绑定并继续此题包") {
			t.Fatal(tt.Texts())
		}
	}
}

func productionAwaiting(f *localParityService) string {
	for _, task := range f.service.RunTasks() {
		if task.AwaitingService {
			return task.TaskId
		}
	}
	return ""
}
func previewAwaiting(p *previewSession) string {
	for _, task := range p.state.tasks {
		if task.AwaitingService {
			return task.TaskId
		}
	}
	return ""
}

func TestPreviewLocalQueueParity(t *testing.T) {
	for _, scenario := range []string{"two_scopes", "second_binding_failure_retry", "cancel_waiting"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLocalParityService(t)
			p := newPreviewSession(t.Context())
			p.loadScene(previewSceneIndex(t, "引擎 · LM Studio 手动绑定"))
			p.state.startBenchmark()
			f.start()
			waitLocalParity(t, func() bool { return f.frame().Awaiting == "zh-svg-pelican" }, func() { time.Sleep(10 * time.Millisecond) })
			waitLocalParity(t, func() bool { return previewLocalFrame(p).Awaiting == "zh-svg-pelican" }, func() { p.advance(100 * time.Millisecond) })
			waiting := localParityFrame{Stage: "run", Awaiting: "zh-svg-pelican", Completed: []string{"zh-core"}, Report: []string{}, Continue: true}
			assertLocalFrames(t, f, p, waiting)
			if e := f.service.BindCandidateService(productionAwaiting(f), 4001, f.endpoint); e == nil || e.Code != "local_engine_service_reused" {
				t.Fatal(e)
			}
			p.dispatch(previewLocalBinding{taskID: previewAwaiting(p), pid: 4001, endpoint: f.endpoint})
			if p.state.failure == nil || p.state.failure.Code != "local_engine_service_reused" {
				t.Fatal(p.state.failure)
			}
			assertLocalFrames(t, f, p, waiting)
			if scenario == "cancel_waiting" {
				if e := cancelBenchmarkRuns(t.Context(), f.service.RunTasks, f.service.CancelRun); e != nil {
					t.Fatal(e)
				}
				f.cancel()
				p.state.newBenchmark()
				p.advance(time.Second)
				for _, task := range f.service.RunTasks() {
					if !terminal(task.State) {
						t.Fatal(task)
					}
				}
				for _, task := range p.state.tasks {
					if !terminal(task.State) {
						t.Fatal(task)
					}
				}
				if len(f.frame().Report) != 0 || p.state.task.report != "" {
					t.Fatal("cancelled queue produced a report")
				}
				return
			}
			if scenario == "second_binding_failure_retry" {
				f.mu.Lock()
				f.failSecond = true
				f.mu.Unlock()
				p.failNext = true
			}
			if e := f.service.BindCandidateService(productionAwaiting(f), 4002, f.endpoint); e != nil {
				t.Fatal(e)
			}
			p.dispatch(previewLocalBinding{taskID: previewAwaiting(p), pid: 4002, endpoint: f.endpoint})
			if scenario == "second_binding_failure_retry" {
				waitLocalParity(t, func() bool { return f.frame().Failure != "" }, func() { time.Sleep(10 * time.Millisecond) })
				waitLocalParity(t, func() bool { return previewLocalFrame(p).Failure != "" }, func() { p.advance(100 * time.Millisecond) })
				assertLocalFrames(t, f, p, localParityFrame{Stage: "run", Completed: []string{"zh-core"}, Report: []string{}, Failure: "cli_blocked", Retry: true})
				f.mu.Lock()
				f.failSecond = false
				f.mu.Unlock()
				f.start()
				p.state.retryBenchmark()
				waitLocalParity(t, func() bool { return f.frame().Awaiting != "" }, func() { time.Sleep(10 * time.Millisecond) })
				waitLocalParity(t, func() bool { return previewLocalFrame(p).Awaiting != "" }, func() { p.advance(100 * time.Millisecond) })
				assertLocalFrames(t, f, p, waiting)
				if e := f.service.BindCandidateService(productionAwaiting(f), 4003, f.endpoint); e != nil {
					t.Fatal(e)
				}
				p.dispatch(previewLocalBinding{taskID: previewAwaiting(p), pid: 4003, endpoint: f.endpoint})
			}
			close(f.release["zh-svg-pelican"])
			waitLocalParity(t, func() bool { return f.frame().Stage == "done" }, func() { time.Sleep(10 * time.Millisecond) })
			waitLocalParity(t, func() bool { return previewLocalFrame(p).Stage == "done" }, func() { p.advance(100 * time.Millisecond) })
			assertLocalFrames(t, f, p, localParityFrame{Stage: "done", Completed: []string{"zh-core", "zh-svg-pelican"}, Report: []string{"zh-core", "zh-svg-pelican"}})
			if p.state.service != nil {
				t.Fatal("simulator created real service")
			}
		})
	}
}

func TestPreviewLocalParityDetectsReusedServiceMutation(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "引擎 · LM Studio 手动绑定"))
	p.state.startBenchmark()
	waitLocalParity(t, func() bool { return previewAwaiting(p) != "" }, func() { p.advance(100 * time.Millisecond) })
	want := localParityFrame{Stage: "run", Awaiting: "zh-svg-pelican", Completed: []string{"zh-core"}, Report: []string{}, Continue: true}
	if !reflect.DeepEqual(previewLocalFrame(p), want) {
		t.Fatal(previewLocalFrame(p))
	}
	// Isolated fault: bypass the fresh-service gate and reuse the first daemon.
	p.startLocalBinding(4001)
	p.advance(400 * time.Millisecond)
	if reflect.DeepEqual(previewLocalFrame(p), want) {
		t.Fatal("the independent expectation missed service reuse")
	}
}

func TestPreviewLocalDirectFamiliesAndRegistrationIsolation(t *testing.T) {
	for _, family := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang"} {
		t.Run(family, func(t *testing.T) {
			p := newPreviewSession(t.Context())
			p.loadScene(previewSceneIndex(t, "引擎 · 本地安装与目录模型"))
			p.state.engine = family + " · 模拟安装"
			if family == "llama-cpp" {
				p.state.model = "local.gguf"
			}
			p.state.startBenchmark()
			p.advance(2 * time.Second)
			first := p.state.engineState.Pid
			p.advance(7 * time.Second)
			second := p.state.engineState.Pid
			if first <= 0 || second <= 0 || first == second {
				t.Fatal("per-scope service was reused", first, second)
			}
			p.advance(10 * time.Second)
			if got := previewLocalFrame(p); !reflect.DeepEqual(got, localParityFrame{Stage: "done", Completed: []string{"zh-core", "zh-svg-pelican"}, Report: []string{"zh-core", "zh-svg-pelican"}}) {
				t.Fatal(got)
			}
			p.state.changeInstallation("register", "/preview/additional-mlx-lm.json")
			if p.state.service != nil || !slices.Contains(p.state.settings.Settings.EngineInstallations, "/preview/additional-mlx-lm.json") {
				t.Fatal("registration escaped simulator")
			}
			p.state.changeInstallation("remove", "/preview/additional-mlx-lm.json")
			if slices.Contains(p.state.settings.Settings.EngineInstallations, "/preview/additional-mlx-lm.json") {
				t.Fatal("registration removal failed")
			}
		})
	}
}
