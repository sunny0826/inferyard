package core

import (
	"context"
	"fmt"
	"inferyard/studio/internal/l10n"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

const (
	EngineOriginBenchmark    = "benchmark"
	EngineOriginContextSpeed = "context_speed"
	EngineOriginStandalone   = "standalone"
)

func validEngineOrigin(origin string) bool {
	switch origin {
	case EngineOriginBenchmark, EngineOriginContextSpeed, EngineOriginStandalone:
		return true
	default:
		return false
	}
}

func normalizeEngineOrigin(origin string) string {
	if validEngineOrigin(origin) {
		return origin
	}
	return EngineOriginBenchmark
}

type EngineState struct {
	ServiceID         string    `json:"service_id,omitempty"`
	StartTicks        int64     `json:"start_ticks"`
	Local             bool      `json:"local"`
	Adopted           bool      `json:"adopted,omitempty"`
	Origin            string    `json:"origin,omitempty"`
	Family            string    `json:"family,omitempty"`
	Adapter           string    `json:"adapter,omitempty"`
	Executable        string    `json:"executable,omitempty"`
	EngineName        string    `json:"engine_name,omitempty"`
	Model             string    `json:"model,omitempty"`
	ModelName         string    `json:"model_name,omitempty"`
	ModelKind         string    `json:"model_kind,omitempty"`
	ServedModel       string    `json:"served_model,omitempty"`
	Port              int       `json:"port,omitempty"`
	RequestedPort     int       `json:"requested_port,omitempty"`
	LANAccess         bool      `json:"lan_access,omitempty"`
	EstimatedMemoryGB float64   `json:"estimated_memory_gb,omitempty"`
	RuntimeMemoryGB   *float64  `json:"runtime_memory_gb,omitempty"`
	ContextSize       int64     `json:"context_size,omitempty"`
	GPULayers     *int      `json:"gpu_layers,omitempty"`
	Threads       *int      `json:"threads,omitempty"`
	ExtraArgs     []string  `json:"extra_args,omitempty"`
	StartedAt     string    `json:"started_at,omitempty"`
	WebUIDisabled bool      `json:"webui_disabled,omitempty"`
	State         string    `json:"state"`
	Pid           int64     `json:"pid"`
	Endpoint      string    `json:"endpoint"`
	LogPath       string    `json:"log_path"`
	Candidate     string    `json:"candidate"`
	Error         *ApiError `json:"error,omitempty"`
}

type engineEntry struct {
	ownerPath   string
	owner       engineOwner
	pidSnapshot []int // protected by Service.mu; immutable slice replaced on tree refresh
	family      string
	wasHealthy  bool
	adopted     bool
	cmd         *exec.Cmd
	logFile     *os.File
	state       EngineState
	stop        chan struct{}
	detach      chan struct{}
	done        chan struct{}
}

func (s *Service) Engine() EngineState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphan != nil {
		return clone(s.orphan.state)
	}
	if s.engine != nil {
		return clone(s.engine.state)
	}
	return s.primaryManagedServiceLocked()
}

func (s *Service) StartEngine(candidate string) (EngineState, *ApiError) {
	return s.startCandidateEngine(candidate, EngineOriginBenchmark)
}

func (s *Service) startCandidateEngine(candidate, origin string) (EngineState, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	closing := s.closing
	deleting := s.deletingAssets > 0
	s.mu.Unlock()
	if closing {
		return EngineState{}, Err("service_closing")
	}
	if deleting {
		return EngineState{}, workspaceBusyError()
	}
	startup, e := s.Startup(candidate)
	if e != nil {
		return EngineState{}, e
	}
	return s.startFromStartupLocked(startup, origin, candidate)
}

func (s *Service) startFromStartup(startup Startup, origin, ref string) (EngineState, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.startFromStartupLocked(startup, origin, ref)
}

