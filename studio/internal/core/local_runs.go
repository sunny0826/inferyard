package core

import (
	"context"
	"path/filepath"
	"strconv"
	"time"
)

type CandidateRunsRequest struct {
	Candidates        []CandidateRun
	AllowSingleBundle bool
	ExistingService   *EngineState
}

type candidateRunState struct {
	candidate, family, group string
	attach                   chan EngineState
	initial                  *EngineState
	bound                    *EngineState
}

func (s *Service) StartCandidateRuns(req CandidateRunsRequest) ([]RunTask, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if len(req.Candidates) < 1 || len(req.Candidates) > 2 || len(req.Candidates) == 1 && !req.AllowSingleBundle {
		return nil, Err("invalid_request")
	}
	u := s.userSettings()
	if u.ResultsRoot == nil {
		return nil, Conflict("settings_incomplete")
	}
	probe := s.probe(u)
	if probe.Status != "ok" {
		return nil, Err("cli_unavailable")
	}
	group := uuid()
	entries := []*runEntry{}
	seen := map[string]bool{}
	family, model := "", ""
	for i, candidate := range req.Candidates {
		path, failure := s.candidateFile(candidate.CandidatePath)
		if failure != nil {
			return nil, failure
		}
		description, failure := s.DescribeCandidate(s.ctx, path)
		if failure != nil {
			return nil, failure
		}
		if description.Definition != "local-engine-description.v2" || description.Family != candidate.Family {
			return nil, Err("candidate_mismatch")
		}
		if i == 0 {
			family, model = description.Family, description.Assets.Model.Path
		}
		if description.Family != family || description.Assets.Model.Path != model {
			return nil, Err("candidate_mismatch")
		}
		_, root, bundle, err := readConfig(path)
		if err != nil || bundle != candidate.Bundle || seen[bundle] {
			return nil, Err("invalid_request")
		}
		root, err = Resolve(*u.ResultsRoot, root)
		if err != nil {
			return nil, Conflict("output_root_outside_results")
		}
		seen[bundle] = true
		local := &candidateRunState{candidate: path, family: description.Family, group: group, attach: make(chan EngineState, 1)}
		if i == 0 && req.ExistingService != nil {
			if description.Family != "lmstudio" || req.ExistingService.Pid <= 0 || req.ExistingService.Endpoint != description.Endpoint.URL || !validBindingEndpoint(req.ExistingService.Endpoint) {
				return nil, Err("invalid_request")
			}
			copy := *req.ExistingService
			local.initial = &copy
		}
		task := RunTask{TaskId: uuid(), Kind: "run", State: "starting", Queued: true, Bundle: &bundle, CreatedAt: now(), CandidatePath: &path, EngineFamily: description.Family}
		entries = append(entries, &runEntry{Task: task, root: root, binary: *probe.Path, Resources: []ResourceSample{}, local: local})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if failure := s.admitRunLocked(); failure != nil {
		return nil, failure
	}
	if s.runsActiveLocked() {
		return nil, Err("run_active")
	}
	result := []RunTask{}
	for _, entry := range entries {
		s.runs = append(s.runs, entry)
		s.state(entry)
		result = append(result, clone(entry.Task))
	}
	s.signal()
	return result, nil
}

func (s *Service) BindCandidateService(taskID string, pid int64, endpoint string) *ApiError {
	if pid <= 0 || !validBindingEndpoint(endpoint) {
		return Err("invalid_request")
	}
	ticks, err := s.processTicks(pid)
	if err != nil || ticks <= 0 {
		return Err("service_identity_unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return Err("service_closing")
	}
	for _, entry := range s.runs {
		if entry.local != nil && entry.local.bound != nil && entry.local.family == "lmstudio" && entry.local.bound.Pid == pid && entry.local.bound.StartTicks == ticks {
			return Err("local_engine_service_reused")
		}
	}
	for _, entry := range s.runs {
		if entry.Task.TaskId != taskID {
			continue
		}
		if entry.local == nil || entry.local.family != "lmstudio" || !entry.Task.AwaitingService || terminal(entry.Task.State) || entry.Task.State == "cancelling" {
			return Conflict("invalid_state")
		}
		select {
		case entry.local.attach <- EngineState{State: "running", Pid: pid, Endpoint: endpoint, Local: true}:
			entry.Task.AwaitingService = false
			s.state(entry)
			return nil
		default:
			return Conflict("invalid_state")
		}
	}
	return Err("not_found")
}

func (s *Service) candidateStage(t *runEntry, stage string, awaiting bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Task.PreparationStage = stage
	t.Task.AwaitingService = awaiting
	s.state(t)
}

func (s *Service) prepareCandidateRun(ctx context.Context, t *runEntry) (failure *ApiError) {
	ownedStarted := false
	defer func() {
		if failure != nil && ownedStarted {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if cleanupFailure := s.stopEngineForHandoff(cleanup); cleanupFailure != nil {
				failure = cleanupFailure
			}
		}
	}()
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	s.mu.Lock()
	blocked := false
	for _, previous := range s.runs {
		if previous == t {
			break
		}
		if previous.local != nil && previous.local.group == t.local.group &&
			(previous.Task.State != "done" || previous.Task.Exit == nil || previous.Task.Exit.ExitCode != 0 || str(previous.Task.Exit.Completeness) != "complete") {
			blocked = true
		}
	}
	s.mu.Unlock()
	if blocked {
		return Err("previous_scope_incomplete")
	}
	description, failure := s.DescribeCandidate(ctx, t.local.candidate)
	if failure != nil {
		return failure
	}
	var state EngineState
	if t.local.family == "lmstudio" {
		if t.local.initial != nil {
			state = *t.local.initial
		} else {
			s.candidateStage(t, "awaiting_service", true)
			select {
			case <-ctx.Done():
				return Err("cancelled")
			case state = <-t.local.attach:
			}
		}
		if state.Endpoint != description.Endpoint.URL {
			return Err("candidate_endpoint_mismatch")
		}
		state.StartTicks, _ = s.processTicks(state.Pid)
		if state.StartTicks <= 0 {
			return Err("service_identity_unavailable")
		}
		s.mu.Lock()
		reused := false
		for _, previous := range s.runs {
			if previous.local != nil && previous.local.bound != nil && previous.local.family == "lmstudio" && previous != t {
				bound := previous.local.bound
				reused = reused || (bound.Pid == state.Pid && bound.StartTicks == state.StartTicks)
			}
		}
		s.mu.Unlock()
		if reused {
			return Err("local_engine_service_reused")
		}
	} else {
		s.candidateStage(t, "switching_service", false)
		if failure := s.stopEngineForHandoff(ctx); failure != nil {
			return failure
		}
		if ctx.Err() != nil {
			return Err("cancelled")
		}
		s.candidateStage(t, "starting_engine", false)
		state, failure = s.StartEngine(t.local.candidate)
		if failure != nil {
			return failure
		}
		ownedStarted = true
		for state.State == "starting" || state.State == "stopping" {
			if !waitCandidateTick(ctx) {
				return Err("cancelled")
			}
			state = s.Engine()
		}
		if state.State != "running" {
			if state.Error != nil {
				return state.Error
			}
			return Err("engine_start_failed")
		}
	}
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	s.candidateStage(t, "binding", false)
	workspace := workspaceRoot(s.userSettings())
	if workspace == nil {
		return Conflict("settings_incomplete")
	}
	output, failure := nextLocalOutput(*workspace, "bound", str(t.Task.Bundle))
	if failure != nil {
		return failure
	}
	args := []string{"config", "bind", "--candidate", t.local.candidate, "--pid", strconv.FormatInt(state.Pid, 10), "--endpoint", state.Endpoint, "--out", output}
	out, err := s.exec(ctx, t.binary, args...)
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	if failure := exitError(out, err); failure != nil {
		return failure
	}
	if out.ExitCode != 0 {
		return Err("cli_error")
	}
	ticks, identityErr := s.processTicks(state.Pid)
	if identityErr != nil || ticks != state.StartTicks {
		return Err("candidate_binding_invalid")
	}
	config := filepath.Join(output, "config.toml")
	_, root, bundle, err := readConfig(config)
	if err != nil || bundle != str(t.Task.Bundle) || root != t.root {
		return Err("candidate_binding_invalid")
	}
	s.mu.Lock()
	t.local.bound = &state
	t.Task.ConfigPath = &config
	t.Task.PreparationStage = ""
	t.Task.AwaitingService = false
	t.args = []string{"run", "--config", config}
	s.state(t)
	s.mu.Unlock()
	return nil
}

func (s *Service) finishCandidatePreparation(t *runEntry, ctx context.Context, failure *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Task.State = "failed"
	t.Task.Error = failure
	t.Task.FinishedAt = ptr(now())
	t.Task.AwaitingService = false
	if ctx.Err() != nil || failure.Code == "cancelled" {
		t.Task.State = "cancelled"
	}
	s.state(t)
}

func waitCandidateTick(ctx context.Context) bool {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) cleanupCancelledCandidate(ctx context.Context, t *runEntry) {
	if ctx.Err() == nil || t.local.family == "lmstudio" {
		return
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer stop()
	if failure := s.stopEngineForHandoff(cleanup); failure != nil {
		s.mu.Lock()
		t.Task.Error = failure
		s.state(t)
		s.mu.Unlock()
	}
}
