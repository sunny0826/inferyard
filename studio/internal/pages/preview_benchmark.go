//go:build ui_debug && !release

package pages

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func (p *previewSession) beginBenchmark(op previewBenchmark) {
	s := p.state
	if s.busy || s.running() != nil || len(op.bundles) == 0 || op.model == "" || op.engine == "" || !s.preflightReady() {
		return
	}
	fail := p.takeFailure()
	op.bundles = slices.Clone(op.bundles)
	p.cancelEvents("task")
	s.model, s.engine = op.model, op.engine
	s.busy, s.failure = true, nil
	s.task = benchmarkTask{stage: "prepare_candidate", bundles: slices.Clone(op.bundles)}
	s.progress.DefaultScopeComplete = false
	s.progress.Report = inferyard.ProgressReport{}
	for i := range s.progress.Bundles {
		b := &s.progress.Bundles[i]
		if slices.Contains(op.bundles, b.Bundle) {
			b.Phase, b.NextAction, b.Runs = "candidate_missing", "prepare_candidate", nil
		}
	}
	p.after("task", 400*time.Millisecond, func() {
		s.task.stage, s.progress.NextAction = "bind", "bind"
		s.candidate = filepath.Join(s.workspace, "candidates", p.nextID("candidate"))
		s.task.start = startChainResult{candidate: s.candidate, workspace: s.workspace}
		s.engineState = core.EngineState{State: "starting", Pid: 42, Candidate: s.candidate, Endpoint: "http://127.0.0.1:48857"}
		for i := range s.progress.Bundles {
			b := &s.progress.Bundles[i]
			if slices.Contains(op.bundles, b.Bundle) {
				b.Phase, b.NextAction, b.CandidatePath = "bound_missing", "bind", previewValue(s.candidate)
			}
		}
		p.after("task", 800*time.Millisecond, func() {
			if fail {
				s.busy = false
				s.task.failure = core.Err("engine_start_failed")
				s.failure, s.engineState.Error = s.task.failure, s.task.failure
				s.engineState.State = "failed"
				return
			}
			s.engineState.State = "running"
			p.startRun(0, op, p.queueRuns(op))
		})
	})
}

func (p *previewSession) queueRuns(op previewBenchmark) []string {
	var ids []string
	for _, bundle := range op.bundles {
		id := p.nextID("run")
		task := core.RunTask{TaskId: id, Kind: "run", State: "starting", Queued: true, Bundle: previewValue(bundle), CreatedAt: p.timestamp()}
		if op.resumePath != "" {
			task.Kind, task.ResumeFrom = "resume", previewValue(op.resumePath)
		}
		p.state.tasks = append(p.state.tasks, task)
		p.details[id] = &core.RunTaskDetail{Task: task}
		ids = append(ids, id)
	}
	return ids
}

func (p *previewSession) startRun(bundleIndex int, op previewBenchmark, ids []string) {
	s := p.state
	if bundleIndex >= len(op.bundles) {
		p.finishBenchmark()
		return
	}
	bundle := op.bundles[bundleIndex]
	s.task.stage, s.progress.NextAction = "run", "run"
	id := ids[bundleIndex]
	path := filepath.Join(s.results, id)
	task := p.details[id].Task
	task.State, task.Queued = "running", false
	task.RunId, task.RunPath, task.StartedAt = previewValue(id), previewValue(path), previewValue(p.timestamp())
	for i := range s.tasks {
		if s.tasks[i].TaskId == id {
			s.tasks[i] = task
			break
		}
	}
	s.runProgress = &core.RunProgress{Phase: previewValue("formal"), Total: previewValue(int64(120)), CurrentCaseId: previewValue("preview-case-001")}
	p.details[id] = &core.RunTaskDetail{Task: task, Progress: s.runProgress}
	s.history.StandaloneRuns = append(s.history.StandaloneRuns, core.RunSummary{RunId: id, Path: path, ModelName: previewValue(op.model), Engine: previewValue(op.engine), Bundle: previewValue(bundle), Status: "partial", StartedAt: task.StartedAt})
	for i := range s.progress.Bundles {
		b := &s.progress.Bundles[i]
		if b.Bundle == bundle {
			b.Phase, b.NextAction = "run_incomplete", "run"
			b.BoundPath = previewValue(filepath.Join(s.workspace, "bound", id))
		}
	}
	p.tickRun(id, bundleIndex, op, ids)
}

