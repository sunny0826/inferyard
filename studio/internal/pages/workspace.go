package pages

import (
	"context"
	"inferyard/studio/internal/core"
	"time"
)

func (s *viewState) resetWorkspace() {
	s.clearBenchmark()
	s.speed = &speedWorkspace{context: 65536}
	s.deviceChecked = ""
	s.model, s.registry = "", core.Registry{}
	s.initDir, s.engine, s.process, s.bindEndpoint, s.candidate = "", "", "", "", ""
	s.assetsPreset, s.bindingStale, s.bindingUnknown = false, false, false
	s.engineState, s.startup, s.processes = core.EngineState{}, core.Startup{}, nil
	s.cliInstallStatus, s.refreshFailure = "", ""
	s.runProgress, s.progress, s.resumeSource, s.detail = nil, nil, nil, nil
	s.endpoint, s.pid, s.detailID = "", "", ""
	s.detailMu.Lock()
	s.requestedDetail = ""
	s.detailMu.Unlock()
	s.reportChoices.Clear()
	s.denied = map[string]bool{}
}

type benchmarkTask struct {
	lastVerify time.Time
	stage      string
	bindings   *benchmarkBindings
	start      startChainResult
	bundles    []string
	runs       []string
	report     string
	failure    *core.ApiError
}

func (s *viewState) clearBenchmark() {
	if s.taskCancel != nil {
		s.taskCancel()
		s.taskCancel = nil
	}
	s.task = benchmarkTask{}
	s.selectedBundles = nil
	s.focusedRun = ""
	s.newTaskPending = false
}

func (s *viewState) checkedBundles() []string {
	var names []string
	if s.progress == nil {
		return names
	}
	if s.selectedBundles == nil {
		s.selectedBundles = map[string]bool{}
		for _, b := range s.progress.Bundles {
			s.selectedBundles[b.Bundle] = b.Bundle == "zh-core"
		}
	}
	for _, b := range s.progress.Bundles {
		if s.selectedBundles[b.Bundle] {
			names = append(names, b.Bundle)
		}
	}
	return names
}

func (s *viewState) newBenchmark() {
	if s.preview != nil {
		s.preview.resetTask(s.model, s.engine)
		return
	}
	if s.newTaskPending {
		return
	}
	if s.taskCancel != nil {
		s.taskCancel()
	}
	s.newTaskPending = true
	finish := func() {
		s.clearBenchmark()
		if s.speed != nil {
			s.speed.current, s.speed.opened = nil, nil
		}
		s.task.stage = "prepare_candidate"
		s.detailID, s.focusedRun = "", ""
		s.resumeSource = nil
		s.detailMu.Lock()
		s.requestedDetail = ""
		s.detailMu.Unlock()
		s.failure = nil
		s.selected = "dashboard"
	}
	if s.service == nil {
		finish()
		return
	}

	go awaitBenchmarkReset(s.ctx, s.service.RunTasks, s.service.CancelRun, s.win.Update, func(e *core.ApiError) bool {
		if e != nil {
			s.newTaskPending = false
			s.failure = e
			return true
		}
		if benchmarkResetReady(s.busy, s.service.RunTasks()) {
			finish()
			return true
		}
		return false
	})

}

func cancelBenchmarkRuns(ctx context.Context, tasks func() []core.RunTask, cancel func(string) (core.RunTask, *core.ApiError)) *core.ApiError {
	cancelled := map[string]bool{}
	for {
		pending := false
		for _, task := range tasks() {
			if terminal(task.State) {
				continue
			}
			pending = true
			if !cancelled[task.TaskId] {
				if _, e := cancel(task.TaskId); e != nil {
					ended := false
					for _, latest := range tasks() {
						if latest.TaskId == task.TaskId && terminal(latest.State) {
							ended = true
							break
						}
					}
					if !ended {
						return e
					}
				}
				cancelled[task.TaskId] = true
			}
		}
		if !pending {
			return nil
		}
		if !taskWait(ctx) {
			return core.Err("cancelled")
		}
	}

}

func taskWait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(100 * time.Millisecond):
		return true
	}
}

func benchmarkResetReady(busy bool, tasks []core.RunTask) bool {
	if busy {
		return false
	}
	for _, task := range tasks {
		if !terminal(task.State) {
			return false
		}
	}
	return true
}

func awaitBenchmarkReset(ctx context.Context, tasks func() []core.RunTask, cancel func(string) (core.RunTask, *core.ApiError), update func(func()), finish func(*core.ApiError) bool) {
	for {
		e := cancelBenchmarkRuns(ctx, tasks, cancel)
		if ctx.Err() != nil {
			return
		}
		completed := make(chan bool, 1)
		update(func() {
			if ctx.Err() != nil {
				completed <- true
				return
			}
			completed <- finish(e)
		})
		select {
		case done := <-completed:
			if done {
				return
			}
		case <-ctx.Done():
			return
		}
		if !taskWait(ctx) {
			return
		}
	}
}
