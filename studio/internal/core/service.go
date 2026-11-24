package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var Version = "dev"

type Executor func(context.Context, string, ...string) (inferyard.Execution, error)
type Options struct {
	Paths              *BoundPaths
	ProcessStartTicks  func(int64) (int64, error)
	SignalProcess      func(int64, os.Signal) error
	OrphanStopTimeout  time.Duration
	SettingsPath       string
	Execute            Executor
	ExecStream         func(context.Context, func(string), string, ...string) (inferyard.Execution, error)
	HostStatePath      string
	EngineStartTimeout time.Duration
	Update             UpdateOptions
}

type Service struct {
	paths             *BoundPaths
	orphan            *orphanEngine
	identityReader    func(int64) (int64, error)
	processSignal     func(int64, os.Signal) error
	orphanStopTimeout time.Duration
	registryRoots     func(string) []string
	ctx               context.Context
	cancel            context.CancelFunc
	closing           bool
	updateApplying    bool
	shutdownOnce      sync.Once
	shutdownDone      chan struct{}
	queueDone         chan struct{}
	speedPreparing    sync.WaitGroup
	mu                sync.Mutex
	settingsMu        sync.Mutex
	reportMu          sync.Mutex
	settings          UserSettings
	settingsPath      string
	exec              Executor
	execStream        func(context.Context, func(string), string, ...string) (inferyard.Execution, error)
	Bus               *EventBus
	device            DeviceResponse
	deviceGeneration  uint64
	assets            AssetsState
	assetsGen         uint64
	history           RunHistoryResponse
	rebuilding        bool
	runs              []*runEntry
	downloads         []*downloadEntry
	deletingAssets    int
	engine            *engineEntry
	candidateOrigins  map[string]localCandidateOrigin
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
	probeMu           sync.Mutex
	probePath         string
	probeVersion      string
	probeSize         int64
	probeModTime      int64
	probeCheckedAt    time.Time
	probeResult       InferyardProbe
	updater           *updateManager
}

