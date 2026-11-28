package pages

import (
	"context"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func (s *viewState) resetWorkspace() {
	s.clearBenchmark()
	s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	s.deviceChecked = ""
	s.model, s.registry = "", core.Registry{}
	s.initDir, s.engine, s.process, s.bindEndpoint, s.candidate = "", "", "", "", ""
	s.bindingStale = false
	s.engineState, s.startup, s.processes = core.EngineState{}, core.Startup{}, nil
	s.cliInstallStatus, s.refreshFailure, s.refreshFailureCode = "", "", ""
	s.cleanArtifactsConfirm = false
	s.runProgress, s.progress, s.resumeSource, s.detail = nil, nil, nil, nil
	s.endpoint, s.pid, s.detailID = "", "", ""
	s.detailMu.Lock()
	s.requestedDetail = ""
	s.detailMu.Unlock()
	s.reportChoices.Clear()
	s.reportDialogOpen, s.reportDiscardConfirm = false, false
	s.resumeDiscardConfirm = false
	s.settingsLeaveConfirm, s.pendingNavAction = false, nil
	s.benchmarksTab = benchmarksTabTasks
	s.historyQuery, s.historyStatusFilter, s.historyDetailPath = "", "", ""
	s.reportsTab = reportsTabList
	s.nodes.unifiedReports, s.nodes.reportsLoaded = nil, false
	s.resetReportCompare()
	s.settingsTab = settingsTabGeneral
	s.closeOpenedReport()
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
	s.skipAutoModelAdopt = false
	s.lastCandidateEngine, s.lastCandidateInit = "", false
	s.selectedBundles = nil
	s.focusedRun = ""
	s.newTaskPending = false
	s.bindFailure, s.failedBundle, s.handoffFailure = nil, "", nil
	s.localUsedPIDs, s.localPendingPIDs = nil, nil
	s.localBindingTask, s.localPID = "", ""
}

func (s *viewState) checkedBundles() []string {
	var names []string
	if s.progress == nil {
		return names
	}
	if s.selectedBundles == nil {
		s.selectedBundles = map[string]bool{}
		selectedCount := 0
		for _, b := range s.progress.Bundles {
			if b.Bundle != "" && selectedCount < 2 {
				s.selectedBundles[b.Bundle] = true
				selectedCount++
			} else {
				s.selectedBundles[b.Bundle] = false
			}
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
	s.newBenchmarkWithSelection("", "")
}

func (s *viewState) newBenchmarkWithSelection(engine, model string) {
	s.pendingDestructive = destructiveRequest{}
	s.benchmarksTab = benchmarksTabTasks
	s.historyDetailPath = ""
	if s.speed != nil {
		s.speed.selected = false
	}
	if s.preview != nil {
		s.preview.resetTask(model, engine)
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
		s.engine, s.model = engine, model
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
		if s.openedReport != nil {
			s.openedReport.releaseSVGStage()
		}
		s.selected = "benchmarks"
		s.benchFlowOpen = true
	}
	if s.service == nil && s.cancelRound == nil {
		finish()
		return
	}
	tasks := func() []core.RunTask { return s.runTasksAll() }
	update := s.win.Update
	if s.updateWindow != nil {
		update = s.updateWindow
	}

	go awaitBenchmarkReset(s.ctx, tasks, s.cancelRoundTask, update, func(e *core.ApiError) bool {
		if e != nil {
			s.newTaskPending = false
			s.failure = e
			return true
		}
		if benchmarkResetReady(s.busy, tasks()) {
			finish()
			return true
		}
		return false
	})

}

func (s *viewState) runTasksAll() []core.RunTask {
	if s.runTasks != nil {
		return s.runTasks()
	}
	if s.cancelRound != nil {
		return cloneTasks(s.tasks)
	}
	if s.service == nil {
		return nil
	}
	return s.service.RunTasks()
}

func cloneTasks(tasks []core.RunTask) []core.RunTask {
	out := make([]core.RunTask, len(tasks))
	copy(out, tasks)
	return out
}

func (s *viewState) cancelRoundTask(id string) (core.RunTask, *core.ApiError) {
	if s.cancelRound != nil {
		return s.cancelRound(id)
	}
	if s.service == nil {
		return core.RunTask{}, core.Err("internal_error")
	}
	return s.service.CancelRun(id)
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
