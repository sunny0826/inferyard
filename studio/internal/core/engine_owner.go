package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type engineOwner struct {
	ServiceID         string                  `json:"service_id,omitempty"`
	Local             bool                    `json:"local,omitempty"`
	Origin            string                  `json:"origin,omitempty"`
	Family            string                  `json:"family,omitempty"`
	Adapter           string                  `json:"adapter,omitempty"`
	Executable        string                  `json:"executable,omitempty"`
	Engine            string                  `json:"engine,omitempty"`
	Model             string                  `json:"model,omitempty"`
	ModelName         string                  `json:"model_name,omitempty"`
	ModelKind         string                  `json:"model_kind,omitempty"`
	ServedModel       string                  `json:"served_model,omitempty"`
	Port              int                     `json:"port,omitempty"`
	RequestedPort     int                     `json:"requested_port,omitempty"`
	LANAccess         bool                    `json:"lan_access,omitempty"`
	EstimatedMemoryGB float64                 `json:"estimated_memory_gb,omitempty"`
	ContextSize       int64                   `json:"context_size,omitempty"`
	GPULayers    *int                    `json:"gpu_layers,omitempty"`
	Threads      *int                    `json:"threads,omitempty"`
	ExtraArgs    []string                `json:"extra_args,omitempty"`
	WebURL       string                  `json:"web_url,omitempty"`
	StartedAt    string                  `json:"started_at,omitempty"`
	ProcessGroup int64                   `json:"process_group,omitempty"`
	Workers      []engineProcessIdentity `json:"workers,omitempty"`
	PID          int64                   `json:"pid"`
	StartTicks   int64                   `json:"start_ticks"`
	Endpoint     string                  `json:"endpoint"`
	LogFile      string                  `json:"log_file"`
	Candidate    string                  `json:"candidate"`
}
type orphanEngine struct {
	owner engineOwner
	path  string
	state EngineState
	done  chan struct{}
}

func ownerPath(workspace string) string { return filepath.Join(workspace, "logs", "engine-owner.json") }

func serviceOwnersDir(workspace string) string { return filepath.Join(workspace, "engine-owners") }

func serviceOwnerPath(workspace, serviceID string) string {
	return filepath.Join(serviceOwnersDir(workspace), serviceID+".json")
}

func (s *Service) persistEngineOwner(e *engineEntry, workspace string) *ApiError {
	return s.persistEngineOwnerAt(e, ownerPath(workspace))
}

func (s *Service) persistServiceOwner(e *engineEntry, workspace, serviceID string) *ApiError {
	return s.persistEngineOwnerAt(e, serviceOwnerPath(workspace, serviceID))
}

func (s *Service) persistEngineOwnerAt(e *engineEntry, path string) *ApiError {
	webURL := ""
	if !e.state.WebUIDisabled && e.state.Endpoint != "" {
		webURL = e.state.Endpoint
	}
	owner := engineOwner{
		ServiceID:         e.state.ServiceID,
		PID:               e.state.Pid,
		Local:             e.state.Local,
		Origin:            normalizeEngineOrigin(e.state.Origin),
		Family:            e.state.Family,
		Adapter:           e.state.Adapter,
		Executable:        e.state.Executable,
		Engine:            e.state.EngineName,
		Model:             e.state.Model,
		ModelName:         e.state.ModelName,
		ModelKind:         e.state.ModelKind,
		ServedModel:       e.state.ServedModel,
		Port:              e.state.Port,
		RequestedPort:     e.state.RequestedPort,
		LANAccess:         e.state.LANAccess,
		EstimatedMemoryGB: e.state.EstimatedMemoryGB,
		ContextSize:       e.state.ContextSize,
		GPULayers:         cloneIntPtr(e.state.GPULayers),
		Threads:           cloneIntPtr(e.state.Threads),
		ExtraArgs:         append([]string(nil), e.state.ExtraArgs...),
		WebURL:            webURL,
		StartedAt:         e.state.StartedAt,
		Endpoint:          e.state.Endpoint,
		LogFile:           filepath.Base(e.state.LogPath),
		Candidate:         filepath.Base(e.state.Candidate),
	}
	if owner.Local {
		owner.ProcessGroup = owner.PID
	}
	e.owner, e.ownerPath = owner, path
	ticks, err := s.processTicks(e.state.Pid)
	if err != nil || ticks <= 0 {
		return &ApiError{Code: "service_identity_unreadable", Message: l10n.TR("core.engine.err.start_identity_unreadable")}
	}
	owner.StartTicks = ticks
	e.owner = owner
	if e.state.Local {
		owner.Local = true
		owner.ProcessGroup = owner.PID
	}
	e.state.StartTicks = ticks
	if err := writeEngineOwner(path, owner); err != nil {
		return &ApiError{Code: "engine_owner_write_failed", Message: l10n.TR("core.engine.err.owner_write_failed")}
	}
	e.ownerPath = path
	e.owner = owner
	e.pidSnapshot = buildOwnerPIDSnapshot(e.state.Pid, owner)
	return nil
}

func removeEngineOwner(path string, expected engineOwner) error {
	unlock, err := lockEngineOwner(path)
	if err != nil {
		return err
	}
	defer unlock()
	return removeEngineOwnerLocked(path, expected)
}

