package pages

import (
	"context"
	"strconv"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type bindingJob struct {
	bundle, candidate string
}

func (s *viewState) bindingPhase() bool {
	if s.progress == nil || s.progress.DefaultScopeComplete || s.running() != nil {
		return false
	}
	if s.progress.NextAction != "bind" && s.progress.NextAction != "run" {
		return false
	}
	return len(bindingJobs(selectedProgressForTask(s))) > 0
}

func bindingJobs(p *inferyard.WorkspaceProgress) []bindingJob {
	var jobs []bindingJob
	if p != nil {
		for _, b := range p.Bundles {
			if b.Phase == "bound_missing" {
				jobs = append(jobs, bindingJob{b.Bundle, emptyText(b.CandidatePath)})
			}
		}
	}
	return jobs
}

func (s *viewState) bindDiscovered(pid int64, endpoint string) {
	if s.simulate(previewBind) {
		return
	}
	if s.busy || !s.bindingPhase() || pid <= 0 || endpoint == "" {
		return
	}
	workspace, jobs := s.progress.Workspace, bindingJobs(selectedProgressForTask(s))
	s.binding = true
	s.stepConnection()
	ctx, cancel := context.WithCancel(s.ctx)
	s.taskCancel = cancel
	s.guideWork(func() (string, *core.ApiError) {
		defer cancel()
		e := runBindingChain(ctx, workspace, jobs, pid, endpoint, s.service.Progress, s.service.WizardExecute,
			func(bundle string) {
				s.updateView(func() {
					s.bindingBundle = bundle
					s.markServiceUsed(pid)
				})
			})
		s.updateView(func() {
			s.binding, s.bindingBundle = false, ""
			s.bindingOutcome(e)
		})
		if e != nil {
			return "", e
		}
		return "全部待连接的测评题集已连接并核验，可开始运行。", nil
	})
}

// bindingOutcome applies a connection result. A rejection is attributed to the
// bind step; a success clears the stale bind stage and failure, so refreshed
// workspace progress decides the step instead of the old failure.
func (s *viewState) bindingOutcome(e *core.ApiError) {
	if e != nil {
		s.failBinding(e)
		return
	}
	s.clearRecoveredBinding()
}

// markServiceUsed records a connected LM Studio service so the handoff form
// never offers it again.
func (s *viewState) markServiceUsed(pid int64) {
	if pid <= 0 {
		return
	}
	if s.localUsedPIDs == nil {
		s.localUsedPIDs = map[int64]bool{}
	}
	s.localUsedPIDs[pid] = true
}

// Re-read each candidate's phase and command before binding; never reuse the
// first bundle's output or derive eligibility from discovery/health results.
func runBindingChain(ctx context.Context, workspace string, jobs []bindingJob, pid int64, endpoint string,
	progress func(context.Context) (core.ProgressResponse, *core.ApiError),
	execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError),
	announce func(string),
) *core.ApiError {
	for _, job := range jobs {
		if ctx.Err() != nil {
			return core.Err("cancelled")
		}
		current, e := progress(ctx)
		if e != nil {
			return e
		}
		p := current.Progress
		if current.ExitCode != 0 || p == nil || p.Workspace != workspace ||
			(p.NextAction != "bind" && p.NextAction != "run") {
			return core.Err("binding_progress_changed")
		}
		var command string
		for _, b := range p.Bundles {
			if b.Bundle == job.bundle && b.Phase == "bound_missing" && b.NextAction == "bind" &&
				emptyText(b.CandidatePath) == job.candidate && job.candidate != "" {
				if _, err := inferyard.SplitCommand(emptyText(b.NextCommand)); err != nil {
					return core.Err("binding_progress_changed")
				}
				// Reuse the same bind command builder as advanced manual binding.
				one := *p
				one.Bundles = []inferyard.BundleProgress{b}
				view := viewState{progress: &one, candidate: job.candidate}
				command, e = view.bindCommand(strconv.FormatInt(pid, 10), endpoint)
				if e != nil {
					return e
				}
				break
			}
		}
		if command == "" {
			return core.Err("binding_progress_changed")
		}
		announce(job.bundle)
		r, e := execute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return e
		}
		if r.ExitCode != 0 {
			return wizardFailure(r)
		}
	}
	return nil
}

func wizardFailure(r core.WizardResult) *core.ApiError {
	message := human(string(r.Status))
	if len(r.Limitations) > 0 {
		message = human(r.Limitations[0])
	}
	return &core.ApiError{Code: string(r.Status), Message: message, Limitations: r.Limitations}
}

func selectedProgressForTask(s *viewState) *inferyard.WorkspaceProgress {
	if len(s.task.bundles) > 0 {
		return selectedProgress(s.progress, s.task.bundles)
	}
	return s.progress
}
