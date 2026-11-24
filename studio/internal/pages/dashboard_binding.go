package pages

import (
	"context"
	"strconv"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
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
		failedBundle, e := runBindingChain(ctx, workspace, jobs, pid, endpoint, s.service.Progress, s.service.WizardExecute,
			func(bundle string) {
				s.updateView(func() { s.bindingBundle = bundle })
			})
		s.updateView(func() {
			s.binding, s.bindingBundle = false, ""
			s.applyBinding(failedBundle, e, pid)
		})
		if e != nil {
			return "", e
		}
		return l10n.TR("dashboard.toast.all_bundles_bound"), nil
	})
}

func (s *viewState) applyBinding(failedBundle string, e *core.ApiError, pid int64) {
	s.bindingOutcome(failedBundle, e)
	if e == nil {
		s.markServiceUsed(pid)
	}
}

func (s *viewState) bindingOutcome(failedBundle string, e *core.ApiError) {
	if e != nil {
		s.failedBundle = failedBundle
		s.failBinding(e)
		return
	}
	s.failedBundle = ""
	s.clearRecoveredBinding()
}

func (s *viewState) markServiceUsed(pid int64) {
	if pid <= 0 {
		return
	}
	if s.localUsedPIDs == nil {
		s.localUsedPIDs = map[int64]bool{}
	}
	s.localUsedPIDs[pid] = true
}

func runBindingChain(ctx context.Context, workspace string, jobs []bindingJob, pid int64, endpoint string,
	progress func(context.Context) (core.ProgressResponse, *core.ApiError),
	execute func(context.Context, core.WizardRequest) (core.WizardResult, *core.ApiError),
	announce func(string),
) (string, *core.ApiError) {
	for _, job := range jobs {
		if ctx.Err() != nil {
			return "", core.Err("cancelled")
		}
		current, e := progress(ctx)
		if e != nil {
			return job.bundle, e
		}
		p := current.Progress
		if current.ExitCode != 0 || p == nil || p.Workspace != workspace ||
			(p.NextAction != "bind" && p.NextAction != "run") {
			return job.bundle, core.Err("binding_progress_changed")
		}
		var command string
		for _, b := range p.Bundles {
			if b.Bundle == job.bundle && b.Phase == "bound_missing" && b.NextAction == "bind" &&
				emptyText(b.CandidatePath) == job.candidate && job.candidate != "" {
				if _, err := inferyard.SplitCommand(emptyText(b.NextCommand)); err != nil {
					return job.bundle, core.Err("binding_progress_changed")
				}
				one := *p
				one.Bundles = []inferyard.BundleProgress{b}
				view := viewState{progress: &one, candidate: job.candidate}
				command, e = view.bindCommand(strconv.FormatInt(pid, 10), endpoint)
				if e != nil {
					return job.bundle, e
				}
				break
			}
		}
		if command == "" {
			return job.bundle, core.Err("binding_progress_changed")
		}
		announce(job.bundle)
		r, e := execute(ctx, core.WizardRequest{Command: command})
		if e != nil {
			return job.bundle, e
		}
		if r.ExitCode != 0 {
			return job.bundle, wizardFailure(r)
		}
	}
	return "", nil
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
