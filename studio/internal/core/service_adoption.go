package core

import (
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"inferyard/studio/internal/l10n"
)

const defaultAdoptPollInterval = time.Second

func (s *Service) adoptPollPeriod() time.Duration {
	if s.adoptPollInterval > 0 {
		return s.adoptPollInterval
	}
	return defaultAdoptPollInterval
}

func (s *Service) adoptStandaloneServiceLocked(workspace, path string, owner engineOwner, migrateLegacy bool) {
	serviceID := owner.ServiceID
	if serviceID == "" {
		modelKey := owner.ModelName
		if modelKey == "" {
			modelKey = owner.Model
		}
		engineKey := owner.Engine
		if engineKey == "" {
			engineKey = owner.Family
		}
		if modelKey != "" && engineKey != "" {
			serviceID = ServiceIDFor(modelKey, engineKey)
		} else {
			serviceID = sanitizeSlugPart(owner.Candidate, 48)
		}
		owner.ServiceID = serviceID
	}
	if migrateLegacy && workspace != "" {
		newPath := serviceOwnerPath(workspace, serviceID)
		if err := writeEngineOwner(newPath, owner); err == nil {
			_ = os.Remove(path)
			path = newPath
		}
	}
	port := owner.Port
	if port <= 0 {
		port = portFromEndpoint(owner.Endpoint)
	}
	logPath := owner.LogFile
	if logPath != "" && !filepath.IsAbs(logPath) {
		logPath = filepath.Join(workspace, "logs", logPath)
	}
	initialHealth := &http.Client{Timeout: 500 * time.Millisecond}
	healthy := owner.Endpoint != "" && checkEngineHealth(initialHealth, owner.Endpoint, owner.Local, owner.Family)
	initialState := "starting"
	if healthy {
		initialState = "running"
	}
	entry := &engineEntry{
		ownerPath:  path,
		owner:      owner,
		family:     owner.Family,
		wasHealthy: healthy,
		adopted:    true,
		stop:       make(chan struct{}),
		detach:     make(chan struct{}),
		done:       make(chan struct{}),
		state: EngineState{
			ServiceID:         serviceID,
			State:             initialState,
			Adopted:           true,
			Local:             owner.Local,
			Origin:            EngineOriginStandalone,
			Family:            owner.Family,
			Adapter:           owner.Adapter,
			Executable:        owner.Executable,
			EngineName:        owner.Engine,
			Model:             owner.Model,
			ModelName:         owner.ModelName,
			ModelKind:         owner.ModelKind,
			ServedModel:       owner.ServedModel,
			Port:              port,
			RequestedPort:     owner.RequestedPort,
			LANAccess:         owner.LANAccess,
			EstimatedMemoryGB: owner.EstimatedMemoryGB,
			ContextSize:       owner.ContextSize,
			GPULayers:         cloneIntPtr(owner.GPULayers),
			Threads:           cloneIntPtr(owner.Threads),
			ExtraArgs:         append([]string(nil), owner.ExtraArgs...),
			StartedAt:         owner.StartedAt,
			Pid:               owner.PID,
			StartTicks:        owner.StartTicks,
			Endpoint:          owner.Endpoint,
			LogPath:           logPath,
			Candidate:         owner.Candidate,
		},
	}
	s.services[serviceID] = entry
	s.publishEngineLocked(entry)
	go s.monitorAdoptedEngine(entry)
}

func (s *Service) monitorAdoptedEngine(e *engineEntry) {
	defer close(e.done)
	health := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(s.adoptPollPeriod())
	defer ticker.Stop()
	for {
		select {
		case <-e.detach:
			return
		case <-e.stop:
			failure := s.terminateAdoptedEngine(e)
			s.mu.Lock()
			if failure == nil {
				if err := removeEngineOwner(e.ownerPath, e.owner); err != nil {
					failure = &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.owner_remove_failed")}
				}
			}
			if failure != nil {
				e.state.State, e.state.Error = "failed", failure
				s.recordOrphanFromEntryLocked(e, failure)
				s.publishEngineLocked(e)
			} else {
				e.state.State, e.state.Error = "stopped", nil
				s.publishEngineLocked(e)
			}
			s.mu.Unlock()
			return
		case <-ticker.C:
			alive, err := s.ownedTreeAlive(&e.owner, e.ownerPath)
			if err != nil {
				s.mu.Lock()
				failure := orphanIdentityError()
				e.state.State, e.state.Error = "orphaned", failure
				s.recordOrphanFromEntryLocked(e, failure)
				if s.engine == e {
					s.engine = nil
				}
				if e.state.ServiceID != "" && s.services[e.state.ServiceID] == e {
					delete(s.services, e.state.ServiceID)
				}
				s.publishEngineLocked(e)
				s.mu.Unlock()
				return
			}
			if !alive {
				persistErr := s.recordUnexpectedStandaloneExit(e, nil)
				s.mu.Lock()
				var failure *ApiError
				if persistErr == nil {
					if removeErr := removeEngineOwner(e.ownerPath, e.owner); removeErr != nil {
						failure = &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.exited_owner_remove_failed")}
						s.recordOrphanFromEntryLocked(e, failure)
					} else {
						failure = &ApiError{Code: "engine_exited", Message: l10n.TR("core.engine.err.exited")}
					}
				} else {
					failure = &ApiError{Code: "engine_exited", Message: l10n.TR("core.engine.err.exited")}
				}
				e.state.State, e.state.Error = "failed", failure
				s.publishEngineLocked(e)
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			starting := e.state.State == "starting"
			endpoint := e.state.Endpoint
			local := e.state.Local
			family := e.family
			s.mu.Unlock()
			if starting && endpoint != "" && checkEngineHealth(health, endpoint, local, family) {
				s.mu.Lock()
				if e.state.State == "starting" {
					e.wasHealthy = true
					e.state.State = "running"
					s.publishEngineLocked(e)
				}
				s.mu.Unlock()
			}
		}
	}
}

func (s *Service) terminateAdoptedEngine(e *engineEntry) *ApiError {
	if e.family == "polystrata" && e.wasHealthy && e.state.Endpoint != "" {
		_ = unloadPolystrata(e.state.Endpoint)
	}
	if e.owner.Local {
		return s.terminateOwnedTree(&e.owner, e.ownerPath)
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Kill} {
		same, err := s.ownedTreeAlive(&e.owner, e.ownerPath)
		if err != nil {
			return orphanIdentityError()
		}
		if !same {
			return nil
		}
		if err := s.signalOwnedPID(e.owner.PID, sig); err != nil {
			same, checkErr := s.ownedTreeAlive(&e.owner, e.ownerPath)
			if checkErr == nil && !same {
				return nil
			}
			return &ApiError{Code: "engine_stop_failed", Message: l10n.TR("core.engine.err.orphan_stop_failed")}
		}
		same, err = s.waitAdoptedOwnerExit(&e.owner, e.ownerPath)
		if err != nil {
			return orphanIdentityError()
		}
		if !same {
			return nil
		}
		if sig == os.Kill {
			return &ApiError{Code: "engine_stop_failed", Message: l10n.TR("core.engine.err.orphan_still_running")}
		}
	}
	return nil
}

func (s *Service) waitAdoptedOwnerExit(owner *engineOwner, path string) (bool, error) {
	timeout := 10 * time.Second
	if s.orphanStopTimeout > 0 {
		timeout = s.orphanStopTimeout
	}
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		same, err := s.ownedTreeAlive(owner, path)
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
