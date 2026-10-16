package pages

import (
	"context"
	"inferyard/studio/internal/core"
	"time"
)

func (s *viewState) resetWorkspace() {
	s.clearBenchmark()
	s.deviceChecked = ""
	s.autoBind = bindingArm{}
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
	s.groups = nil
	s.reportChoices.Clear()
	s.denied = map[string]bool{}
}

type benchmarkTask struct {
	stage   string
	start   startChainResult
	bundles []string
	runs    []string
	report  string
	failure *core.ApiError
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
	if s.newTaskPending {
		return
	}
	if s.taskCancel != nil {
		s.taskCancel()
	}
	s.autoBind = bindingArm{}
	s.newTaskPending = true
	finish := func() {
		s.clearBenchmark()
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

	go func() {
		for {
			e := cancelBenchmarkRuns(s.ctx, s.service.RunTasks, s.service.CancelRun)
			if s.ctx.Err() != nil {
				return
			}
			done := false
			s.win.Update(func() {
				if e != nil {
					s.newTaskPending = false
					s.failure = e
					done = true
					return
				}
				if !s.busy {
					finish()
					done = true
				}
			})
			if done || !taskWait(s.ctx) {
				return
			}
		}
	}()

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
					return e
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
