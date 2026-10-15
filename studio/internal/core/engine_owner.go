package core

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

// engineOwner contains identity and display metadata only, never startup argv.
type engineOwner struct {
	PID        int64  `json:"pid"`
	StartTicks int64  `json:"start_ticks"`
	Endpoint   string `json:"endpoint"`
	LogFile    string `json:"log_file"`
	Candidate  string `json:"candidate"`
}
type orphanEngine struct {
	owner engineOwner
	path  string
	state EngineState
	done  chan struct{}
}

func ownerPath(workspace string) string { return filepath.Join(workspace, "logs", "engine-owner.json") }

func (s *Service) persistEngineOwner(e *engineEntry, workspace string) *ApiError {
	ticks, err := s.processTicks(e.state.Pid)
	if err != nil || ticks <= 0 {
		return &ApiError{Code: "service_identity_unreadable", Message: "无法读取引擎启动标识，已中止启动。"}
	}
	owner := engineOwner{PID: e.state.Pid, StartTicks: ticks, Endpoint: e.state.Endpoint, LogFile: filepath.Base(e.state.LogPath), Candidate: filepath.Base(e.state.Candidate)}
	path := ownerPath(workspace)
	if err := writeEngineOwner(path, owner); err != nil {
		return &ApiError{Code: "engine_owner_write_failed", Message: "无法保存工作区 logs/engine-owner.json，已中止启动。"}
	}
	e.ownerPath = path
	e.owner = owner
	return nil
}

func removeEngineOwner(path string, expected engineOwner) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var current engineOwner
	if json.Unmarshal(raw, &current) != nil {
		return os.ErrInvalid
	}
	if current.PID != expected.PID || current.StartTicks != expected.StartTicks {
		return nil
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeEngineOwner(path string, owner engineOwner) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "engine-owner-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}

func validOwner(owner engineOwner) bool {
	if owner.PID <= 0 || owner.StartTicks <= 0 || owner.LogFile == "" || owner.Candidate == "" {
		return false
	}
	for _, name := range []string{owner.LogFile, owner.Candidate} {
		if name == "." || name == ".." || filepath.Base(name) != name {
			return false
		}
	}
	u, err := url.Parse(owner.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	return u.Port() != "" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())
}

func (s *Service) detectEngineOwner() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detectEngineOwnerLocked()
}

func (s *Service) detectEngineOwnerLocked() {
	workspace := str(workspaceRoot(s.settings))
	if workspace == "" {
		return
	}
	path := ownerPath(workspace)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	var owner engineOwner
	var failure *ApiError
	if err != nil || json.Unmarshal(raw, &owner) != nil || !validOwner(owner) {
		failure = &ApiError{Code: "engine_owner_invalid", Message: "引擎所有权记录无法读取，请检查工作区 logs/engine-owner.json，或仅删除记录（不终止进程）。"}
	} else {
		ticks, err := s.processTicks(owner.PID)
		if errors.Is(err, os.ErrNotExist) || err == nil && ticks != owner.StartTicks {
			if err := removeEngineOwner(path, owner); err == nil {
				return
			}
			failure = &ApiError{Code: "engine_owner_remove_failed", Message: "无法清除工作区 logs/engine-owner.json；可重试仅删除记录（不终止进程）。"}
		} else if err != nil {
			failure = &ApiError{Code: "service_identity_unreadable", Message: "无法确认遗留引擎的启动标识；可仅删除工作区 logs/engine-owner.json（不终止进程）。"}
		}
	}
	state := EngineState{State: "orphaned", Pid: owner.PID, Endpoint: owner.Endpoint, LogPath: owner.LogFile, Candidate: owner.Candidate, Error: failure}
	s.orphan = &orphanEngine{owner: owner, path: path, state: state}
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(state)})
}

func (s *Service) processTicks(pid int64) (int64, error) {
	if s.identityReader != nil {
		return s.identityReader(pid)
	}
	return processStartTicks(pid)
}
