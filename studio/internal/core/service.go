package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/remote"
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
	EngineUpdate       EngineUpdateOptions
	RemoteHub          remote.Hub
}

type preparingService struct {
	estimatedMemoryGB float64
	gpuLayers         *int
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
	mu                     sync.Mutex
	settingsMu             sync.Mutex
	reportMu               sync.Mutex
	presetsMu              sync.Mutex
	exclusivityMu          sync.Mutex
	settings               UserSettings
	keepServicesOnShutdown *bool
	exitNotices            []ServiceExitNotice
	exitNoticesLoaded      bool
	adoptPollInterval      time.Duration
	runtimeMemInterval     time.Duration
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
	services          map[string]*engineEntry
	orphanedServices  map[string]*orphanEngine
	reservedPorts     map[int]bool
	preparingServices map[string]preparingService
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
	probeExecMu       sync.Mutex
	serveProbeExecMu  sync.Mutex
	progressExecMu    sync.Mutex
	probeGen          uint64
	probePath         string
	probeVersion      string
	probeSize         int64
	probeModTime      int64
	probeCheckedAt    time.Time
	probeResult       InferyardProbe
	serveProbePath    string
	serveProbeVersion string
	serveProbeSize    int64
	serveProbeModTime int64
	serveProbeChecked time.Time
	serveSupported    bool
	updater           *updateManager
	engineUpdater     *engineUpdateManager
	cliInstallMu      sync.Mutex
	cliUpgradeMu      sync.Mutex
	cliUpgrade        CLIUpgradeState
	cliUpgradeCancel  context.CancelFunc
	cliUpgradeOnce    sync.Once
	caches            serviceCaches
	nodes             *NodeManager
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
	s := &Service{ctx: serviceCtx, cancel: cancel, shutdownDone: make(chan struct{}), queueDone: make(chan struct{}), settingsPath: o.SettingsPath, exec: o.Execute, execStream: o.ExecStream, Bus: NewEventBus(), wake: make(chan struct{}, 1), hostStatePath: o.HostStatePath, engineTimeout: o.EngineStartTimeout, services: map[string]*engineEntry{}, orphanedServices: map[string]*orphanEngine{}, reservedPorts: map[int]bool{}, preparingServices: map[string]preparingService{}, checks: map[string]engineCheckEntry{}, checkInflight: map[string]bool{}, checkSupported: true, checkSem: make(chan struct{}, 2), checkRetry: time.Minute}
	s.updater = newUpdateManager(s, o.Update)
	s.engineUpdater = newEngineUpdateManager(s, o.EngineUpdate)
	raw, err := os.ReadFile(o.SettingsPath)
	if err == nil {
		if err = json.Unmarshal(raw, &s.settings); err != nil {
			s.settings = UserSettings{}
			log.Print("settings: invalid JSON or field types; using defaults")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	s.paths = o.Paths
	if s.paths != nil {
		s.settings = s.paths.bind(s.settings)
		s.registryRoots = func(string) []string { return []string{s.paths.Engines} }
	}
	s.identityReader, s.processSignal, s.orphanStopTimeout = o.ProcessStartTicks, o.SignalProcess, o.OrphanStopTimeout
	nodes, err := newNodeManager(s, o.RemoteHub)
	if err != nil {
		cancel()
		return nil, err
	}
	s.nodes = nodes
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
	if s.modelsRoot(s.settings) != nil {
		s.RefreshAssets()
		s.RefreshDevice()
	}
	return s, nil
}
func (s *Service) userSettings() UserSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.settings)
}
func (s *Service) Settings() SettingsResponse {
	u := s.userSettings()
	p := s.probe(u)
	return SettingsResponse{u, EffectiveSettings{
		WorkspaceRoot:      workspaceRoot(u),
		InferyardPath:      p.Path,
		InferyardVersion:   p.Version,
		InferyardStatus:    p.Status,
		ResultsRoot:        s.resultsRoot(u),
		ModelsRoot:         s.modelsRoot(u),
		ExtraModelsRoots:   append([]string(nil), u.ExtraModelsRoots...),
		DownloadSource:     NormalizeDownloadSource(u.DownloadSource),
		DownloadProxy:      u.DownloadProxy,
		Theme:              NormalizeTheme(u.Theme),
		Accent:             NormalizeAccent(u.Accent),
		FontSize:           NormalizeFontSize(u.FontSize),
		Language:           NormalizeLanguage(u.Language),
		ReasoningEffort:    NormalizeReasoningEffort(u.ReasoningEffort),
		MinimizeToTray:     u.MinimizeToTray,
		KeepServicesOnQuit: EffectiveKeepServicesOnQuitValue(u),
		ReportsRoot:        s.reportsRoot(u),
	}}
}
func (s *Service) UpdateSettings(patch map[string]json.RawMessage) (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	for k := range patch {
		if !isAllowedSettingKey(k, false) {
			return SettingsResponse{}, Err("invalid_request")
		}
	}
	if s.paths != nil {
		for k := range patch {
			if !isAllowedSettingKey(k, true) {
				return SettingsResponse{}, Conflict("paths_bound")
			}
		}
	}
	u := s.userSettings()
	for _, k := range []string{"workspace_root", "inferyard_path"} {
		v, ok := patch[k]
		if !ok {
			continue
		}
		var p *string
		if json.Unmarshal(v, &p) != nil || (p == nil && k != "inferyard_path") || (p != nil && (!filepath.IsAbs(*p) || strings.ContainsRune(*p, 0))) {
			return SettingsResponse{}, Err("invalid_request")
		}
		if k == "workspace_root" {
			u.WorkspaceRoot = p
		} else {
			u.InferyardPath = p
		}
	}
	for _, k := range []string{"extra_models_roots", "download_source", "download_proxy", "theme", "accent", "font_size", "language", "reasoning_effort", "minimize_to_tray", "keep_services_on_quit"} {
		v, ok := patch[k]
		if !ok {
			continue
		}
		if _, apiErr := s.applySettingsPatchExtra(&u, k, v); apiErr != nil {
			return SettingsResponse{}, apiErr
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
	if workspaceChanged && (s.orphan != nil || len(s.orphanedServices) > 0 || len(s.preparingServices) > 0 || s.runsActiveLocked() || s.hasActiveManagedServicesLocked() || (s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping"))) {
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
	changed := str(s.resultsRoot(u)) != str(s.resultsRoot(s.settings))
	modelsChanged := str(s.modelsRoot(u)) != str(s.modelsRoot(s.settings)) || !slicesEqualStrings(u.ExtraModelsRoots, s.settings.ExtraModelsRoots)
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
		s.services = map[string]*engineEntry{}
		s.orphanedServices = map[string]*orphanEngine{}
		s.exitNotices = nil
		s.exitNoticesLoaded = false
	}
	s.settings = u
	if changed {
		s.rebuilding = true
		s.history = RunHistoryResponse{ResultsRoot: s.resultsRoot(u), ScannedAt: now(), Batches: []BatchSummary{}, StandaloneRuns: []RunSummary{}}
	}
	s.mu.Unlock()
	if workspaceChanged || changed {
		s.caches.clearAll()
	}
	if cliChanged {
		s.clearProbeCache()
		s.evictProgressCache()
		if !forceCLIRefresh && s.probe(u).Status == "ok" {
			s.clearCLIUpgradeState()
		}
	}
	if workspaceChanged {
		s.detectEngineOwner()
	}
	if changed {
		go s.rebuild(u)
	}
	if (modelsChanged || cliChanged) && len(s.allModelsRoots(u)) > 0 {
		s.RefreshAssets()
		s.RefreshDevice()
	}
	return s.Settings(), nil
}