func (p *previewSession) tickRun(id string, bundleIndex int, op previewBenchmark, ids []string) {
	p.after("task", 500*time.Millisecond, func() {
		s := p.state
		detail := p.details[id]
		if detail == nil || detail.Task.State != "running" {
			return
		}
		progress := detail.Progress
		end := min(progress.Done+10, *progress.Total)
		for i := progress.Done; i < end; i++ {
			progress.Cases = append(progress.Cases, core.CaseProgress{CaseId: fmt.Sprintf("preview-case-%03d", i+1), State: "completed", Category: previewValue("模拟分类"), QualityState: previewValue("pass"), DurationMs: previewValue(1240.0)})
		}
		progress.Done, progress.LastSeq = end, end
		progress.CurrentCaseId = previewValue(fmt.Sprintf("preview-case-%03d", min(end+1, *progress.Total)))
		detail.Resources = append(detail.Resources, core.ResourceSample{Utc: p.timestamp(), MemAvailableBytes: previewValue(int64(10<<30) - end*(1<<20)), ServiceRssBytes: previewValue(int64(2<<30) + end*(1<<20)), ServiceCpuPercent: previewValue(120.0 + float64(end%40))})
		if end < *progress.Total {
			p.tickRun(id, bundleIndex, op, ids)
			return
		}
		p.terminalRun(id, "done")
		s.task.runs = append(s.task.runs, emptyText(detail.Task.RunPath))
		for i := range s.progress.Bundles {
			b := &s.progress.Bundles[i]
			if b.Bundle == emptyText(detail.Task.Bundle) {
				b.Phase, b.NextAction = "run_complete", "none"
			}
		}
		p.startRun(bundleIndex+1, op, ids)
	})
}

func (p *previewSession) terminalRun(id, state string) {
	s := p.state
	for i := range s.tasks {
		task := &s.tasks[i]
		if task.TaskId != id {
			continue
		}
		task.State, task.FinishedAt, task.Queued = state, previewValue(p.timestamp()), false
		if detail := p.details[id]; detail != nil {
			detail.Task = *task
		}
		for j := range s.history.StandaloneRuns {
			run := &s.history.StandaloneRuns[j]
			if run.RunId == id {
				run.FinishedAt, run.Status = task.FinishedAt, state
				if state == "done" {
					run.Status, run.Sealed = "complete", true
					run.Completeness = previewValue("complete")
				} else if state == "cancelled" {
					run.StopReason = previewValue("user_cancelled")
				}
			}
		}
	}
}

func (p *previewSession) finishBenchmark() {
	s := p.state
	s.task.stage, s.progress.NextAction = "report", "report"
	p.after("task", 600*time.Millisecond, func() {
		s.task.report = p.createReport(s.task.runs)
		s.task.stage, s.progress.NextAction, s.busy = "done", "done", false
		s.progress.DefaultScopeComplete = true
		for _, b := range s.progress.Bundles {
			if b.Phase != "run_complete" {
				s.progress.DefaultScopeComplete = false
			}
		}
		s.progress.Report.Path = previewValue(s.task.report)
		s.progress.Report.Status = "unverified"
	})
}

func (p *previewSession) cancelRun(id string) {
	s := p.state
	for i := range s.tasks {
		if s.tasks[i].TaskId == id && !terminal(s.tasks[i].State) {
			p.resetTask(s.model, s.engine)
			return
		}
	}
}

func (p *previewSession) resetTask(model, engine string) {
	s := p.state
	if s.newTaskPending {
		return
	}
	p.cancelEvents("task")
	p.cancelEvents("service")
	p.cancelEvents("environment")
	p.cancelEvents("report")
	pending := s.running() != nil || s.busy
	for i := range s.tasks {
		task := &s.tasks[i]
		if !terminal(task.State) {
			task.State = "cancelling"
			if detail := p.details[task.TaskId]; detail != nil {
				detail.Task = *task
			}
		}
	}
	finish := func() {
		for _, task := range s.tasks {
			if task.State == "cancelling" {
				p.terminalRun(task.TaskId, "cancelled")
			}
		}
		s.clearBenchmark()
		s.failure, s.busy = nil, false
		s.selected, s.detailID, s.focusedRun = "dashboard", "", ""
		s.detail, s.resumeSource, s.runProgress, s.openedReport = nil, nil, nil, nil
		s.model, s.engine = model, engine
		if s.engineState.State == "starting" || s.engineState.State == "stopping" {
			s.engineState = core.EngineState{State: "stopped"}
		}
		if s.progress != nil {
			s.task.stage, s.progress.NextAction = "prepare_candidate", "prepare_candidate"
			s.progress.DefaultScopeComplete = false
			for i := range s.progress.Bundles {
				b := &s.progress.Bundles[i]
				b.Phase, b.NextAction, b.Runs = "candidate_missing", "prepare_candidate", nil
			}
		}
	}
	if pending {
		s.newTaskPending = true
		p.after("task", 400*time.Millisecond, finish)
	} else {
		finish()
	}
}
