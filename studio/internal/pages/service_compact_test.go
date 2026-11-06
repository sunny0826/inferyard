package pages

import (
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func compactRunFixture() *viewState {
	s := testState()
	s.ready.Workspace.Initialized, s.ready.Results.Exists, s.ready.Models.Exists = true, true, true
	s.progress = &inferyard.WorkspaceProgress{NextAction: "run", Bundles: []inferyard.BundleProgress{{
		Bundle: "zh-core", Phase: "run_incomplete", NextAction: "run",
		CandidatePath: ptr("/fixture/candidate"), BoundPath: ptr("/fixture/bound"),
	}}}
	s.engineState = core.EngineState{State: "running", Pid: 42, Endpoint: "http://127.0.0.1:48857"}
	s.tasks = []core.RunTask{{TaskId: "fixture", State: "running", RunPath: ptr("/fixture/run")}}
	s.runProgress = &core.RunProgress{Done: 2, Total: ptr(int64(120))}
	return &s
}