func New(ctx context.Context, o Options) (*Service, error) {
	if o.Paths != nil {
		copy := *o.Paths
		o.Paths = &copy
		if err := o.Paths.validate(); err != nil {
			return nil, err
		}
		o.SettingsPath = o.Paths.Settings
	}
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
		if o.ExecStream == nil {
			o.ExecStream = func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
				return (inferyard.Client{Path: path}).ExecStream(ctx, onLine, args...)
			}
		}
	}
	if o.ExecStream == nil {
		execFn := o.Execute
		o.ExecStream = func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			out, err := execFn(ctx, path, args...)
			if onLine != nil && len(out.Stderr) > 0 {
				for _, line := range strings.Split(strings.TrimRight(string(out.Stderr), "\r\n"), "\n") {
					onLine(strings.TrimRight(line, "\r"))
				}
			}
			return out, err
		}
	}
	serviceCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{ctx: serviceCtx, cancel: cancel, shutdownDone: make(chan struct{}), queueDone: make(chan struct{}), settingsPath: o.SettingsPath, exec: o.Execute, execStream: o.ExecStream, Bus: NewEventBus(), wake: make(chan struct{}, 1), hostStatePath: o.HostStatePath, engineTimeout: o.EngineStartTimeout, checks: map[string]engineCheckEntry{}, checkInflight: map[string]bool{}, checkSupported: true, checkSem: make(chan struct{}, 2), checkRetry: time.Minute}
	s.updater = newUpdateManager(s, o.Update)
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
	s.paths = o.Paths
	if s.paths != nil {
		s.settings = s.paths.bind(s.settings)
		s.registryRoots = func(string) []string { return []string{s.paths.Engines} }
	}
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
		s.RefreshAssets()
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
		return &ApiError{Code: code, Message: l10n.TR("core.err.output_name_exhausted")}
	}
	var msg string
	switch code {
	case "invalid_request":
		msg = l10n.TR("core.err.invalid_request")
	case "not_found":
		msg = l10n.TR("core.err.not_found")
	case "conflict":
		msg = l10n.TR("core.err.conflict")
	case "cli_blocked":
		msg = l10n.TR("core.err.cli_blocked")
	case "internal_error":
		msg = l10n.TR("core.err.internal_error")
	case "cli_error":
		msg = l10n.TR("core.err.cli_error")
	case "cli_unavailable":
		msg = l10n.TR("core.err.cli_unavailable")
	case "cli_outdated":
		msg = l10n.TR("core.err.cli_outdated")
	}
	return &ApiError{Code: code, Message: msg, Limitations: []string{}}
}
func Conflict(reason string) *ApiError {
	e := Err("conflict")
	e.Details = map[string]any{"reason": reason}
	if reason == "output_exists" {
		e.Message = l10n.TR("core.err.output_exists")
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
	if r, p := inferyard.Parse(out.Stdout); p == nil {
		if r.Limitations != nil {
			e.Limitations = r.Limitations
		}
		if len(r.Details) > 0 && string(r.Details) != "null" {
			var parsed map[string]any
			if json.Unmarshal(r.Details, &parsed) == nil {
				sanitized := map[string]any{}
				for _, key := range []string{"failure_stage", "failure_cause"} {
					if v, ok := parsed[key].(string); ok && v != "" {
						sanitized[key] = v
					}
				}
				if len(sanitized) > 0 {
					e.Details = sanitized
				}
			}
		}
	}
	return e
}
func (s *Service) userSettings() UserSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.settings)
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
	return SettingsResponse{u, EffectiveSettings{
		WorkspaceRoot:    workspaceRoot(u),
		InferyardPath:    p.Path,
		InferyardVersion: p.Version,
		InferyardStatus:  p.Status,
		ResultsRoot:      u.ResultsRoot,
		ModelsRoot:       u.ModelsRoot,
		ExtraModelsRoots: append([]string(nil), u.ExtraModelsRoots...),
		DownloadProxy:    u.DownloadProxy,
		Theme:            NormalizeTheme(u.Theme),
		Language:         NormalizeLanguage(u.Language),
		MinimizeToTray:   u.MinimizeToTray,
		ReportsRoot:      reportsRoot(u),
	}}
}
func (s *Service) UpdateSettings(patch map[string]json.RawMessage) (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	for k, v := range patch {
		if s.paths != nil && k != "model_download_name" && k != "engine_download_name" && k != "download_proxy" && k != "theme" && k != "language" && k != "minimize_to_tray" {
			return SettingsResponse{}, Conflict("paths_bound")
		}
		if handled, apiErr := applySettingsPatchExtra(&u, k, v); handled {
			if apiErr != nil {
				return SettingsResponse{}, apiErr
			}
			continue
		}
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
	return s.persistSettings(u, false)
}

func (s *Service) persistSettings(u UserSettings, forceCLIRefresh bool) (SettingsResponse, *ApiError) {
	if s.paths != nil && !s.paths.matches(u) {
		return SettingsResponse{}, Conflict("paths_bound")
	}
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
	cliChanged := forceCLIRefresh || str(u.InferyardPath) != str(s.settings.InferyardPath)
	changed := str(u.ResultsRoot) != str(s.settings.ResultsRoot)
	modelsChanged := str(u.ModelsRoot) != str(s.settings.ModelsRoot) || !slicesEqualStrings(u.ExtraModelsRoots, s.settings.ExtraModelsRoots)
	if modelsChanged || cliChanged {
		s.deviceGeneration++
		s.device.Snapshot = nil
		s.device.CapturedAt = nil
		s.device.LastError = nil
		s.device.Refreshing = false
		s.assetsGen++
		s.assets = AssetsState{}
	}
	if workspaceChanged {
		s.engine = nil
	}
	s.settings = u
	if changed {
		s.rebuilding = true
	}
	s.mu.Unlock()
	if cliChanged {
		s.clearProbeCache()
	}
	if workspaceChanged {
		s.detectEngineOwner()
	}
	if changed {
		go s.rebuild(u)
	}
	if (modelsChanged || cliChanged) && len(allModelsRoots(u)) > 0 {
		s.RefreshAssets()
		s.RefreshDevice()
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
	roots := allModelsRoots(u)
	if len(roots) == 0 {
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
			failure = cliProbeError(p)
		} else {
			deviceArgs := append([]string{"device-check", "--json"}, modelsRootCLIArgs(roots)...)
			out, e := s.exec(s.ctx, *p.Path, deviceArgs...)
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
		if gen != s.deviceGeneration {
			return
		}
		s.device.Refreshing = false
		if failure == nil {
			s.device.Snapshot = snapshot
			s.device.CapturedAt = ptr(now())
		}
		s.device.LastError = failure
		s.Bus.Publish("device.updated", map[string]any{"snapshot": s.device.Snapshot, "captured_at": s.device.CapturedAt, "error": failure})
	}()
	return nil
}

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
