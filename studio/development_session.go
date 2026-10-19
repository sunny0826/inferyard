//go:build studio_dev && !release

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"inferyard/studio/internal/core"
)

type devSession struct {
	Kind       string `json:"kind"`
	SessionID  string `json:"session_id"`
	PID        int64  `json:"pid"`
	StartTicks int64  `json:"start_ticks"`
	State      string `json:"state"`
	CLIPath    string `json:"cli_path"`
}
type devStopRequest struct {
	SessionID string `json:"session_id"`
}

func (m *devManifest) sessionID() string { return filepath.Base(filepath.Dir(m.path)) }
func (m *devManifest) sessionPath() string {
	return filepath.Join(filepath.Dir(m.path), "session.json")
}
func (m *devManifest) stopPath() string { return filepath.Join(filepath.Dir(m.path), "stop.json") }
func (m *devManifest) ownerAbsent() error {
	_, err := os.Lstat(filepath.Join(m.ProfileRoot, "workspace", "logs", "engine-owner.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("engine-owner record remains; refusing environment switch")
}

func (m *devManifest) readSession() (devSession, error) {
	var s devSession
	if err := readDevJSON(m.sessionPath(), &s); err != nil {
		return s, err
	}
	if s.Kind != "studio_dev_session.v1" || s.SessionID != m.sessionID() || s.PID <= 0 || s.StartTicks <= 0 || s.CLIPath != m.cliPath() || (s.State != "starting" && s.State != "ready" && s.State != "stopped" && s.State != "shutdown_failed") {
		return s, fmt.Errorf("invalid development session")
	}
	return s, nil
}

// The callback returns only after any created Service has completed shutdown.
func (m *devManifest) runSession(ctx context.Context, cancel context.CancelFunc, run func(*devSession) error) (result error) {
	if _, err := os.Lstat(m.sessionPath()); !os.IsNotExist(err) {
		return fmt.Errorf("launch already has session evidence: %s", m.sessionPath())
	}
	ticks, err := core.DevelopmentProcessStartTicks(int64(os.Getpid()))
	if err != nil {
		return err
	}
	session := devSession{Kind: "studio_dev_session.v1", SessionID: m.sessionID(), PID: int64(os.Getpid()), StartTicks: ticks, State: "starting", CLIPath: m.cliPath()}
	if err := atomicDevJSON(m.sessionPath(), session); err != nil {
		return err
	}
	defer func() {
		cancel()
		err := m.ownerAbsent()
		if session.State == "shutdown_failed" || err != nil {
			session.State = "shutdown_failed"
		} else {
			session.State = "stopped"
		}
		if writeErr := atomicDevJSON(m.sessionPath(), session); err == nil {
			err = writeErr
		}
		if err != nil {
			result = errors.Join(result, err)
		}
	}()
	if m.stopRequested() {
		cancel()
	}
	go m.watchStop(ctx, cancel)
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(&session)
}

func (m *devManifest) stopRequested() bool {
	var request devStopRequest
	return readDevJSON(m.stopPath(), &request) == nil && request.SessionID == m.sessionID()
}

type devProcessReader struct {
	ticks      func(int64) (int64, error)
	executable func(int64) (string, error)
}

func nativeDevProcessReader() devProcessReader {
	return devProcessReader{core.DevelopmentProcessStartTicks, core.DevelopmentProcessExecutable}
}

func (m *devManifest) sessionAlive(s devSession, reader devProcessReader) (bool, error) {
	ticks, err := reader.ticks(s.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ticks != s.StartTicks {
		return false, fmt.Errorf("session PID identity mismatch")
	}
	path, err := reader.executable(s.PID)
	if err != nil {
		return false, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	if path != m.studioPath() {
		return false, fmt.Errorf("session executable mismatch")
	}
	if err := checkDigest(path, m.StudioSHA256); err != nil {
		return false, err
	}
	ticks, err = reader.ticks(s.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ticks != s.StartTicks {
		return false, fmt.Errorf("session PID changed during validation")
	}
	return true, nil
}

func (m *devManifest) stopSession(ctx context.Context, reader devProcessReader) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, err := m.readSession()
	if err != nil {
		return err
	}
	alive, err := m.sessionAlive(s, reader)
	if err != nil {
		return err
	}
	if s.State == "shutdown_failed" {
		return fmt.Errorf("previous shutdown failed")
	}
	if !alive {
		return m.ownerAbsent()
	}
	if s.State == "stopped" {
		return m.ownerAbsent()
	}
	if err := atomicDevJSON(m.stopPath(), devStopRequest{m.sessionID()}); err != nil {
		return err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("development stop did not complete: %w", ctx.Err())
		case <-ticker.C:
			current, err := m.readSession()
			if err != nil {
				return err
			}
			if current.PID != s.PID || current.StartTicks != s.StartTicks {
				return fmt.Errorf("session changed during shutdown")
			}
			alive, err := m.sessionAlive(current, reader)
			if err != nil {
				return err
			}
			if current.State == "shutdown_failed" {
				return fmt.Errorf("Studio shutdown failed")
			}
			if !alive || current.State == "stopped" {
				return m.ownerAbsent()
			}
		}
	}
}

func (m *devManifest) watchStop(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.stopRequested() {
				cancel()
				return
			}
		}
	}
}
