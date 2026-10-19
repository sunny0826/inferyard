package core

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"inferyard/studio/internal/evidence"
	"inferyard/studio/internal/inferyard"
)

func (s *Service) executeContextSpeed(t *runEntry) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.mu.Lock()
	t.cancel = cancel
	if t.Task.State == "cancelling" {
		cancel()
	} else {
		t.Task.State = "running"
	}
	t.Task.ContextSpeed.Stage = "measuring"
	s.state(t)
	s.mu.Unlock()
	type response struct {
		out inferyard.Execution
		err error
	}
	done := make(chan response, 1)
	go func() {
		if e := s.speedExecutionBoundary(t); e != nil {
			done <- response{inferyard.Execution{ExitCode: 4}, e}
			return
		}
		out, err := s.exec(inferyard.WithDrainGrace(ctx, 10*time.Second), t.binary, t.args...)
		done <- response{out, err}
	}()
	ticker := time.NewTicker(ProgressInterval)
	defer ticker.Stop()
	var completed response
	var ownershipError bool
waitLoop:
	for {
		select {
		case completed = <-done:
			break waitLoop
		case <-ticker.C:
			runs, e := evidence.ContextSpeedRuns(t.root, t.Task.ContextSpeed.Plan.SHA256, t.Task.ContextSpeed.Plan.TrialIDs)
			if e != nil {
				ownershipError = true
				cancel()
				continue
			}
			count := 0
			for _, r := range runs {
				if r.State == evidence.Sealed {
					count++
				}
			}
			s.mu.Lock()
			t.Task.ContextSpeed.Completed = count
			s.state(t)
			s.mu.Unlock()
		}
	}
	out, err := completed.out, completed.err
	var failure *ApiError
	state := "done"
	switch {
	case ownershipError:
		state = "failed"
		failure = Err("run_identity_mismatch")
	case out.ExitCode == 130:
		state = "cancelled"
	case out.ExitCode < 0:
		state = "failed"
		failure = Err("cli_forced_or_unfinished_exit")
	case err != nil && ctx.Err() == nil:
		state = "failed"
		failure = Err("cli_error")
	case out.ExitCode != 0 && out.ExitCode != 3:
		state = "failed"
		failure = exitError(out, nil)
	case ctx.Err() != nil:
		state = "cancelled"
	}
	s.mu.Lock()
	t.Task.Exit = &RunExit{ExitCode: int64(out.ExitCode), Limitations: []string{}}
	if r, e := inferyard.Parse(out.Stdout); e == nil {
		t.Task.Exit.CliStatus = &r.Status
		t.Task.Exit.Completeness = &r.Completeness
		t.Task.Exit.Limitations = r.Limitations
	}
	s.mu.Unlock()
	if _, e := os.Stat(filepath.Join(t.root, "batch.json")); e == nil {
		readCtx, stop := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
		s.mu.Lock()
		t.cancel = stop
		t.Task.ContextSpeed.Stage = "reading"
		s.state(t)
		s.mu.Unlock()
		// Cancellation of execution must not cancel the new offline read.
		s.mu.Lock()
		readingTask := clone(t.Task.ContextSpeed)
		s.mu.Unlock()
		result, e := s.readContextSpeed(readCtx, t.binary, readingTask)
		stop()
		s.mu.Lock()
		if e != nil {
			if state == "done" {
				state = "failed"
			}
			if failure == nil {
				failure = e
			}
		} else {
			t.Task.ContextSpeed.DisplayModel = result.Metadata.Model.DisplayName
			t.Task.ContextSpeed.DisplayEngine = result.Metadata.Engine.Release
			t.Task.ContextSpeed.Completed = result.Execution.Counts.Completed
			t.Task.ContextSpeed.Batch.Runs = []inferyard.SpeedRunRef{}
			for _, sample := range result.Samples {
				if sample.Run != nil {
					t.Task.ContextSpeed.Batch.Runs = append(t.Task.ContextSpeed.Batch.Runs, *sample.Run)
				}
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Task.State = state
	t.Task.Error = failure
	t.Task.FinishedAt = ptr(now())
	t.Task.ContextSpeed.Stage = state
	s.state(t)
}

func (s *Service) ReadContextSpeed(ctx context.Context, id string) (inferyard.ContextSpeedResult, *ApiError) {
	rows, e := s.ContextSpeedHistory()
	if e != nil {
		return inferyard.ContextSpeedResult{}, e
	}
	for _, t := range rows {
		if t.TaskId != id {
			continue
		}
		if !terminal(t.State) {
			return inferyard.ContextSpeedResult{}, Conflict("run_active")
		}
		p := s.probe(s.userSettings())
		if p.Status != "ok" {
			return inferyard.ContextSpeedResult{}, Err("cli_unavailable")
		}
		return s.readContextSpeed(ctx, *p.Path, t.ContextSpeed)
	}
	return inferyard.ContextSpeedResult{}, Err("not_found")
}

func (s *Service) readContextSpeed(ctx context.Context, binary string, task *ContextSpeedTask) (inferyard.ContextSpeedResult, *ApiError) {
	fail := func(code string) (inferyard.ContextSpeedResult, *ApiError) {
		return inferyard.ContextSpeedResult{}, Err(code)
	}
	if task == nil || task.Batch == nil {
		return fail("batch_not_created")
	}
	u := s.userSettings()
	if u.ResultsRoot == nil {
		return fail("settings_incomplete")
	}
	path, err := Resolve(*u.ResultsRoot, task.Batch.Path)
	if err != nil {
		return fail("output_root_outside_results")
	}
	out, err := s.exec(ctx, binary, "speed-results", "--batch", path)
	if ctx.Err() != nil || out.ExitCode == 130 {
		return fail("cancelled")
	}
	if err != nil {
		return fail("cli_error")
	}
	r, err := inferyard.ParseContextSpeed(out.Stdout, out.ExitCode)
	if err != nil {
		return fail("context_speed_invalid_response")
	}
	if r.ReadError != nil {
		code := "cli_error"
		if out.ExitCode == 2 {
			code = "cli_blocked"
		}
		e := Err(code)
		if fixedReason.MatchString(r.ReadError.Code) {
			e.Limitations = []string{r.ReadError.Code}
		}
		return inferyard.ContextSpeedResult{}, e
	}
	envelope, err := inferyard.Parse(out.Stdout)
	if err != nil || envelope.EvidenceDir == nil {
		return fail("context_speed_invalid_response")
	}
	returned, err := Resolve(*u.ResultsRoot, *envelope.EvidenceDir)
	if err != nil || returned != path {
		return fail("batch_identity_mismatch")
	}
	if task.Plan != nil {
		if r.Source.PlanSHA256 != task.Plan.SHA256 || len(task.Plan.TrialIDs) != 5 {
			return fail("plan_identity_mismatch")
		}
		for i, sample := range r.Samples {
			if sample.TrialID != task.Plan.TrialIDs[i] {
				return fail("trial_identity_mismatch")
			}
		}
	}
	for _, sample := range r.Samples {
		if sample.Run == nil {
			continue
		}
		if _, err := Resolve(path, filepath.Join(path, filepath.FromSlash(sample.Run.Path))); err != nil {
			return fail("run_identity_mismatch")
		}
	}
	return r, nil
}