func (s *Service) startFromStartupLocked(startup Startup, origin, ref string) (EngineState, *ApiError) {
	origin = normalizeEngineOrigin(origin)
	baseRef := filepath.Base(ref)
	if ref == "" || baseRef == "." || baseRef == ".." || baseRef == string(filepath.Separator) {
		return EngineState{}, Err("invalid_input")
	}
	if origin == EngineOriginStandalone && ref != baseRef {
		return EngineState{}, Err("invalid_input")
	}
	s.mu.Lock()
	closing := s.closing
	deleting := s.deletingAssets > 0
	s.mu.Unlock()
	if closing {
		return EngineState{}, Err("service_closing")
	}
	if deleting {
		return EngineState{}, workspaceBusyError()
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
	if s.orphan != nil {
		return clone(s.orphan.state), &ApiError{Code: "engine_orphaned", Message: l10n.TR("core.engine.err.handle_orphan_first")}
	}
	if s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping") {
		if s.engine.state.Candidate != ref || normalizeEngineOrigin(s.engine.state.Origin) != origin {
			return clone(s.engine.state), Err("engine_candidate_mismatch")
		}
		return clone(s.engine.state), nil
	}
	s.detectEngineOwnerLocked()
	if s.orphan != nil {
		return clone(s.orphan.state), Err("engine_orphaned")
	}
	if s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping") {
		if s.engine.state.Candidate != ref || normalizeEngineOrigin(s.engine.state.Origin) != origin {
			return clone(s.engine.state), Err("engine_candidate_mismatch")
		}
		return clone(s.engine.state), nil
	}
	if s.reservedPorts[port] {
		return EngineState{}, &ApiError{Code: "engine_port_in_use", Message: l10n.TR("core.engine.err.port_in_use")}
	}
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	listen, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return EngineState{}, &ApiError{Code: "engine_port_in_use", Message: l10n.TR("core.engine.err.port_in_use")}
	}
	_ = listen.Close()
	logDir := filepath.Join(*w, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return EngineState{}, Err("internal_error")
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("engine-%d.log", time.Now().UnixNano()))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
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
		apiErr := &ApiError{Code: "engine_start_failed", Message: l10n.TR("core.engine.err.start_failed")}
		if detail := sanitizeErrorDetail(err.Error()); detail != "" {
			apiErr.Details = map[string]any{"detail": detail}
		}
		return EngineState{}, apiErr
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
			State:         "starting",
			Local:         startup.Local,
			Origin:        origin,
			Family:        startup.Family,
			Adapter:       startup.Adapter,
			Executable:    executable,
			EngineName:    startup.EngineName,
			Model:         startup.ModelPath,
			ModelName:     startup.ModelName,
			ModelKind:     startup.ModelKind,
			ServedModel:   startup.ServedModel,
			Port:          port,
			RequestedPort: startup.RequestedPort,
			ContextSize:   startup.ContextSize,
			GPULayers:     cloneIntPtr(startup.GPULayers),
			Threads:       cloneIntPtr(startup.Threads),
			ExtraArgs:     append([]string(nil), startup.ExtraArgs...),
			StartedAt:     now(),
			WebUIDisabled: startup.WebUIDisabled,
			Pid:           int64(cmd.Process.Pid),
			Endpoint:      endpoint,
			LogPath:       logPath,
			Candidate:     ref,
		},
	}
	if failure := s.persistEngineOwner(entry, *w); failure != nil {
		if entry.state.Local {
			if cleanupErr := s.terminateOwnedTree(&entry.owner, ""); cleanupErr != nil {
				_ = writeEngineOwner(entry.ownerPath, entry.owner)
				orphanState := clone(entry.state)
				orphanState.State = "orphaned"
				orphanState.Error = cleanupErr
				s.orphan = &orphanEngine{owner: entry.owner, path: entry.ownerPath, state: orphanState}
				s.Bus.Publish("engine.state", map[string]any{"engine": orphanState})
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		return EngineState{}, failure
	}
	s.engine = entry
	s.publishEngineLocked(entry)
	go s.monitorEngine(entry, endpoint)
	return clone(entry.state), nil
}

func (s *Service) StopEngine() (EngineState, *ApiError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runsActiveLocked() {
		return EngineState{}, Err("run_active")
	}
	if s.engine != nil {
		s.stopEngineLocked(s.engine)
		return clone(s.engine.state), nil
	}
	return s.stopPrimaryManagedServiceLocked(), nil
}

func (s *Service) stopEngineLocked(e *engineEntry) {
	if e.state.State != "starting" && e.state.State != "running" {
		return
	}
	e.state.State = "stopping"
	close(e.stop)
	s.publishEngineLocked(e)
}

func (s *Service) detachEngineLocked(e *engineEntry) {
	if e.detach == nil {
		return
	}
	select {
	case <-e.detach:
	default:
		close(e.detach)
	}
}

func (s *Service) publishEngineLocked(e *engineEntry) {
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
	if e.state.ServiceID != "" || normalizeEngineOrigin(e.state.Origin) == EngineOriginStandalone {
		s.publishServicesStateLocked()
	}
}

func (s *Service) terminateEngine(e *engineEntry, waitCh <-chan error) {
	s.mu.Lock()
	canUnload := e.family == "polystrata" && e.wasHealthy && e.state.Endpoint != ""
	endpoint := e.state.Endpoint
	s.mu.Unlock()
	if canUnload {
		_ = unloadPolystrata(endpoint)
	}
	if e.state.Local {
		if s.terminateOwnedTree(&e.owner, e.ownerPath) == nil {
			<-waitCh
		}
		return
	}
	signalEngine(e, syscall.SIGTERM)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-waitCh:
	case <-timer.C:
		signalEngine(e, os.Kill)
		<-waitCh
	}
}

func signalEngine(e *engineEntry, sig os.Signal) {
	if runtime.GOOS == "windows" || sig == os.Kill {
		_ = e.cmd.Process.Kill()
	} else {
		_ = e.cmd.Process.Signal(sig)
	}
}

func (s *Service) engineStartTimeout() time.Duration {
	if s.engineTimeout > 0 {
		return s.engineTimeout
	}
	return 10 * time.Minute
}

func (s *Service) stopEngineForHandoff(ctx context.Context) *ApiError {
	s.mu.Lock()
	if s.orphan != nil {
		s.mu.Unlock()
		return Err("engine_orphaned")
	}
	for _, run := range s.runs {
		if run.commandActive {
			s.mu.Unlock()
			return Err("run_active")
		}
	}
	entry := s.engine
	if entry == nil {
		s.mu.Unlock()
		return nil
	}
	s.stopEngineLocked(entry)
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return Err("cancelled")
	case <-entry.done:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphan != nil {
		return Err("engine_orphaned")
	}
	if entry.state.State != "stopped" && entry.state.State != "failed" {
		return Err("engine_stop_failed")
	}
	return nil
}
