package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"inferyard/studio/internal/l10n"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ServiceIDFor returns a deterministic, filesystem-safe service ID for a (model, engine) pair.
func ServiceIDFor(model, engine string) string {
	m := strings.TrimSpace(model)
	e := strings.TrimSpace(engine)
	sum := sha256.Sum256([]byte(m + "\x00" + e))
	tag := hex.EncodeToString(sum[:4])
	return fmt.Sprintf("%s-%s-%s", sanitizeSlugPart(m, 32), sanitizeSlugPart(e, 24), tag)
}

func isActiveServiceState(state string) bool {
	return state == "starting" || state == "running" || state == "stopping"
}

// StartStandaloneEngine starts an independent model service without stopping other running services.
func (s *Service) StartStandaloneEngine(req StandaloneStartRequest) (EngineState, *ApiError) {
	return s.StartStandalone(s.ctx, req)
}

// ManagedServices returns all active (starting/running/stopping) and orphaned standalone services,
// sorted stably by start time and service ID.
func (s *Service) ManagedServices() []EngineState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managedServicesLocked()
}

func (s *Service) managedServicesLocked() []EngineState {
	out := make([]EngineState, 0, len(s.services)+len(s.orphanedServices))
	seen := map[string]bool{}
	for id, entry := range s.services {
		if entry == nil {
			continue
		}
		if !isActiveServiceState(entry.state.State) {
			continue
		}
		out = append(out, clone(entry.state))
		seen[id] = true
	}
	for id, orphan := range s.orphanedServices {
		if orphan == nil || seen[id] {
			continue
		}
		out = append(out, clone(orphan.state))
		seen[id] = true
	}
	if s.engine != nil && normalizeEngineOrigin(s.engine.state.Origin) == EngineOriginStandalone && isActiveServiceState(s.engine.state.State) {
		if s.engine.state.ServiceID == "" || !seen[s.engine.state.ServiceID] {
			out = append(out, clone(s.engine.state))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			if out[i].StartedAt == "" {
				return false
			}
			if out[j].StartedAt == "" {
				return true
			}
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ServiceID < out[j].ServiceID
	})
	return out
}

// ManagedService looks up a standalone service or orphaned standalone service by ServiceID.
func (s *Service) ManagedService(serviceID string) (EngineState, bool) {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return EngineState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if orphan, ok := s.orphanedServices[id]; ok && orphan != nil {
		return clone(orphan.state), true
	}
	if entry, ok := s.services[id]; ok && entry != nil {
		return clone(entry.state), true
	}
	if s.engine != nil && s.engine.state.ServiceID == id {
		return clone(s.engine.state), true
	}
	return EngineState{}, false
}

// StopManagedService safely stops a single standalone service by ServiceID without affecting others.
func (s *Service) StopManagedService(serviceID string) (EngineState, *ApiError) {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return EngineState{}, Err("invalid_request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runsActiveLocked() {
		return EngineState{}, Err("run_active")
	}
	entry, ok := s.services[id]
	if !ok || entry == nil {
		if s.engine != nil && s.engine.state.ServiceID == id {
			s.stopEngineLocked(s.engine)
			return clone(s.engine.state), nil
		}
		return EngineState{}, Err("not_found")
	}
	s.stopEngineLocked(entry)
	return clone(entry.state), nil
}

// StopOrphanService safely stops a specific orphaned standalone service by ServiceID.
func (s *Service) StopOrphanService(serviceID string) (EngineState, *ApiError) {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return EngineState{}, Err("invalid_request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return EngineState{}, Err("service_closing")
	}
	if s.runsActiveLocked() {
		return EngineState{}, Err("run_active")
	}
	orphan, ok := s.orphanedServices[id]
	if !ok || orphan == nil {
		return EngineState{}, Err("not_found")
	}
	return s.stopOrphanEntryLocked(orphan)
}

// DiscardOrphanService removes a specific orphaned standalone service record without killing any process.
func (s *Service) DiscardOrphanService(serviceID string) (EngineState, *ApiError) {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return EngineState{}, Err("invalid_request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return EngineState{}, Err("service_closing")
	}
	if s.runsActiveLocked() {
		return EngineState{}, Err("run_active")
	}
	orphan, ok := s.orphanedServices[id]
	if !ok || orphan == nil {
		return EngineState{}, Err("not_found")
	}
	return s.discardOrphanEntryLocked(orphan)
}

