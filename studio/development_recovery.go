//go:build studio_dev && !release

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"inferyard/studio/internal/core"
)

type devChild struct {
	PID int64 `json:"pid"`
}

type devRecoveryResult struct {
	Kind          string `json:"kind"`
	Outcome       string `json:"outcome"`
	Controller    string `json:"controller_manifest"`
	ControllerSHA string `json:"controller_sha256"`
	Target        string `json:"target_manifest"`
	TargetSHA     string `json:"target_sha256"`
	ChildSHA      string `json:"child_sha256"`
	PID           int64  `json:"pid"`
}

type devRecoveryReader struct {
	observe func(int64) (core.DevelopmentProcessState, error)
	lock    func(context.Context, string) (func(), error)
}

func nativeDevRecoveryReader() devRecoveryReader {
	return devRecoveryReader{core.DevelopmentObserveProcess, core.DevelopmentLockEngineOwner}
}

func (m *devManifest) recoverySession(pid int64) (devSession, bool, error) {
	s, err := m.readSession()
	if os.IsNotExist(err) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	if s.State == "shutdown_failed" {
		return s, true, fmt.Errorf("previous shutdown failed")
	}
	if pid != 0 && s.PID != pid {
		return s, true, fmt.Errorf("late session does not match child PID")
	}
	return s, true, nil
}

// This mode proves only that a single managed child can no longer execute.
// A valid session is handed back to its original, identity-checking controller.
func recoverDevelopment(ctx context.Context, controller, target *devManifest, reader devRecoveryReader) (devRecoveryResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result := devRecoveryResult{Kind: "studio_dev_recovery.v1", Controller: controller.path, ControllerSHA: controller.inputDigest, Target: target.path, TargetSHA: target.inputDigest}
	if controller.Checkout != target.Checkout {
		return result, fmt.Errorf("recovery target belongs to another checkout")
	}
	validateInputs := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, m := range []*devManifest{controller, target} {
			if err := checkDigest(m.path, m.inputDigest); err != nil {
				return err
			}
			if err := checkDigest(m.studioPath(), m.StudioSHA256); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validateInputs(); err != nil {
		return result, err
	}
	handoff := func(pid int64) (bool, error) {
		s, present, err := target.recoverySession(pid)
		if err != nil {
			return false, err
		}
		if present {
			result.Outcome = "session_present"
			result.PID = s.PID
		}
		return present, nil
	}
	if present, err := handoff(0); present || err != nil {
		return result, err
	}
	childPath := filepath.Join(filepath.Dir(target.path), "child.json")
	var child devChild
	digest, err := readDevJSONSnapshot(childPath, &child)
	if err != nil {
		return result, err
	}
	if err := core.DevelopmentValidatePID(child.PID); err != nil {
		return result, err
	}
	result.ChildSHA, result.PID = digest, child.PID
	if err := target.ensureStopRequest(); err != nil {
		return result, err
	}
	state, err := reader.observe(child.PID)
	if err != nil || state != core.DevelopmentProcessAbsent {
		if present, sessionErr := handoff(child.PID); present || sessionErr != nil {
			return result, sessionErr
		}
		return result, fmt.Errorf("child absence is not proven: state=%d: %v", state, err)
	}
	unlock, err := reader.lock(ctx, filepath.Join(target.ProfileRoot, "workspace"))
	if err != nil {
		return result, err
	}
	defer unlock()
	if present, err := handoff(child.PID); present || err != nil {
		return result, err
	}
	if err := target.ownerAbsent(); err != nil {
		return result, err
	}
	if err := validateInputs(); err != nil {
		return result, err
	}
	if err := checkDigest(childPath, digest); err != nil {
		return result, err
	}
	if err := target.ensureStopRequest(); err != nil {
		return result, err
	}
	state, err = reader.observe(child.PID)
	if err != nil || state != core.DevelopmentProcessAbsent {
		return result, fmt.Errorf("child absence changed: state=%d: %v", state, err)
	}
	if present, err := handoff(child.PID); present || err != nil {
		return result, err
	}
	if err := target.ownerAbsent(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Outcome = "recovered_missing_session"
	return result, nil
}
