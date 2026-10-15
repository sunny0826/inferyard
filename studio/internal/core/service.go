package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version is set from the embedded desktop configuration before serving requests.
var Version = "dev"

type Executor func(context.Context, string, ...string) (inferyard.Execution, error)
type Options struct {
	ProcessStartTicks func(int64) (int64, error)
	SignalProcess     func(int64, os.Signal) error
	OrphanStopTimeout time.Duration
	SettingsPath      string
	Execute           Executor
	HostStatePath     string
	// EngineStartTimeout bounds the managed engine's health wait; tests set a
	// short value, zero means ten minutes.
	EngineStartTimeout time.Duration
}
type Service struct {
	orphan            *orphanEngine
	identityReader    func(int64) (int64, error)
	processSignal     func(int64, os.Signal) error
	orphanStopTimeout time.Duration
	registryRoots     func(string) []string
	ctx               context.Context
	cancel            context.CancelFunc
	closing           bool
	shutdownOnce      sync.Once
	shutdownDone      chan struct{}
	queueDone         chan struct{}
	mu                sync.Mutex
	settingsMu        sync.Mutex
	reportMu          sync.Mutex
	settings          UserSettings
	settingsPath      string
	exec              Executor
	Bus               *EventBus
	device            DeviceResponse
	deviceGeneration  uint64
	history           RunHistoryResponse
	rebuilding        bool
	runs              []*runEntry
	downloads         []*downloadEntry
	engine            *engineEntry
	engineTimeout     time.Duration
	wake              chan struct{}
	hostStatePath     string
	checkMu           sync.Mutex
	checks            map[string]engineCheckEntry
	checkInflight     map[string]bool
	checkNames        []engineCheckTarget
	checkCLI          string
	checkGeneration   uint64
	checkSupported    bool
	checkSem          chan struct{}
	checkRetry        time.Duration
}