// StopAllManagedServices stops all running/starting standalone services in parallel and waits for completion.
func (s *Service) StopAllManagedServices() {
	s.mu.Lock()
	var targets []*engineEntry
	for _, entry := range s.services {
		if entry == nil {
			continue
		}
		if entry.state.State == "starting" || entry.state.State == "running" {
			s.stopEngineLocked(entry)
			targets = append(targets, entry)
		} else if entry.state.State == "stopping" {
			targets = append(targets, entry)
		}
	}
	if s.engine != nil && normalizeEngineOrigin(s.engine.state.Origin) == EngineOriginStandalone {
		if s.engine.state.State == "starting" || s.engine.state.State == "running" {
			s.stopEngineLocked(s.engine)
			targets = append(targets, s.engine)
		} else if s.engine.state.State == "stopping" {
			targets = append(targets, s.engine)
		}
	}
	s.mu.Unlock()
	for _, entry := range targets {
		<-entry.done
	}
}

// ServiceLogTail reads up to lines trailing non-empty log lines for a specific managed service.
func (s *Service) ServiceLogTail(serviceID string, lines int) ([]string, *ApiError) {
	st, ok := s.ManagedService(serviceID)
	if !ok {
		return nil, Err("not_found")
	}
	if strings.TrimSpace(st.LogPath) == "" {
		return []string{}, nil
	}
	tail, err := s.EngineLogTail(st.LogPath, lines)
	if err != nil {
		return nil, Err("not_found")
	}
	if strings.TrimSpace(tail) == "" {
		return []string{}, nil
	}
	return strings.Split(tail, "\n"), nil
}

func (s *Service) publishServicesStateLocked() {
	s.Bus.Publish("services.state", map[string]any{"services": s.managedServicesLocked()})
}

func (s *Service) hasActiveManagedServicesLocked() bool {
	for _, e := range s.services {
		if e != nil && isActiveServiceState(e.state.State) {
			return true
		}
	}
	return false
}

func (s *Service) firstOrphanServiceLocked() *orphanEngine {
	if len(s.orphanedServices) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.orphanedServices))
	for id := range s.orphanedServices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return s.orphanedServices[ids[0]]
}

func (s *Service) primaryManagedServiceLocked() EngineState {
	if orphan := s.firstOrphanServiceLocked(); orphan != nil {
		return clone(orphan.state)
	}
	active := s.managedServicesLocked()
	if len(active) > 0 {
		return active[0]
	}
	var latest *engineEntry
	for _, e := range s.services {
		if e == nil {
			continue
		}
		if latest == nil || e.state.StartedAt > latest.state.StartedAt {
			latest = e
		}
	}
	if latest != nil {
		return clone(latest.state)
	}
	return EngineState{State: "stopped"}
}

func (s *Service) stopPrimaryManagedServiceLocked() EngineState {
	active := s.managedServicesLocked()
	for _, st := range active {
		if st.ServiceID != "" {
			if e, ok := s.services[st.ServiceID]; ok && e != nil && (e.state.State == "starting" || e.state.State == "running") {
				s.stopEngineLocked(e)
				return clone(e.state)
			}
		}
	}
	return EngineState{State: "stopped"}
}

func (s *Service) recordOrphanFromEntryLocked(e *engineEntry, err *ApiError) {
	orphanState := clone(e.state)
	orphanState.State = "orphaned"
	orphanState.Error = err
	orphan := &orphanEngine{owner: e.owner, path: e.ownerPath, state: orphanState}
	if e.state.ServiceID != "" {
		s.orphanedServices[e.state.ServiceID] = orphan
		s.Bus.Publish("engine.state", map[string]any{"engine": clone(orphanState)})
		s.publishServicesStateLocked()
		return
	}
	s.orphan = orphan
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(orphanState)})
}

func (s *Service) occupiedPortsLocked() map[int]bool {
	ports := map[int]bool{}
	addEntry := func(e *engineEntry) {
		if e == nil || !isActiveServiceState(e.state.State) {
			return
		}
		p := e.state.Port
		if p <= 0 {
			p = portFromEndpoint(e.state.Endpoint)
		}
		if p > 0 {
			ports[p] = true
		}
	}
	addEntry(s.engine)
	for _, e := range s.services {
		addEntry(e)
	}
	for p := range s.reservedPorts {
		if p > 0 {
			ports[p] = true
		}
	}
	return ports
}

