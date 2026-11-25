package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
	"os"
)

func (s *Service) DiscardOrphanRecord() (EngineState, *ApiError) {
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
		e = s.firstOrphanServiceLocked()
	}
	if e == nil {
		return EngineState{}, Conflict("no_orphan_record")
	}
	return s.discardOrphanEntryLocked(e)
}

func (s *Service) discardOrphanEntryLocked(e *orphanEngine) (EngineState, *ApiError) {
	if e.state.State == "stopping" {
		return clone(e.state), Err("engine_stopping")
	}
	unlock, err := lockEngineOwner(e.path)
	if err != nil {
		return clone(e.state), ownerRemovalError()
	}
	defer unlock()
	raw, err := os.ReadFile(e.path)
	if err != nil && !os.IsNotExist(err) {
		return clone(e.state), ownerRemovalError()
	}
	var owner engineOwner
	valid := err == nil && json.Unmarshal(raw, &owner) == nil && validOwner(owner)
	if valid {
		same, identityErr := s.ownedTreeAlive(&owner, "")
		if owner.Local && identityErr != nil {
			return clone(e.state), orphanIdentityError()
		}
		if identityErr == nil && same {
			return clone(e.state), &ApiError{Code: "engine_orphaned", Message: l10n.TR("core.engine.err.orphan_still_matches")}
		}
		if err := removeEngineOwnerLocked(e.path, owner); err != nil {
			return clone(e.state), ownerRemovalError()
		}
	} else if err == nil {
		if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
			return clone(e.state), ownerRemovalError()
		}
	}
	if s.orphan == e {
		s.orphan = nil
	}
	if e.state.ServiceID != "" && s.orphanedServices[e.state.ServiceID] == e {
		delete(s.orphanedServices, e.state.ServiceID)
	}
	if s.engine != nil && (s.engine.state.State == "failed" || s.engine.state.State == "stopped") {
		s.engine = nil
	}
	state := EngineState{State: "stopped"}
	s.Bus.Publish("engine.state", map[string]any{"engine": s.primaryManagedServiceLocked()})
	s.publishServicesStateLocked()
	return state, nil
}

func ownerRemovalError() *ApiError {
	return &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.owner_read_or_remove_failed")}
}
