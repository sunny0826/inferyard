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

type EngineState struct {
	State     string    `json:"state"`
	Pid       int64     `json:"pid"`
	Endpoint  string    `json:"endpoint"`
	LogPath   string    `json:"log_path"`
	Candidate string    `json:"candidate"`
	Error     *ApiError `json:"error,omitempty"`
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
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return EngineState{}, &ApiError{Code: "engine_start_failed", Message: "引擎进程启动失败，请检查文件权限与运行环境。"}
	}
	entry := &engineEntry{cmd: cmd, logFile: logFile, stop: make(chan struct{}), done: make(chan struct{}), state: EngineState{State: "starting", Pid: int64(cmd.Process.Pid), Endpoint: endpoint, LogPath: logPath, Candidate: candidate}}
	if failure := s.persistEngineOwner(entry, *w); failure != nil {
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
	finish := func(state string, err *ApiError) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if removeErr := removeEngineOwner(e.ownerPath, e.owner); removeErr != nil {
			state, err = "failed", &ApiError{Code: "engine_owner_remove_failed", Message: "引擎已退出，但工作区 logs/engine-owner.json 未能清除；可仅删除记录（不终止进程）。"}
			s.orphan = &orphanEngine{owner: e.owner, path: e.ownerPath, state: EngineState{State: "orphaned", Error: err}}
		}
		e.state.State, e.state.Error = state, err
		s.publishEngineLocked(e)
	}
	for {
		select {
		case <-e.stop:
			terminateEngine(e, waitCh)
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
		case <-ticker.C:
			s.mu.Lock()
			starting := e.state.State == "starting"
			s.mu.Unlock()
			if !starting {
				continue
			}
			healthy := false
			if resp, err := health.Get(endpoint + "/health"); err == nil {
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
				terminateEngine(e, waitCh)
				finish("failed", &ApiError{Code: "engine_start_timeout", Message: "引擎启动超时（模型加载未完成），请检查引擎日志。"})
				return
			}
		}
	}
}

func terminateEngine(e *engineEntry, waitCh <-chan error) {
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
