package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"inferyard-studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Executor func(context.Context, string, ...string) (inferyard.Execution, error)
type Options struct {
	SettingsPath  string
	Execute       Executor
	HostStatePath string
}
type Service struct {
	ctx              context.Context
	mu               sync.Mutex
	settingsMu       sync.Mutex
	reportMu         sync.Mutex
	settings         UserSettings
	settingsPath     string
	exec             Executor
	Bus              *EventBus
	device           DeviceResponse
	deviceGeneration uint64
	history          RunHistoryResponse
	rebuilding       bool
	runs             []*runEntry
	downloads        []*downloadEntry
	wake             chan struct{}
	hostStatePath    string
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
	s := &Service{ctx: ctx, settingsPath: o.SettingsPath, exec: o.Execute, Bus: NewEventBus(), wake: make(chan struct{}, 1), hostStatePath: o.HostStatePath}
	raw, err := os.ReadFile(o.SettingsPath)
	if err == nil {
		if err = json.Unmarshal(raw, &s.settings); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	s.history = RunHistoryResponse{ScannedAt: now(), Batches: []BatchSummary{}, StandaloneRuns: []RunSummary{}}
	go s.runQueue()
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
	messages := map[string]string{"invalid_request": "请求参数无效", "unauthorized": "会话验证失败", "not_found": "未找到资源", "conflict": "操作与当前状态冲突", "cli_blocked": "inferyard 拒绝执行", "internal_error": "服务内部错误", "cli_error": "inferyard 工具或证据错误", "cli_unavailable": "inferyard 不可用"}
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
func (e *ApiError) HTTPStatus() int {
	return map[string]int{"invalid_request": 400, "unauthorized": 401, "not_found": 404, "conflict": 409, "cli_blocked": 422, "internal_error": 500, "cli_error": 500, "cli_unavailable": 503}[e.Code]
}
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
	return SettingsResponse{u, EffectiveSettings{p.Path, p.Version, p.Status, u.ResultsRoot, u.ModelsRoot, reportsRoot(u)}}
}
func (s *Service) UpdateSettings(patch map[string]json.RawMessage) (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	for k, v := range patch {
		if k != "inferyard_path" && k != "results_root" && k != "models_root" {
			return SettingsResponse{}, Err("invalid_request")
		}
		var p *string
		if json.Unmarshal(v, &p) != nil || (p == nil && k != "inferyard_path") || (p != nil && (!filepath.IsAbs(*p) || strings.ContainsRune(*p, 0))) {
			return SettingsResponse{}, Err("invalid_request")
		}
		switch k {
		case "inferyard_path":
			u.InferyardPath = p
		case "results_root":
			u.ResultsRoot = p
		case "models_root":
			u.ModelsRoot = p
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.settingsPath), 0700); err != nil {
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
	s.settings = u
	if changed {
		s.rebuilding = true
	}
	s.mu.Unlock()
	if changed {
		go s.rebuild(u)
	}
	return s.Settings(), nil
}
func (s *Service) Status() StatusResponse {
	p := s.probe(s.userSettings())
	s.mu.Lock()
	defer s.mu.Unlock()
	r := StatusResponse{Status: "ok", Version: "0.1.0", Inferyard: p}
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
