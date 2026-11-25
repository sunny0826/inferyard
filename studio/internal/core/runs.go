package core

import (
	"context"
	"inferyard/studio/internal/evidence"
	"inferyard/studio/internal/inferyard"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type runEntry struct {
	Task               RunTask
	Progress           *RunProgress
	Resources          []ResourceSample
	root               string
	args               []string
	binary             string
	cancel             context.CancelFunc
	recordRoot         string
	commandActive      bool
	local              *candidateRunState
	initialExclusivity string
	probeEndpoint      string
	probePID           int
}

func (s *Service) StartRuns(req StartRunsRequest) ([]RunTask, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if len(req.ConfigPaths) < 1 || len(req.ConfigPaths) > 2 || (len(req.ConfigPaths) == 1 && (req.AllowSingleBundle == nil || !*req.AllowSingleBundle)) {
		return nil, Err("invalid_request")
	}
	u := s.userSettings()
	results := s.resultsRoot(u)
	if results == nil {
		return nil, Conflict("settings_incomplete")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return nil, cliProbeError(p)
	}
	entries := []*runEntry{}
	seen, bundles := map[string]bool{}, map[string]bool{}
	for _, path := range req.ConfigPaths {
		config, root, bundle, e := readConfig(path)
		if e != nil {
			return nil, Err("invalid_request")
		}
		root, e = Resolve(*results, root)
		if e != nil {
			return nil, Conflict("output_root_outside_results")
		}
		if seen[config] || bundles[bundle] {
			return nil, Err("invalid_request")
		}
		seen[config] = true
		bundles[bundle] = true
		entries = append(entries, &runEntry{Task: RunTask{TaskId: uuid(), Kind: "run", State: "starting", Queued: true, ConfigPath: &config, Bundle: &bundle, CreatedAt: now()}, root: root, binary: *p.Path, args: []string{"run", "--config", config}, Resources: []ResourceSample{}, initialExclusivity: normalizeInitialExclusivity(req.InitialExclusivity)})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.admitRunLocked(); e != nil {
		return nil, e
	}
	for _, t := range s.runs {
		if !terminal(t.Task.State) && seen[str(t.Task.ConfigPath)] {
			return nil, Conflict("queue_duplicate")
		}
	}
	out := []RunTask{}
	for _, t := range entries {
		s.runs = append(s.runs, t)
		s.state(t)
		out = append(out, clone(t.Task))
	}
	s.signal()
	return out, nil
}
func (s *Service) Resume(req ResumeRunRequest) (RunTask, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if req.FromRun == "" || (req.EndpointUrl == nil) != (req.ServerPid == nil) || (req.ServerPid != nil && *req.ServerPid <= 0) {
		return RunTask{}, Err("invalid_request")
	}
	if req.EndpointUrl != nil {
		u, e := url.Parse(*req.EndpointUrl)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return RunTask{}, Err("invalid_request")
		}
	}
	u := s.userSettings()
	results := s.resultsRoot(u)
	if results == nil {
		return RunTask{}, Conflict("settings_incomplete")
	}
	path, e := Resolve(*results, req.FromRun)
	if e != nil {
		return RunTask{}, Err("invalid_request")
	}
	h, err := s.History()
	if err != nil {
		return RunTask{}, err
	}
	var found *RunSummary
	for _, b := range h.Batches {
		for _, r := range b.Runs {
			if r.Path == path {
				found = ptr(r)
			}
		}
	}
	for _, r := range h.StandaloneRuns {
		if r.Path == path {
			found = ptr(r)
		}
	}
	if found == nil {
		return RunTask{}, Err("not_found")
	}
	if !found.Resumable {
		return RunTask{}, Conflict("not_resumable")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return RunTask{}, cliProbeError(p)
	}
	args := []string{"resume", "--from-run", path}
	var probeEndpoint string
	var probePID int
	if req.EndpointUrl != nil {
		args = append(args, "--endpoint-url", *req.EndpointUrl, "--server-pid", strconv.FormatInt(*req.ServerPid, 10))
		probeEndpoint = *req.EndpointUrl
		probePID = int(*req.ServerPid)
	}
	t := &runEntry{Task: RunTask{TaskId: uuid(), Kind: "resume", State: "starting", Queued: true, ResumeFrom: &path, Bundle: found.Bundle, CreatedAt: now()}, root: filepath.Dir(path), binary: *p.Path, args: args, Resources: []ResourceSample{}, probeEndpoint: probeEndpoint, probePID: probePID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.admitRunLocked(); e != nil {
		return RunTask{}, e
	}
	for _, old := range s.runs {
		if !terminal(old.Task.State) && str(old.Task.ResumeFrom) == path {
			return RunTask{}, Conflict("queue_duplicate")
		}
	}
	s.runs = append(s.runs, t)
	s.state(t)
	s.signal()
	return clone(t.Task), nil
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) state(t *runEntry) {
	if t.Task.ContextSpeed != nil {
		if err := saveSpeedTask(t.recordRoot, t.Task); err != nil {
			t.Task.Error = Err("task_record_write_failed")
		}
	}
	_ = s.Bus.Publish("run.state", map[string]any{"task": t.Task})
}
func (s *Service) RunTasks() []RunTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []RunTask{}
	for i := len(s.runs) - 1; i >= 0; i-- {
		out = append(out, clone(s.runs[i].Task))
	}
	return out
}
func (s *Service) RunDetail(id string) (RunTaskDetail, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.runs {
		if t.Task.TaskId == id {
			return clone(RunTaskDetail{t.Task, t.Progress, t.Resources}), nil
		}
	}
	return RunTaskDetail{}, Err("not_found")
}
func (s *Service) CancelRun(id string) (RunTask, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.runs {
		if t.Task.TaskId != id {
			continue
		}
		if terminal(t.Task.State) {
			return RunTask{}, Conflict("invalid_state")
		}
		if t.Task.Queued {
			t.Task.State = "cancelled"
			t.Task.Queued = false
			t.Task.FinishedAt = ptr(now())
		} else {
			t.Task.State = "cancelling"
			if t.cancel != nil {
				t.cancel()
			}
		}
		s.state(t)
		return clone(t.Task), nil
	}
	return RunTask{}, Err("not_found")
}
func (s *Service) runQueue() {
	defer close(s.queueDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		}
		for {
			s.mu.Lock()
			if s.closing {
				s.mu.Unlock()
				return
			}
			var next *runEntry
			for _, t := range s.runs {
				if t.Task.Queued {
					next = t
					break
				}
			}
			if next != nil {
				next.Task.Queued = false
				next.Task.StartedAt = ptr(now())
				s.state(next)
			}
			s.mu.Unlock()
			if next == nil {
				break
			}
			s.executeRun(next)
		}
	}
}
func (s *Service) executeRun(t *runEntry) {
	if t.Task.ContextSpeed != nil {
		s.executeContextSpeed(t)
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.mu.Lock()
	t.cancel = cancel
	if t.Task.State == "cancelling" {
		cancel()
	}
	s.mu.Unlock()
	if t.local != nil {
		defer s.cleanupCancelledCandidate(ctx, t)
		if failure := s.prepareCandidateRun(ctx, t); failure != nil {
			s.finishCandidatePreparation(t, ctx, failure)
			return
		}
	}
	before := map[string]bool{}
	for _, r := range evidence.Index(t.root).Runs {
		before[r.Path] = true
	}
	type result struct {
		out inferyard.Execution
		err error
	}
	done := make(chan result, 1)
	execArgs := s.executionArgsWithRecovery(t)
	exclusivity := s.startRunExclusivity(ctx, t)
	s.mu.Lock()
	t.commandActive = true
	s.mu.Unlock()
	go func() { out, e := s.exec(ctx, t.binary, execArgs...); done <- result{out, e} }()
	ticker := time.NewTicker(ProgressInterval)
	defer ticker.Stop()
	var a *aggregate
	var ec, mc evidence.Cursor
	var tailErr error
	lastProgress := time.Time{}
	lastResources := time.Time{}
	resourcePending := []ResourceSample{}
	var pending bool
	poll := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if t.Task.RunPath == nil {
			for _, r := range evidence.Index(t.root).Runs {
				if !before[r.Path] {
					t.Task.RunPath = ptr(r.Path)
					t.Task.RunId = ptr(r.ID)
					if t.Task.State != "cancelling" {
						t.Task.State = "running"
					}
					a = newAggregate(r.Inputs)
					s.state(t)
					break
				}
			}
		}
		if a == nil {
			return
		}
		path := *t.Task.RunPath
		rows, c, e := evidence.Tail(filepath.Join(path, "events.jsonl"), ec)
		if e != nil && !os.IsNotExist(e) {
			tailErr = e
			cancel()
			return
		}
		ec = c
		if len(rows) > 0 {
			a.events(rows)
			pending = true
		}
		rows, c, e = evidence.Tail(filepath.Join(path, "memory.jsonl"), mc)
		if e != nil && !os.IsNotExist(e) {
			tailErr = e
			cancel()
			return
		}
		mc = c
		prevPhase := str(a.Progress.Phase)
		points := a.resources(rows)
		if str(a.Progress.Phase) != prevPhase {
			pending = true
		}
		t.Progress = ptr(clone(a.Progress))
		if pending && time.Since(lastProgress) >= ProgressInterval {
			s.Bus.Publish("run.progress", map[string]any{"task_id": t.Task.TaskId, "run_id": t.Task.RunId, "progress": a.Progress})
			pending = false
			lastProgress = time.Now()
		}
		if len(points) > 0 {
			t.Resources = appendResources(t.Resources, points)
			resourcePending = appendResources(resourcePending, points)
		}
		if len(resourcePending) > 0 && time.Since(lastResources) >= time.Second {
			s.Bus.Publish("run.resources", map[string]any{"task_id": t.Task.TaskId, "points": resourcePending})
			resourcePending = nil
			lastResources = time.Now()
		}
	}
	var resultValue result
loop:
	for {
		select {
		case <-ticker.C:
			poll()
			exclusivity.poll(ctx, t)
		case resultValue = <-done:
			poll()
			break loop
		}
	}
	if pending {
		wait := ProgressInterval - time.Since(lastProgress)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-s.ctx.Done():
				timer.Stop()
			}
		}
		poll()
	}
	exclusivity.finish(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(resourcePending) > 0 {
		s.Bus.Publish("run.resources", map[string]any{"task_id": t.Task.TaskId, "points": resourcePending})
	}
	out, err := resultValue.out, resultValue.err
	t.commandActive = false
	t.Task.FinishedAt = ptr(now())
	r, parseErr := inferyard.Parse(out.Stdout)
	limits := r.Limitations
	if limits == nil {
		limits = []string{}
	}
	t.Task.Exit = &RunExit{ExitCode: int64(out.ExitCode), CliStatus: text(r.Status), Completeness: text(r.Completeness), Limitations: limits}
	if a != nil {
		t.Task.Exit.StopReason = a.stop
	}
	if t.Task.Exit.StopReason == nil {
		details := document(r.Details)
		t.Task.Exit.StopReason = text(details["stop_reason"])
		if t.Task.Exit.StopReason == nil {
			t.Task.Exit.StopReason = text(details["last_stop_reason"])
		}
	}
	if out.ExitCode == inferyard.ExitIncomplete {
		t.Task.Exit.Completeness = ptr("incomplete")
	}
	switch {
	case tailErr != nil:
		t.Task.State = "failed"
		t.Task.Error = Err("internal_error")
	case out.ExitCode == inferyard.ExitCancelled || ctx.Err() != nil:
		t.Task.State = "cancelled"
		if ctx.Err() != nil {
			t.Task.Exit.ExitCode = inferyard.ExitCancelled
		}
	case err != nil:
		t.Task.State = "failed"
		t.Task.Error = Err("internal_error")
	case out.ExitCode == inferyard.ExitComplete || out.ExitCode == inferyard.ExitIncomplete:
		if parseErr != nil {
			t.Task.State = "failed"
			t.Task.Error = Err("internal_error")
		} else {
			t.Task.State = "done"
		}
	default:
		t.Task.State = "failed"
		t.Task.Error = exitError(out, nil)
	}
	s.state(t)
}
