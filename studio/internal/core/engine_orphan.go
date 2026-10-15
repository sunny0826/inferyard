package core

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

func (s *Service) StopOrphanEngine() (EngineState, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return EngineState{}, Err("service_closing")
	}
	if s.runsActiveLocked() {
		return EngineState{}, Err("run_active")
	}
	e := s.orphan
	if e == nil {
		return EngineState{State: "stopped"}, nil
	}
	if e.state.State == "stopping" {
		return clone(e.state), nil
	}
	if !validOwner(e.owner) {
		return clone(e.state), e.state.Error
	}
	// Recheck immediately before requesting termination, not only at launch.
	same, err := s.orphanIdentity(e)
	if err != nil {
		e.state.Error = orphanIdentityError()
		s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
		return clone(e.state), e.state.Error
	}
	if !same {
		if err := s.clearOrphanLocked(e); err != nil {
			return clone(e.state), err
		}
		return EngineState{State: "stopped"}, nil
	}
	e.state.State, e.state.Error = "stopping", nil
	e.done = make(chan struct{})
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
	go s.stopOrphan(e)
	return clone(e.state), nil
}

func orphanIdentityError() *ApiError {
	return &ApiError{Code: "service_identity_unreadable", Message: "无法确认遗留引擎的启动标识；可仅删除工作区 logs/engine-owner.json（不终止进程）。"}
}
func (s *Service) orphanIdentity(e *orphanEngine) (bool, error) {
	ticks, err := s.processTicks(e.owner.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ticks == e.owner.StartTicks, nil
}

func (s *Service) stopOrphan(e *orphanEngine) {
	defer close(e.done)
	var failure *ApiError
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Kill} {
		same, err := s.orphanIdentity(e)
		if err != nil {
			failure = orphanIdentityError()
			break
		}
		if !same {
			break
		}
		if err := s.signalOwnedPID(e.owner.PID, sig); err != nil {
			same, identityErr := s.orphanIdentity(e)
			if identityErr == nil && !same {
				break
			}
			failure = &ApiError{Code: "engine_stop_failed", Message: "遗留引擎停止失败，所有权记录已保留。"}
			break
		}
		same, err = s.waitOrphan(e)
		if err != nil {
			failure = orphanIdentityError()
			break
		}
		if !same {
			break
		}
		if sig == os.Kill {
			failure = &ApiError{Code: "engine_stop_failed", Message: "遗留引擎仍未退出，所有权记录已保留。"}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if failure == nil {
		failure = s.clearOrphanLocked(e)
	}
	if failure != nil {
		e.state.State, e.state.Error = "orphaned", failure
		s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
	}
}

func (s *Service) waitOrphan(e *orphanEngine) (bool, error) {
	timeout := 10 * time.Second
	if s.orphanStopTimeout > 0 {
		timeout = s.orphanStopTimeout
	}
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		same, err := s.orphanIdentity(e)
		if err != nil || !same {
			return same, err
		}
		select {
		case <-timer.C:
			return true, nil
		case <-ticker.C:
		}
	}
}

func (s *Service) signalOwnedPID(pid int64, sig os.Signal) error {
	if s.processSignal != nil {
		return s.processSignal(pid, sig)
	}
	process, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}
	defer process.Release()
	if runtime.GOOS == "windows" || sig == os.Kill {
		return process.Kill()
	}
	return process.Signal(sig)
}

func (s *Service) clearOrphanLocked(e *orphanEngine) *ApiError {
	if err := removeEngineOwner(e.path, e.owner); err != nil {
		return &ApiError{Code: "engine_owner_remove_failed", Message: "无法清除工作区 logs/engine-owner.json；可重试仅删除记录（不终止进程）。"}
	}
	if s.orphan == e {
		s.orphan = nil
	}
	s.Bus.Publish("engine.state", map[string]any{"engine": EngineState{State: "stopped"}})
	return nil
}
