package core

import (
	"context"
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

type EngineState struct {
	StartTicks int64     `json:"start_ticks"`
	Local      bool      `json:"local"`
	State      string    `json:"state"`
	Pid        int64     `json:"pid"`
	Endpoint   string    `json:"endpoint"`
	LogPath    string    `json:"log_path"`
	Candidate  string    `json:"candidate"`
	Error      *ApiError `json:"error,omitempty"`
}

type engineEntry struct {
	ownerPath string
	owner     engineOwner
	cmd       *exec.Cmd
	logFile   *os.File
	state     EngineState
	stop      chan struct{}
	done      chan struct{}
}

func (s *Service) Engine() EngineState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphan != nil {
		return clone(s.orphan.state)
	}
	if s.engine == nil {
		return EngineState{State: "stopped"}
	}
	return clone(s.engine.state)
}

func (s *Service) StartEngine(candidate string) (EngineState, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return EngineState{}, Err("service_closing")
	}
	startup, e := s.Startup(candidate)
	if e != nil {
		return EngineState{}, e
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
	if s.orphan != nil {
		return clone(s.orphan.state), &ApiError{Code: "engine_orphaned", Message: "请先处理工作区 logs/engine-owner.json 对应的遗留服务或无效记录，再启动引擎。"}
	}
	if s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping") {
		if s.engine.state.Candidate != candidate {
			return clone(s.engine.state), Err("engine_candidate_mismatch")
		}
		return clone(s.engine.state), nil
	}
	s.detectEngineOwnerLocked()
	if s.orphan != nil {
		return clone(s.orphan.state), Err("engine_orphaned")
	}
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
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
	if err := configureEngineProcess(cmd, startup); err != nil {
		_ = logFile.Close()
		return EngineState{}, Err("engine_process_isolation_failed")
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return EngineState{}, &ApiError{Code: "engine_start_failed", Message: "引擎进程启动失败，请检查文件权限与运行环境。"}
	}
	entry := &engineEntry{cmd: cmd, logFile: logFile, stop: make(chan struct{}), done: make(chan struct{}), state: EngineState{State: "starting", Local: startup.Local, Pid: int64(cmd.Process.Pid), Endpoint: endpoint, LogPath: logPath, Candidate: candidate}}
	if failure := s.persistEngineOwner(entry, *w); failure != nil {
		if entry.state.Local {
			// A failed owner write must still clean already spawned workers. Never
			// replace a competing record while rolling this startup back.
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
	if s.engine == nil {
		return EngineState{State: "stopped"}, nil
	}
	s.stopEngineLocked(s.engine)
	return clone(s.engine.state), nil
}

func (s *Service) stopEngineLocked(e *engineEntry) {
	if e.state.State != "starting" && e.state.State != "running" {
		return
	}
	e.state.State = "stopping"
	close(e.stop)
	s.publishEngineLocked(e)
}

func (s *Service) publishEngineLocked(e *engineEntry) {
	s.Bus.Publish("engine.state", map[string]any{"engine": clone(e.state)})
}

// The monitor is the only waiter and terminator. It deliberately does not
// share the run cancellation context: Shutdown must finish run cleanup first.
func (s *Service) monitorEngine(e *engineEntry, endpoint string) {
	defer close(e.done)
	defer e.logFile.Close()
	waitCh := make(chan error, 1)
	go func() { waitCh <- e.cmd.Wait() }()
	health := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(s.engineStartTimeout())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	treeTicker := time.NewTicker(500 * time.Millisecond)
	defer treeTicker.Stop()
	finish := func(state string, err *ApiError) {
		treeErr := s.finishEngineTree(e)
		s.mu.Lock()
		defer s.mu.Unlock()
		if treeErr != nil {
			state, err = "failed", treeErr
			s.orphan = &orphanEngine{owner: e.owner, path: e.ownerPath, state: EngineState{State: "orphaned", Pid: e.owner.PID, StartTicks: e.owner.StartTicks, Local: e.owner.Local, Endpoint: e.owner.Endpoint, Candidate: e.owner.Candidate, Error: err}}
		} else if removeErr := removeEngineOwner(e.ownerPath, e.owner); removeErr != nil {
			state, err = "failed", &ApiError{Code: "engine_owner_remove_failed", Message: "引擎已退出，但工作区 logs/engine-owner.json 未能清除；可仅删除记录（不终止进程）。"}
			s.orphan = &orphanEngine{owner: e.owner, path: e.ownerPath, state: EngineState{State: "orphaned", Error: err}}
		}
		e.state.State, e.state.Error = state, err
		s.publishEngineLocked(e)
	}
	for {
		select {
		case <-e.stop:
			s.terminateEngine(e, waitCh)
			finish("stopped", nil)
			return
		case <-waitCh:
			s.mu.Lock()
			stopping := e.state.State == "stopping"
			s.mu.Unlock()
			if stopping {
				finish("stopped", nil)
			} else {
				finish("failed", &ApiError{Code: "engine_exited", Message: "引擎进程已退出，请检查日志后手动重启并重新绑定。"})
			}
			return
		case <-treeTicker.C:
			if e.state.Local {
				_ = s.refreshEngineTree(&e.owner, e.ownerPath)
			}
		case <-ticker.C:
			s.mu.Lock()
			starting := e.state.State == "starting"
			s.mu.Unlock()
			if !starting {
				continue
			}
			healthy := false
			if resp, err := health.Get(endpoint + engineHealthPath(e.state.Local)); err == nil {
				_ = resp.Body.Close()
				healthy = resp.StatusCode >= 200 && resp.StatusCode < 300
			}
			s.mu.Lock()
			if e.state.State != "starting" {
				s.mu.Unlock()
				continue
			}
			if healthy {
				e.state.State = "running"
				s.publishEngineLocked(e)
				s.mu.Unlock()
				continue
			}
			// During shutdown even a start timeout must wait for run cleanup.
			timedOut := time.Now().After(deadline) && !s.closing
			if timedOut {
				s.stopEngineLocked(e)
			}
			s.mu.Unlock()
			if timedOut {
				s.terminateEngine(e, waitCh)
				finish("failed", &ApiError{Code: "engine_start_timeout", Message: "引擎启动超时（模型加载未完成），请检查引擎日志。"})
				return
			}
		}
	}
}

func (s *Service) terminateEngine(e *engineEntry, waitCh <-chan error) {
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

func engineHealthPath(local bool) string {
	if local {
		return "/v1/models"
	}
	return "/health"
}

// Called only by the serial run queue between CLI children.
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
