//go:build ui_debug && !release

package pages

import (
	"context"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func localParityEngineEndpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestLocalParityEngineHelper(t *testing.T) {
	if !slices.Contains(os.Args, "--local-parity-engine") {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) })
	if err := http.ListenAndServe("127.0.0.1:"+localParityFlag(os.Args, "--port"), mux); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewLocalCancelStopsOnlyOwnedServiceParity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	for _, family := range []string{"mlx-lm", "lmstudio"} {
		t.Run(family, func(t *testing.T) {
			var f *localParityService
			if family == "lmstudio" {
				f = newLocalParityService(t)
			} else {
				f = newLocalParityService(t, family)
			}
			f.release["zh-core"] = make(chan struct{})
			f.start()
			waitLocalParity(t, func() bool {
				for _, task := range f.service.RunTasks() {
					if task.State == "running" {
						return true
					}
				}
				return false
			}, func() { time.Sleep(10 * time.Millisecond) })
			p := newPreviewSession(t.Context())
			scene := "引擎 · 本地安装与目录模型"
			if family == "lmstudio" {
				scene = "引擎 · LM Studio 手动绑定"
			}
			p.loadScene(previewSceneIndex(t, scene))
			p.state.startBenchmark()
			p.advance(2 * time.Second)
			if p.state.running() == nil {
				t.Fatal("preview run never started")
			}
			tt := ui.NewTester(p.state.runView, 1120, 900)
			if err := tt.Click("取消运行"); err != nil {
				t.Fatal(err, tt.Texts())
			}
			resetCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			awaitBenchmarkReset(resetCtx, f.service.RunTasks, f.service.CancelRun, func(apply func()) { apply() }, func(e *core.ApiError) bool {
				if e != nil {
					t.Fatal(e)
				}
				return true
			})
			p.advance(time.Second)
			waitLocalParity(t, func() bool { return f.service.Engine().State == "stopped" }, func() { time.Sleep(10 * time.Millisecond) })
			type cancelledFrame struct {
				Engine string
				States []string
				Report bool
			}
			frame := func(engine string, tasks []core.RunTask, report bool) cancelledFrame {
				result := cancelledFrame{Engine: engine, Report: report}
				for _, task := range tasks {
					result.States = append(result.States, emptyText(task.Bundle)+":"+task.State)
				}
				slices.Sort(result.States)
				return result
			}
			production := frame(f.service.Engine().State, f.service.RunTasks(), len(f.frame().Report) > 0)
			preview := frame(p.state.engineState.State, p.state.tasks, p.state.task.report != "")
			want := cancelledFrame{Engine: "stopped", States: []string{"zh-core:cancelled", "zh-svg-pelican:cancelled"}}
			for name, got := range map[string]cancelledFrame{"production": production, "preview": preview} {
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: %+v want %+v", name, got, want)
				}
			}
			if !reflect.DeepEqual(production, preview) {
				t.Error("normal cancellation diverged", production, preview)
			}
		})
	}
}

func TestPreviewNewTaskDuringReportKeepsIdleLocalService(t *testing.T) {
	p := newPreviewSession(t.Context())
	p.loadScene(previewSceneIndex(t, "引擎 · 本地安装与目录模型"))
	p.state.startBenchmark()
	waitLocalParity(t, func() bool { return p.state.task.stage == "report" }, func() { p.advance(100 * time.Millisecond) })
	if p.state.engineState.State != "running" || !p.state.busy {
		t.Fatal("fixture did not reach report preparation")
	}
	for _, task := range p.state.tasks {
		if task.State != "done" {
			t.Fatal(task)
		}
	}
	p.state.newBenchmark()
	p.advance(time.Second)
	if p.state.engineState.State != "running" || p.state.task.report != "" {
		t.Fatal("reset stopped an idle service or published the cancelled report", p.state.engineState)
	}
}