func (s *Service) startManagedServiceLocked(startup Startup, slug, serviceID string) (EngineState, *ApiError) {
	baseRef := filepath.Base(slug)
	if slug == "" || baseRef != slug || baseRef == "." || baseRef == ".." || serviceID == "" {
		return EngineState{}, Err("invalid_input")
	}
	host, port, e := validateStartup(startup)
	if e != nil {
		return EngineState{}, e
	}
	w := workspaceRoot(s.userSettings())
	if w == nil {
		return EngineState{}, Conflict("settings_incomplete")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return EngineState{}, Err("service_closing")
	}
	if s.deletingAssets > 0 {
		return EngineState{}, workspaceBusyError()
	}
	if orphan, exists := s.orphanedServices[serviceID]; exists && orphan != nil {
		return clone(orphan.state), &ApiError{Code: "engine_orphaned", Message: l10n.TR("core.engine.err.handle_orphan_first")}
	}
	if existing, exists := s.services[serviceID]; exists && existing != nil && isActiveServiceState(existing.state.State) {
		return clone(existing.state), Err("engine_candidate_mismatch")
	}
	for _, other := range s.services {
		if other == nil || !isActiveServiceState(other.state.State) {
			continue
		}
		otherPort := other.state.Port
		if otherPort <= 0 {
			otherPort = portFromEndpoint(other.state.Endpoint)
		}
		if otherPort == port {
			return EngineState{}, portInUseError(port)
		}
	}
	if s.engine != nil && isActiveServiceState(s.engine.state.State) {
		engPort := s.engine.state.Port
		if engPort <= 0 {
			engPort = portFromEndpoint(s.engine.state.Endpoint)
		}
		if engPort == port {
			return EngineState{}, portInUseError(port)
		}
	}

	bindHost := host
	if startup.LANAccess {
		bindHost = "0.0.0.0"
	}
	endpoint := "http://" + net.JoinHostPort(bindHost, strconv.Itoa(port))
	listen, err := net.Listen("tcp", net.JoinHostPort(bindHost, strconv.Itoa(port)))
	if err != nil {
		return EngineState{}, portInUseError(port)
	}
	_ = listen.Close()

	logDir := filepath.Join(*w, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return EngineState{}, Err("internal_error")
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("engine-%s-%d.log", sanitizeSlugPart(serviceID, 32), time.Now().UnixNano()))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return EngineState{}, Err("internal_error")
	}
	cmd := exec.Command(startup.Path, startup.Args...)
	if err := configureEngineProcess(cmd, startup); err != nil {
		_ = logFile.Close()
		return EngineState{}, Err("engine_process_isolation_failed")
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return EngineState{}, &ApiError{Code: "engine_start_failed", Message: l10n.TR("core.engine.err.start_failed")}
	}
	executable := ""
	if startup.Path != "" {
		executable = filepath.Base(startup.Path)
	}
	entry := &engineEntry{
		family:  startup.Family,
		cmd:     cmd,
		logFile: logFile,
		stop:    make(chan struct{}),
		detach:  make(chan struct{}),
		done:    make(chan struct{}),
		state: EngineState{
			ServiceID:         serviceID,
			State:             "starting",
			Local:             startup.Local,
			Origin:            EngineOriginStandalone,
			Family:            startup.Family,
			Adapter:           startup.Adapter,
			Executable:        executable,
			EngineName:        startup.EngineName,
			Model:             startup.ModelPath,
			ModelName:         startup.ModelName,
			ModelKind:         startup.ModelKind,
			ServedModel:       startup.ServedModel,
			Port:              port,
			RequestedPort:     startup.RequestedPort,
			LANAccess:         startup.LANAccess,
			EstimatedMemoryGB: startup.EstimatedMemoryGB,
			ContextSize:       startup.ContextSize,
			GPULayers:         cloneIntPtr(startup.GPULayers),
			Threads:           cloneIntPtr(startup.Threads),
			ExtraArgs:         append([]string(nil), startup.ExtraArgs...),
			StartedAt:         now(),
			WebUIDisabled:     startup.WebUIDisabled,
			Pid:               int64(cmd.Process.Pid),
			Endpoint:          endpoint,
			LogPath:           logPath,
			Candidate:         slug,
		},
	}
	if failure := s.persistServiceOwner(entry, *w, serviceID); failure != nil {
		if entry.state.Local {
			if cleanupErr := s.terminateOwnedTree(&entry.owner, ""); cleanupErr != nil {
				_ = writeEngineOwner(entry.ownerPath, entry.owner)
				s.recordOrphanFromEntryLocked(entry, cleanupErr)
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		return EngineState{}, failure
	}
	s.services[serviceID] = entry
	s.publishEngineLocked(entry)
	go s.monitorEngine(entry, endpoint)
	return clone(entry.state), nil
}

func portInUseError(port int) *ApiError {
	if port > 0 {
		return &ApiError{
			Code:    "engine_port_in_use",
			Message: l10n.TRf("core.engine.err.port_in_use_numbered", "port", strconv.Itoa(port)),
			Details: map[string]any{"port": port},
		}
	}
	return &ApiError{Code: "engine_port_in_use", Message: l10n.TR("core.engine.err.port_in_use")}
}
