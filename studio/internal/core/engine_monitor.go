package core

import (
	"context"
	"errors"
	"inferyard/studio/internal/l10n"
	"net/http"
	"os/exec"
	"time"
)

func (s *Service) monitorEngine(e *engineEntry, endpoint string) {
	defer close(e.done)
	defer e.logFile.Close()
	waitCh := make(chan error, 1)
	go func() { waitCh <- e.cmd.Wait() }()
	health := &http.Client{Timeout: 2 * time.Second}
	probeCtx, cancelProbe := context.WithCancel(context.Background())
	defer cancelProbe()
	probeCh := make(chan bool, 1)
	probeInFlight := false
	startedAt := time.Now()
	deadline := startedAt.Add(s.engineStartTimeout())
	probeInterval := 40 * time.Millisecond
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	treeTicker := time.NewTicker(500 * time.Millisecond)
	defer treeTicker.Stop()
	var lastMemSample time.Time
	finish := func(state string, err *ApiError, keepOwner bool) {
		treeErr := s.finishEngineTree(e)
		s.mu.Lock()
		defer s.mu.Unlock()
		if treeErr != nil {
			state, err = "failed", treeErr
			s.recordOrphanFromEntryLocked(e, err)
		} else if !keepOwner {
			if removeErr := removeEngineOwner(e.ownerPath, e.owner); removeErr != nil {
				state, err = "failed", &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.exited_owner_remove_failed")}
				s.recordOrphanFromEntryLocked(e, err)
			}
		}
		e.state.State, e.state.Error = state, err
		s.publishEngineLocked(e)
	}
	handleTimeout := func() bool {
		s.mu.Lock()
		if e.state.State != "starting" {
			s.mu.Unlock()
			return false
		}
		timedOut := time.Now().After(deadline) && !s.closing
		if timedOut {
			s.stopEngineLocked(e)
		}
		s.mu.Unlock()
		if !timedOut {
			return false
		}
		cancelProbe()
		s.terminateEngine(e, waitCh)
		finish("failed", &ApiError{Code: "engine_start_timeout", Message: l10n.TR("core.engine.err.start_timeout")}, false)
		return true
	}
	for {
		select {
		case <-e.detach:
			cancelProbe()
			return
		case <-e.stop:
			cancelProbe()
			s.terminateEngine(e, waitCh)
			finish("stopped", nil, false)
			return
		case waitErr := <-waitCh:
			cancelProbe()
			s.mu.Lock()
			stopping := e.state.State == "stopping"
			wasHealthy := e.wasHealthy
			logPath := e.state.LogPath
			s.mu.Unlock()
			if stopping {
				finish("stopped", nil, false)
			} else {
				var exitCode *int
				if e.cmd != nil && e.cmd.ProcessState != nil {
					code := e.cmd.ProcessState.ExitCode()
					exitCode = &code
				} else {
					var ee *exec.ExitError
					if errors.As(waitErr, &ee) {
						code := ee.ExitCode()
						exitCode = &code
					}
				}
				exitErr := &ApiError{Code: "engine_exited", Message: l10n.TR("core.engine.err.exited")}
				if !wasHealthy && logPath != "" {
					if rawTail, tailErr := readLogTailWindow(logPath); tailErr == nil {
						if tail := summarizeLogTail(rawTail, 5); tail != "" {
							exitErr.Details = map[string]any{"log_tail": tail}
						}
					}
				}
				persistErr := s.recordUnexpectedStandaloneExit(e, exitCode)
				finish("failed", exitErr, persistErr != nil)
			}
			return
		case <-treeTicker.C:
			if e.state.Local {
				_ = s.refreshEntryTree(e)
			}
		case healthy := <-probeCh:
			probeInFlight = false
			s.mu.Lock()
			if e.state.State != "starting" {
				s.mu.Unlock()
				continue
			}
			if healthy {
				e.wasHealthy = true
				e.state.State = "running"
				s.publishEngineLocked(e)
				s.mu.Unlock()
				if probeInterval != time.Second {
					probeInterval = time.Second
					ticker.Reset(probeInterval)
				}
				s.maybeSampleRuntimeMemory(e, &lastMemSample, true)
				continue
			}
			s.mu.Unlock()
			if handleTimeout() {
				return
			}
			if next := startingProbeInterval(time.Since(startedAt)); next != probeInterval {
				probeInterval = next
				ticker.Reset(probeInterval)
			}
		case <-ticker.C:
			s.mu.Lock()
			starting := e.state.State == "starting"
			s.mu.Unlock()
			if !starting {
				if probeInterval != time.Second {
					probeInterval = time.Second
					ticker.Reset(probeInterval)
				}
				s.maybeSampleRuntimeMemory(e, &lastMemSample, false)
				continue
			}
			if !probeInFlight {
				probeInFlight = true
				local, family := e.state.Local, e.family
				go func() {
					probeCh <- checkEngineHealthContext(probeCtx, health, endpoint, local, family)
				}()
			}
		}
	}
}
