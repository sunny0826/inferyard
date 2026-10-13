package core

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// EngineState is the snapshot of the Studio-managed model service. Studio owns
// the processes it starts: shutdown stops them, a crash surfaces as failed
// with the log path, and there is no automatic restart.
type EngineState struct {
	State     string    `json:"state"` // stopped, starting, running, stopping, failed
	Pid       int64     `json:"pid"`
	Endpoint  string    `json:"endpoint"`
	LogPath   string    `json:"log_path"`
	Candidate string    `json:"candidate"`
	Error     *ApiError `json:"error,omitempty"`
}

type engineEntry struct {
	cmd         *exec.Cmd
	intentional bool
	logFile     *os.File
	state       EngineState
}

// Engine returns the current managed-service snapshot; the zero value is
// stopped when Studio never started a service this session.
func (s *Service) Engine() EngineState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return EngineState{State: "stopped"}
	}
	return clone(s.engine.state)
}

// StartEngine launches the frozen engine of a bound candidate as a
// Studio-owned child process and waits for its health endpoint. Repeated
// calls while starting or running are a no-op returning the current state.
func (s *Service) StartEngine(candidate string) (EngineState, *ApiError) {
	s.mu.Lock()
	if s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping") {
		state := clone(s.engine.state)
		s.mu.Unlock()
		return state, nil
	}
	s.mu.Unlock()
	u := s.userSettings()
	w := workspaceRoot(u)
	if w == nil {
		return EngineState{}, Conflict("settings_incomplete")
	}
	startup, e := s.Startup(candidate)
	if e != nil {
		return EngineState{}, e
	}
	host, port := startupAddress(startup.Args)
	if port <= 0 {
		return EngineState{}, Err("invalid_request")
	}
	dialHost := host
	if dialHost == "" || dialHost == "0.0.0.0" || dialHost == "::" {
		dialHost = "127.0.0.1"
	}
	endpoint := "http://" + net.JoinHostPort(dialHost, strconv.Itoa(port))
	listen, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return EngineState{}, &ApiError{Code: "engine_port_in_use", Message: "端口已被占用：该端口可能已有模型服务。请先停止它，或改用「绑定已有进程」。"}
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
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return EngineState{}, &ApiError{Code: "engine_start_failed", Message: "引擎进程启动失败：" + err.Error()}
	}
	entry := &engineEntry{cmd: cmd, logFile: logFile, state: EngineState{State: "starting", Pid: int64(cmd.Process.Pid), Endpoint: endpoint, LogPath: logPath, Candidate: candidate}}
	s.mu.Lock()
	s.engine = entry
	s.mu.Unlock()
	s.publishEngine(entry)
	go s.monitorEngine(entry, dialHost, port)
	return clone(entry.state), nil
}

// StopEngine asks the managed service to terminate (SIGTERM with a grace
// window, then SIGKILL). Stopping an unmanaged or already-stopped service is
// a no-op.
func (s *Service) StopEngine() (EngineState, *ApiError) {
	s.mu.Lock()
	e := s.engine
	if e == nil || e.state.State == "stopped" || e.state.State == "failed" {
		state := EngineState{State: "stopped"}
		if e != nil {
			state = clone(e.state)
		}
		s.mu.Unlock()
		return state, nil
	}
	e.intentional = true
	e.state.State = "stopping"
	s.mu.Unlock()
	s.publishEngine(e)
	signalEngine(e, syscall.SIGTERM)
	return clone(e.state), nil
}

func (s *Service) publishEngine(e *engineEntry) {
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
}

// monitorEngine owns the process lifecycle after Start: health polling until
// the start deadline, crash detection, Studio shutdown, and the SIGTERM grace
// window before SIGKILL.
func (s *Service) monitorEngine(e *engineEntry, host string, port int) {
	waitCh := make(chan error, 1)
	go func() { waitCh <- e.cmd.Wait() }()
	defer func() { _ = e.logFile.Close() }()
	health := &http.Client{Timeout: 2 * time.Second}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/health"
	deadline := time.Now().Add(s.engineStartTimeout())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	finish := func(state string, err *ApiError) {
		s.mu.Lock()
		e.state.State = state
		e.state.Error = err
		s.mu.Unlock()
		s.publishEngine(e)
	}
	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			e.intentional = true
			e.state.State = "stopping"
			s.mu.Unlock()
			s.publishEngine(e)
			terminateEngine(e, waitCh)
			finish("stopped", nil)
			return
		case err := <-waitCh:
			s.mu.Lock()
			intentional := e.intentional
			s.mu.Unlock()
			if intentional {
				finish("stopped", nil)
				return
			}
			detail := "引擎进程已退出"
			if err != nil {
				detail = "引擎进程异常退出：" + err.Error()
			}
			finish("failed", &ApiError{Code: "engine_exited", Message: detail + "，日志见 " + e.state.LogPath})
			return
		case <-ticker.C:
			s.mu.Lock()
			state := e.state.State
			s.mu.Unlock()
			if state != "starting" {
				continue
			}
			if resp, err := health.Get(url); err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					s.mu.Lock()
					e.state.State = "running"
					s.mu.Unlock()
					s.publishEngine(e)
					continue
				}
			}
			if time.Now().After(deadline) {
				s.mu.Lock()
				e.intentional = true
				s.mu.Unlock()
				terminateEngine(e, waitCh)
				finish("failed", &ApiError{Code: "engine_start_timeout", Message: "引擎启动超时（模型加载未完成），日志见 " + e.state.LogPath})
				return
			}
		}
	}
}

// terminateEngine sends SIGTERM and escalates to SIGKILL after the grace
// window; the process reaped through waitCh.
func terminateEngine(e *engineEntry, waitCh chan error) {
	signalEngine(e, syscall.SIGTERM)
	select {
	case <-waitCh:
	case <-time.After(10 * time.Second):
		signalEngine(e, os.Kill)
		<-waitCh
	}
}

func signalEngine(e *engineEntry, sig os.Signal) {
	if e.cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" || sig == os.Kill {
		_ = e.cmd.Process.Kill()
		return
	}
	_ = e.cmd.Process.Signal(sig)
}

// startupAddress extracts --host/--port from the frozen startup arguments.
func startupAddress(args []string) (string, int) {
	host, port := "", 0
	for i, a := range args {
		if i+1 >= len(args) {
			continue
		}
		switch a {
		case "--host":
			host = args[i+1]
		case "--port":
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	return host, port
}

func (s *Service) engineStartTimeout() time.Duration {
	if s.engineTimeout > 0 {
		return s.engineTimeout
	}
	return 10 * time.Minute
}