func removeEngineOwnerLocked(path string, expected engineOwner) error {
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
	if !reflect.DeepEqual(current, expected) {
		return nil
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeEngineOwner(path string, owner engineOwner) error {
	unlock, err := lockEngineOwner(path)
	if err != nil {
		return err
	}
	defer unlock()
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
	if owner.Origin != "" && !validEngineOrigin(owner.Origin) {
		return false
	}
	if owner.Local && owner.ProcessGroup != owner.PID || !owner.Local && (owner.ProcessGroup != 0 || len(owner.Workers) != 0) {
		return false
	}
	seen := map[int64]bool{owner.PID: true}
	for _, worker := range owner.Workers {
		if worker.PID <= 0 || worker.StartTicks < owner.StartTicks || seen[worker.PID] {
			return false
		}
		seen[worker.PID] = true
	}
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
	return u.Port() != "" && isAllowedBindHost(u.Hostname(), owner.LANAccess)
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
	s.detectLegacyOwnerLocked(workspace)
	s.detectMultiServiceOwnersLocked(workspace)
}

func (s *Service) detectLegacyOwnerLocked(workspace string) {
	path := ownerPath(workspace)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	var owner engineOwner
	var failure *ApiError
	if err != nil || json.Unmarshal(raw, &owner) != nil || !validOwner(owner) {
		failure = &ApiError{Code: "engine_owner_invalid", Message: l10n.TR("core.engine.err.owner_invalid")}
	} else {
		same, err := s.ownedTreeAlive(&owner, path)
		if err == nil && !same {
			if normalizeEngineOrigin(owner.Origin) == EngineOriginStandalone {
				if writeErr := s.recordExitedWhileAwayLocked(workspace, owner); writeErr != nil {
					failure = &ApiError{Code: "service_exit_notice_write_failed", Message: l10n.TR("core.engine.err.exit_notice_write_failed")}
				}
			}
			if failure == nil {
				if err := removeEngineOwner(path, owner); err == nil {
					return
				}
				failure = &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.owner_remove_failed")}
			}
		} else if err == nil && same {
			if normalizeEngineOrigin(owner.Origin) == EngineOriginStandalone {
				s.adoptStandaloneServiceLocked(workspace, path, owner, true)
				return
			}
		} else if err != nil {
			failure = &ApiError{Code: "service_identity_unreadable", Message: l10n.TR("core.engine.err.orphan_identity_unreadable")}
		}
	}
	state := EngineState{
		ServiceID:         owner.ServiceID,
		State:             "orphaned",
		Origin:            normalizeEngineOrigin(owner.Origin),
		Family:            owner.Family,
		EngineName:        owner.Engine,
		Model:             owner.Model,
		ModelName:         owner.ModelName,
		Port:              owner.Port,
		RequestedPort:     owner.RequestedPort,
		LANAccess:         owner.LANAccess,
		EstimatedMemoryGB: owner.EstimatedMemoryGB,
		Pid:               owner.PID,
		StartTicks:        owner.StartTicks,
		Local:             owner.Local,
		Endpoint:          owner.Endpoint,
		LogPath:           owner.LogFile,
		Candidate:         owner.Candidate,
		Error:             failure,
	}
	s.orphan = &orphanEngine{owner: owner, path: path, state: state}
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(state)})
}

func (s *Service) detectMultiServiceOwnersLocked(workspace string) {
	dir := serviceOwnersDir(workspace)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	changed := false
	for _, name := range names {
		fileID := strings.TrimSuffix(name, ".json")
		if fileID == "" {
			continue
		}
		if _, exists := s.services[fileID]; exists {
			continue
		}
		if _, exists := s.orphanedServices[fileID]; exists {
			continue
		}
		path := filepath.Join(dir, name)
		raw, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue
		}
		var owner engineOwner
		var failure *ApiError
		if readErr != nil || json.Unmarshal(raw, &owner) != nil || !validOwner(owner) {
			failure = &ApiError{Code: "engine_owner_invalid", Message: l10n.TR("core.engine.err.owner_invalid")}
			owner.ServiceID = fileID
		} else {
			if owner.ServiceID == "" {
				owner.ServiceID = fileID
			}
			same, idErr := s.ownedTreeAlive(&owner, path)
			if idErr == nil && !same {
				if writeErr := s.recordExitedWhileAwayLocked(workspace, owner); writeErr != nil {
					failure = &ApiError{Code: "service_exit_notice_write_failed", Message: l10n.TR("core.engine.err.exit_notice_write_failed")}
				} else if rmErr := removeEngineOwner(path, owner); rmErr != nil {
					failure = &ApiError{Code: "engine_owner_remove_failed", Message: l10n.TR("core.engine.err.owner_remove_failed")}
				} else {
					continue
				}
			} else if idErr == nil && same {
				s.adoptStandaloneServiceLocked(workspace, path, owner, false)
				changed = true
				continue
			} else if idErr != nil {
				failure = &ApiError{Code: "service_identity_unreadable", Message: l10n.TR("core.engine.err.orphan_identity_unreadable")}
			}
		}
		serviceID := owner.ServiceID
		if serviceID == "" {
			serviceID = fileID
		}
		state := EngineState{
			ServiceID:         serviceID,
			State:             "orphaned",
			Origin:            EngineOriginStandalone,
			Family:            owner.Family,
			EngineName:        owner.Engine,
			Model:             owner.Model,
			ModelName:         owner.ModelName,
			Port:              owner.Port,
			RequestedPort:     owner.RequestedPort,
			LANAccess:         owner.LANAccess,
			EstimatedMemoryGB: owner.EstimatedMemoryGB,
			Pid:               owner.PID,
			StartTicks:        owner.StartTicks,
			Local:             owner.Local,
			Endpoint:          owner.Endpoint,
			LogPath:           owner.LogFile,
			Candidate:         owner.Candidate,
			Error:             failure,
		}
		s.orphanedServices[serviceID] = &orphanEngine{owner: owner, path: path, state: state}
		changed = true
	}
	if changed {
		s.publishServicesStateLocked()
	}
}

func (s *Service) processTicks(pid int64) (int64, error) {
	if s.identityReader != nil {
		return s.identityReader(pid)
	}
	return processStartTicks(pid)
}
