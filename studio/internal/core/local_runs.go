package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"time"
)

type CandidateRunsRequest struct {
	Candidates         []CandidateRun
	AllowSingleBundle  bool
	ExistingService    *EngineState
	InitialExclusivity string `json:"initial_exclusivity,omitempty"`
}

type candidateRunState struct {
	candidate, family, group string
	attach                   chan EngineState
	initial                  *EngineState
	reused                   *EngineState
	bound                    *EngineState
	description              inferyard.CandidateDescription
	size                     int64
	modTime                  int64
	fingerprinted            bool
}

func (s *Service) StartCandidateRuns(req CandidateRunsRequest) ([]RunTask, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if len(req.Candidates) < 1 || len(req.Candidates) > 2 || len(req.Candidates) == 1 && !req.AllowSingleBundle {
		return nil, Err("invalid_request")
	}
	u := s.userSettings()
	results := s.resultsRoot(u)
	if results == nil {
		return nil, Conflict("settings_incomplete")
	}
	probe := s.probe(u)
	if probe.Status != "ok" {
		return nil, cliProbeError(probe)
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
		size, modTime, fingerprinted := int64(0), int64(0), false
		if st, err := os.Stat(path); err == nil {
			size, modTime, fingerprinted = st.Size(), st.ModTime().UnixNano(), true
		}
		description, failure := s.describeCandidateCached(s.ctx, path)
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
		root, err = Resolve(*results, root)
		if err != nil {
			return nil, Conflict("output_root_outside_results")
		}
		seen[bundle] = true
		local := &candidateRunState{candidate: path, family: description.Family, group: group, attach: make(chan EngineState, 1),
			description: description, size: size, modTime: modTime, fingerprinted: fingerprinted}
		if req.ExistingService != nil {
			if req.ExistingService.Pid <= 0 || req.ExistingService.Endpoint != description.Endpoint.URL || !validBindingEndpoint(req.ExistingService.Endpoint) {
				return nil, Err("invalid_request")
			}
			if description.Family == "lmstudio" {
				if i == 0 {
					copy := *req.ExistingService
					local.initial = &copy
				}
			} else {
				copy := *req.ExistingService
				local.reused = &copy
			}
		}
		task := RunTask{TaskId: uuid(), Kind: "run", State: "starting", Queued: true, Bundle: &bundle, CreatedAt: now(), CandidatePath: &path, EngineFamily: description.Family, ReasoningEffort: description.Generation.ReasoningEffort, ReasoningMode: description.Generation.ReasoningMode}
		entries = append(entries, &runEntry{Task: task, root: root, binary: *probe.Path, Resources: []ResourceSample{}, local: local, initialExclusivity: normalizeInitialExclusivity(req.InitialExclusivity)})
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

func (s *Service) reusableGroupEngine(t *runEntry, description inferyard.CandidateDescription) (EngineState, bool) {
	s.mu.Lock()
	if s.closing || s.orphan != nil || s.engine == nil || s.engine.state.State != "running" {
		s.mu.Unlock()
		return EngineState{}, false
	}
	var matched *runEntry
	for _, previous := range s.runs {
		if previous == t {
			break
		}
		if previous.local != nil && previous.local.group == t.local.group && previous.local.bound != nil {
			matched = previous
		}
	}
	if matched == nil {
		s.mu.Unlock()
		return EngineState{}, false
	}
	state := clone(s.engine.state)
	bound := matched.local.bound
	prevDesc := matched.local.description
	s.mu.Unlock()
	if state.Pid <= 0 || state.Pid != bound.Pid || state.StartTicks <= 0 || state.StartTicks != bound.StartTicks {
		return EngineState{}, false
	}
	if state.Endpoint != description.Endpoint.URL ||
		prevDesc.Family != description.Family ||
		prevDesc.Assets.Model.Path != description.Assets.Model.Path ||
		prevDesc.Conditions.ContextSize != description.Conditions.ContextSize {
		return EngineState{}, false
	}
	ticks, err := s.processTicks(state.Pid)
	if err != nil || ticks != state.StartTicks {
		return EngineState{}, false
	}
	return state, true
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
	description := t.local.description
	st, statErr := os.Stat(t.local.candidate)
	if !t.local.fingerprinted || statErr != nil || st.Size() != t.local.size || st.ModTime().UnixNano() != t.local.modTime {
		var failure *ApiError
		description, failure = s.DescribeCandidate(ctx, t.local.candidate)
		if failure != nil {
			return failure
		}
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
	} else if t.local.reused != nil {
		state = *t.local.reused
		if state.Endpoint != description.Endpoint.URL {
			return Err("candidate_endpoint_mismatch")
		}
		state.StartTicks, _ = s.processTicks(state.Pid)
		if state.StartTicks <= 0 {
			return Err("service_identity_unavailable")
		}
	} else if groupState, ok := s.reusableGroupEngine(t, description); ok {
		state = groupState
		ownedStarted = true
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
	output, failure := NextOutput(*workspace, "bound", str(t.Task.Bundle))
	if failure != nil {
		return failure
	}
	args, failure := BindArgs(t.local.candidate, output, state.Pid, state.Endpoint)
	if failure != nil {
		return failure
	}
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
	if ctx.Err() == nil || t.local.family == "lmstudio" || t.local.reused != nil {
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
