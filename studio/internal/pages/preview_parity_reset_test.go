//go:build ui_debug && !release

package pages

import (
	"context"
	"reflect"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

// Switching the workspace root drops the Reports tab and the open detail in
// both paths, because that data belonged to the previous workspace. Production
// reaches resetWorkspace from applySettings; preview must do the same from its
// simulated Settings 保存.
func TestWorkspaceSwitchResetsReportsTabParity(t *testing.T) {
	previous, next := "/previous/workspace", "/next/workspace"
	production := testState()
	production.settings = core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr(previous)},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(previous)},
	}
	production.loadSettings(production.settings)
	production.reportsTab, production.openedReport = reportsTabHistory, &reportViewState{reference: "gone"}
	production.applySettings(core.SettingsResponse{
		Settings:  core.UserSettings{WorkspaceRoot: ptr(next)},
		Effective: core.EffectiveSettings{WorkspaceRoot: ptr(next)},
	})

	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "设置 · 路径与目录名"))
	p.state.reportsTab, p.state.openedReport = reportsTabHistory, &reportViewState{reference: "gone"}
	p.state.workspace = next
	tt := previewTester(p, 1120, 1600)
	click(t, tt, "保存")

	for name, s := range map[string]*viewState{"production": &production, "preview": p.state} {
		if s.reportsTab != reportsTabList || s.openedReport != nil {
			t.Errorf("%s: tab=%d opened=%v", name, s.reportsTab, s.openedReport != nil)
		}
	}
	if p.state.settings.Effective.WorkspaceRoot == nil || *p.state.settings.Effective.WorkspaceRoot != next {
		t.Error("preview did not save the new workspace root")
	}
}

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
		})
	}
}
