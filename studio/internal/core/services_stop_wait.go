package core

import (
	"context"
	"errors"
	"strings"
)

func stopContextError(ctx context.Context) *ApiError {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Err("engine_stop_failed")
	}
	return Err("cancelled")
}

// StopManagedServiceAndWait stops a single managed standalone service by ServiceID
// and waits directly on its monitor completion channel (entry.done) without polling.
func (s *Service) StopManagedServiceAndWait(ctx context.Context, serviceID string) *ApiError {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return Err("invalid_request")
	}
	if apiErr := stopContextError(ctx); apiErr != nil {
		return apiErr
	}
	s.mu.Lock()
	if apiErr := stopContextError(ctx); apiErr != nil {
		s.mu.Unlock()
		return apiErr
	}
	if s.orphanedServices[id] != nil {
		s.mu.Unlock()
		return Err("engine_orphaned")
	}
	entry, ok := s.services[id]
	if !ok || entry == nil {
		if s.engine != nil && s.engine.state.ServiceID == id {
			entry = s.engine
		}
	}
	if entry == nil || entry.state.State == "" || entry.state.State == "stopped" || entry.state.State == "failed" {
		s.mu.Unlock()
		return nil
	}
	if s.runsActiveLocked() {
		s.mu.Unlock()
		return Err("run_active")
	}
	if entry.state.State == "starting" || entry.state.State == "running" {
		s.stopEngineLocked(entry)
	}
	done := entry.done
	s.mu.Unlock()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return stopContextError(ctx)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphanedServices[id] != nil {
		return Err("engine_orphaned")
	}
	if entry.state.State != "stopped" && entry.state.State != "failed" {
		return Err("engine_stop_failed")
	}
	return nil
}

// StopPrimaryEngineAndWait stops only the primary engine (s.engine if present,
// otherwise the first active managed service) and waits directly on entry.done.
func (s *Service) StopPrimaryEngineAndWait(ctx context.Context) *ApiError {
	if apiErr := stopContextError(ctx); apiErr != nil {
		return apiErr
	}
	s.mu.Lock()
	if apiErr := stopContextError(ctx); apiErr != nil {
		s.mu.Unlock()
		return apiErr
	}
	if s.orphan != nil {
		s.mu.Unlock()
		return Err("engine_orphaned")
	}
	var entry *engineEntry
	if s.engine != nil {
		if !isActiveServiceState(s.engine.state.State) {
			s.mu.Unlock()
			return nil
		}
		entry = s.engine
	} else {
		for _, st := range s.managedServicesLocked() {
			if st.ServiceID != "" {
				if e, ok := s.services[st.ServiceID]; ok && e != nil && isActiveServiceState(e.state.State) {
					entry = e
					break
				}
			}
		}
	}
	if entry == nil {
		s.mu.Unlock()
		return nil
	}
	if s.runsActiveLocked() {
		s.mu.Unlock()
		return Err("run_active")
	}
	if entry.state.State == "starting" || entry.state.State == "running" {
		s.stopEngineLocked(entry)
	}
	done := entry.done
	targetID := entry.state.ServiceID
	s.mu.Unlock()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return stopContextError(ctx)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphan != nil || (targetID != "" && s.orphanedServices[targetID] != nil) {
		return Err("engine_orphaned")
	}
	if entry.state.State != "stopped" && entry.state.State != "failed" {
		return Err("engine_stop_failed")
	}
	return nil
}

// StopActiveServicesAndWait stops all active managed standalone services and any active
// primary engine, looping until no active services remain, waiting directly on entry.done.
func (s *Service) StopActiveServicesAndWait(ctx context.Context) *ApiError {
	for {
		if apiErr := stopContextError(ctx); apiErr != nil {
			return apiErr
		}
		s.mu.Lock()
		if apiErr := stopContextError(ctx); apiErr != nil {
			s.mu.Unlock()
			return apiErr
		}
		if s.orphan != nil || len(s.orphanedServices) > 0 {
			s.mu.Unlock()
			return Err("engine_orphaned")
		}
		var targets []*engineEntry
		seen := map[*engineEntry]bool{}
		collect := func(entry *engineEntry) {
			if entry == nil || seen[entry] {
				return
			}
			if isActiveServiceState(entry.state.State) {
				seen[entry] = true
				targets = append(targets, entry)
			}
		}
		for _, entry := range s.services {
			collect(entry)
		}
		collect(s.engine)
		if len(targets) == 0 {
			s.mu.Unlock()
			return nil
		}
		if s.runsActiveLocked() {
			s.mu.Unlock()
			return Err("run_active")
		}
		for _, entry := range targets {
			if entry.state.State == "starting" || entry.state.State == "running" {
				s.stopEngineLocked(entry)
			}
		}
		s.mu.Unlock()

		for _, entry := range targets {
			if entry.done == nil {
				continue
			}
			select {
			case <-entry.done:
			case <-ctx.Done():
				return stopContextError(ctx)
			}
		}

		s.mu.Lock()
		if s.orphan != nil || len(s.orphanedServices) > 0 {
			s.mu.Unlock()
			return Err("engine_orphaned")
		}
		for _, entry := range targets {
			if isActiveServiceState(entry.state.State) {
				s.mu.Unlock()
				return Err("engine_stop_failed")
			}
		}
		s.mu.Unlock()
	}
}