func New(ctx context.Context, o Options) (*Service, error) {
	if o.SettingsPath == "" {
		d, e := os.UserConfigDir()
		if e != nil {
			return nil, e
		}
		o.SettingsPath = filepath.Join(d, "InferYard Studio", "settings.json")
	}
	if o.Execute == nil {
		o.Execute = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			return (inferyard.Client{Path: path}).Exec(ctx, args...)
		}
	}
	serviceCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{ctx: serviceCtx, cancel: cancel, shutdownDone: make(chan struct{}), queueDone: make(chan struct{}), settingsPath: o.SettingsPath, exec: o.Execute, Bus: NewEventBus(), wake: make(chan struct{}, 1), hostStatePath: o.HostStatePath, engineTimeout: o.EngineStartTimeout, checks: map[string]engineCheckEntry{}, checkInflight: map[string]bool{}, checkSupported: true, checkSem: make(chan struct{}, 2), checkRetry: time.Minute}
	raw, err := os.ReadFile(o.SettingsPath)
	if err == nil {
		if err = json.Unmarshal(raw, &s.settings); err != nil {
			s.settings = UserSettings{}
			log.Print("settings: invalid JSON or field types; using defaults")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	s.settings = downloadNameDefaults(s.settings)
	s.identityReader, s.processSignal, s.orphanStopTimeout = o.ProcessStartTicks, o.SignalProcess, o.OrphanStopTimeout
	s.detectEngineOwner()
	s.history = RunHistoryResponse{ScannedAt: now(), Batches: []BatchSummary{}, StandaloneRuns: []RunSummary{}}
	go s.runQueue()
	go func() {
		select {
		case <-ctx.Done():
			s.beginShutdown()
		case <-s.shutdownDone:
		}
	}()
	if s.settings.ModelsRoot != nil {
		s.RefreshDevice()
	}
	return s, nil
}
func ptr[T any](v T) *T { return &v }
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[:])
}
func clone[T any](v T) T { raw, _ := json.Marshal(v); var c T; _ = json.Unmarshal(raw, &c); return c }
func Err(code string) *ApiError {
	if code == "output_name_exhausted" {
		return &ApiError{Code: code, Message: "前 1000 个目录名均已占用，请更换目录名或工作区。"}
	}
	messages := map[string]string{"invalid_request": "请求参数无效", "not_found": "未找到资源", "conflict": "操作与当前状态冲突", "cli_blocked": "inferyard 拒绝执行", "internal_error": "服务内部错误", "cli_error": "inferyard 工具或证据错误", "cli_unavailable": "inferyard 不可用"}
	return &ApiError{Code: code, Message: messages[code], Limitations: []string{}}
}
func Conflict(reason string) *ApiError {
	e := Err("conflict")
	e.Details = map[string]any{"reason": reason}
	if reason == "output_exists" {
		e.Message = "输出目录已存在，请更换目录名"
	}
	return e
}
func (e *ApiError) Error() string { return e.Message }
func exitError(out inferyard.Execution, err error) *ApiError {
	if err != nil {
		return Err("internal_error")
	}
	if out.ExitCode == 0 || out.ExitCode == 3 || out.ExitCode == 130 {
		return nil
	}
	code := "cli_error"
	if out.ExitCode == 2 {
		code = "cli_blocked"
	}
	e := Err(code)
	if r, p := inferyard.Parse(out.Stdout); p == nil && r.Limitations != nil {
		e.Limitations = r.Limitations
	}
	return e
}
func (s *Service) userSettings() UserSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.settings)
}
func (s *Service) probe(u UserSettings) InferyardProbe {
	path := str(u.InferyardPath)
	if path == "" {
		path = "inferyard"
	}
	p, e := exec.LookPath(path)
	if e != nil {
		status := "not_found"
		if _, e := os.Stat(path); e == nil {
			status = "not_executable"
		}
		return InferyardProbe{Status: status}
	}
	p, _ = filepath.Abs(p)
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	out, e := s.exec(ctx, p, "--version")
	if e != nil || out.ExitCode != 0 {
		return InferyardProbe{Path: &p, Status: "version_failed"}
	}
	v, e := inferyard.ParseVersion(out.Stdout)
	if e != nil {
		return InferyardProbe{Path: &p, Status: "version_failed"}
	}
	return InferyardProbe{Path: &p, Version: &v, Status: "ok"}
}
func reportsRoot(u UserSettings) *string {
	if u.ResultsRoot == nil {
		return nil
	}
	return ptr(filepath.Join(filepath.Dir(*u.ResultsRoot), "reports"))
}
func (s *Service) Settings() SettingsResponse {
	u := s.userSettings()
	p := s.probe(u)
	return SettingsResponse{u, EffectiveSettings{workspaceRoot(u), p.Path, p.Version, p.Status, u.ResultsRoot, u.ModelsRoot, reportsRoot(u)}}
}
func (s *Service) UpdateSettings(patch map[string]json.RawMessage) (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	for k, v := range patch {
		if k == "model_download_name" || k == "engine_download_name" {
			var name string
			if json.Unmarshal(v, &name) != nil || !ValidDownloadName(name) {
				return SettingsResponse{}, Err("invalid_request")
			}
			if k == "model_download_name" {
				u.ModelDownloadName = name
			} else {
				u.EngineDownloadName = name
			}
			continue
		}
		if k != "inferyard_path" && k != "results_root" && k != "models_root" && k != "workspace_root" {
			return SettingsResponse{}, Err("invalid_request")
		}
		var p *string
		if json.Unmarshal(v, &p) != nil || (p == nil && k != "inferyard_path") || (p != nil && (!filepath.IsAbs(*p) || strings.ContainsRune(*p, 0))) {
			return SettingsResponse{}, Err("invalid_request")
		}
		switch k {
		case "workspace_root":
			u.WorkspaceRoot = p
		case "inferyard_path":
			u.InferyardPath = p
		case "results_root":
			u.ResultsRoot = p
		case "models_root":
			u.ModelsRoot = p
		}
	}
	return s.saveSettings(u)
}
func (s *Service) saveSettings(u UserSettings) (SettingsResponse, *ApiError) {
	s.mu.Lock()
	workspaceChanged := str(workspaceRoot(u)) != str(workspaceRoot(s.settings))
	if workspaceChanged && (s.orphan != nil || s.runsActiveLocked() || (s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping"))) {
		s.mu.Unlock()
		return SettingsResponse{}, Err("workspace_busy")
	}
	s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.settingsPath), 0700); err != nil {
		return SettingsResponse{}, Err("internal_error")
	}
	if err := os.Chmod(filepath.Dir(s.settingsPath), 0700); err != nil {
		return SettingsResponse{}, Err("internal_error")
	}
	data, _ := json.Marshal(u)
	f, err := os.CreateTemp(filepath.Dir(s.settingsPath), "settings-*.json")
	if err != nil {
		return SettingsResponse{}, Err("internal_error")
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(data)
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(name, s.settingsPath)
	}
	if err != nil {
		return SettingsResponse{}, Err("internal_error")
	}
	s.mu.Lock()
	changed := str(u.ResultsRoot) != str(s.settings.ResultsRoot)
	if str(u.ModelsRoot) != str(s.settings.ModelsRoot) {
		s.deviceGeneration++
		s.device.Snapshot = nil
		s.device.CapturedAt = nil
		s.device.LastError = nil
	}
	if workspaceChanged {
		s.engine = nil
	}
	s.settings = u
	if changed {
		s.rebuilding = true
	}
	s.mu.Unlock()
	if workspaceChanged {
		s.detectEngineOwner()
	}
	if changed {
		go s.rebuild(u)
	}
	return s.Settings(), nil
}
func (s *Service) Status() StatusResponse {
	p := s.probe(s.userSettings())
	s.mu.Lock()
	defer s.mu.Unlock()
	r := StatusResponse{Status: "ok", Version: Version, Inferyard: p}
	for _, t := range s.runs {
		if !terminal(t.Task.State) {
			r.ActiveRunTasks++
		}
	}
	for _, d := range s.downloads {
		if d.Task.State == "downloading" {
			r.ActiveDownloads++
		}
	}
	return r
}
func (s *Service) Device() DeviceResponse { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.device) }
func (s *Service) RefreshDevice() *ApiError {
	s.mu.Lock()
	if s.device.Refreshing {
		s.mu.Unlock()
		return Conflict("refresh_in_progress")
	}
	u := clone(s.settings)
	if u.ModelsRoot == nil {
		s.mu.Unlock()
		return Conflict("settings_incomplete")
	}
	s.device.Refreshing = true
	gen := s.deviceGeneration
	s.mu.Unlock()
	go func() {
		p := s.probe(u)
		var failure *ApiError
		var snapshot json.RawMessage
		if p.Status != "ok" {
			failure = Err("cli_unavailable")
		} else {
			out, e := s.exec(s.ctx, *p.Path, "device-check", "--json", "--models-root", *u.ModelsRoot)
			if e != nil {
				failure = Err("internal_error")
			} else if out.ExitCode != 0 && out.ExitCode != 2 {
				failure = exitError(out, nil)
				if failure == nil {
					failure = Err("cli_error")
				}
			} else {
				r, e := inferyard.Parse(out.Stdout)
				if e == nil {
					_, e = inferyard.ParseDeviceCheck(r)
				}
				if e != nil {
					failure = Err("internal_error")
				} else {
					snapshot = r.Details
				}
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.device.Refreshing = false
		if gen != s.deviceGeneration {
			return
		}
		if failure == nil {
			s.device.Snapshot = snapshot
			s.device.CapturedAt = ptr(now())
		}
		s.device.LastError = failure
		s.Bus.Publish("device.updated", map[string]any{"snapshot": s.device.Snapshot, "captured_at": s.device.CapturedAt, "error": failure})
	}()
	return nil
}

// Resolve includes symlink resolution for existing ancestors of new outputs.
func Resolve(root, path string) (string, error) {
	if root == "" || path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	root, e := canonical(root)
	if e != nil {
		return "", e
	}
	path, e = canonical(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("outside root")
	}
	return path, nil
}
func canonical(p string) (string, error) {
	p = filepath.Clean(p)
	resolved, e := filepath.EvalSymlinks(p)
	if e == nil {
		return resolved, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", e
	}
	r, e := canonical(parent)
	if e != nil {
		return "", e
	}
	return filepath.Join(r, filepath.Base(p)), nil
}
func terminal(state string) bool { return state == "done" || state == "failed" || state == "cancelled" }
