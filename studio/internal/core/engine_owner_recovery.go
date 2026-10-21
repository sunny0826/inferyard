package core

import (
	"encoding/json"
	"os"
)

// DiscardOrphanRecord never signals a process. Re-read the on-disk record so a
// repaired/replaced record cannot bypass the normal stop path via stale UI state.
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
		return EngineState{}, Conflict("no_orphan_record")
	}
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
			return clone(e.state), &ApiError{Code: "engine_orphaned", Message: "logs/engine-owner.json 的进程身份仍匹配，请使用「停止遗留服务」。"}
		}
		// Compare-and-delete: a concurrent Studio instance may have written a
		// fresh record after our read; only remove the exact record we checked.
		if err := removeEngineOwnerLocked(e.path, owner); err != nil {
			return clone(e.state), ownerRemovalError()
		}
	} else if err == nil {
		// The cross-process lock protects the corrupt-record read and deletion.
		if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
			return clone(e.state), ownerRemovalError()
		}
	}
	s.orphan = nil
	// A managed engine may have exited with an owner-record cleanup failure.
	if s.engine != nil && (s.engine.state.State == "failed" || s.engine.state.State == "stopped") {
		s.engine = nil
	}
	state := EngineState{State: "stopped"}
	s.Bus.Publish("engine.state", map[string]any{"engine": state})
	return state, nil
}

func ownerRemovalError() *ApiError {
	return &ApiError{Code: "engine_owner_remove_failed", Message: "无法读取或删除工作区 logs/engine-owner.json，请检查文件权限；未终止任何进程。"}
}
