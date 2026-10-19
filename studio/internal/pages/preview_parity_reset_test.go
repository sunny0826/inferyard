//go:build ui_debug && !release

package pages

import (
	"context"
	"reflect"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestPreviewResetLeavesNextTaskAloneParity(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		initial []core.RunTask
		want    []string
	}{
		{"idle", nil, []string{"next-specialty:starting"}},
		{"active_and_queued", []core.RunTask{
			{TaskId: "completed", State: "done"},
			{TaskId: "active", State: "running"},
			{TaskId: "waiting", State: "starting", Queued: true},
		}, []string{"completed:done", "active:cancelled", "waiting:cancelled", "next-specialty:starting"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			production := productionResetThenNextTask(t, scenario.initial)
			p := newPreviewSession(context.Background())
			p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
			p.state.tasks = append([]core.RunTask(nil), scenario.initial...)
			p.state.newBenchmark()
			p.advance(time.Second)
			if p.state.newTaskPending || p.state.busy {
				t.Fatal("preview reset did not finish")
			}
			p.state.tasks = append(p.state.tasks, core.RunTask{TaskId: "next-specialty", State: "starting"})
			p.advance(time.Second)
			observe := func(tasks []core.RunTask) []string {
				var states []string
				for _, task := range tasks {
					state := task.State
					if task.Queued {
						state += ":queued"
					}
					states = append(states, task.TaskId+":"+state)
				}
				return states
			}
			real, simulated := observe(production), observe(p.state.tasks)
			for name, got := range map[string][]string{"production": real, "preview": simulated} {
				if !reflect.DeepEqual(got, scenario.want) {
					t.Errorf("%s: got %v, want %v", name, got, scenario.want)
				}
			}
			if !reflect.DeepEqual(real, simulated) {
				t.Errorf("reset behavior drift: %v / %v", real, simulated)
			}
		})
	}
}
