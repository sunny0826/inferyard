package core

import (
	"context"
	"errors"
	"inferyard/studio/internal/l10n"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	UpdateIdle         = "idle"
	UpdateChecking     = "checking"
	UpdateUpToDate     = "up_to_date"
	UpdateAvailable    = "available"
	UpdateDownloading  = "downloading"
	UpdateVerifying    = "verifying"
	UpdateReadyToApply = "ready_to_apply"
	UpdateApplying     = "applying"
	UpdateError        = "error"
)

const (
	defaultUpdateAPIURL       = "https://api.github.com/repos/sunny0826/inferyard/releases/latest"
	defaultUpdateStartupDelay = 2 * time.Second
	defaultUpdateCheckTimeout = 10 * time.Second
)

var ErrUpdateWorkloadActive = errors.New("active workloads must be confirmed before applying update")

type UpdateRelease struct {
	Version      string `json:"version"`
	TagName      string `json:"tag_name"`
	Name         string `json:"name"`
	Notes        string `json:"notes"`
	ReleaseURL   string `json:"release_url"`
	PublishedAt  string `json:"published_at,omitempty"`
	AssetName    string `json:"asset_name,omitempty"`
	AssetURL     string `json:"asset_url,omitempty"`
	AssetSize    int64  `json:"asset_size,omitempty"`
	ChecksumsURL string `json:"checksums_url,omitempty"`
}

type UpdateActiveWorkloads struct {
	ActiveRuns      int `json:"active_runs"`
	ActiveDownloads int `json:"active_downloads"`
}

func (w UpdateActiveWorkloads) Any() bool {
	return w.ActiveRuns > 0 || w.ActiveDownloads > 0
}

type UpdateState struct {
	Seq             uint64                `json:"seq,omitempty"`
	State           string                `json:"state"`
	CurrentVersion  string                `json:"current_version"`
	Release         *UpdateRelease        `json:"release,omitempty"`
	DownloadedBytes int64                 `json:"downloaded_bytes,omitempty"`
	TotalBytes      *int64                `json:"total_bytes,omitempty"`
	BytesPerSecond  *float64              `json:"bytes_per_second,omitempty"`
	CheckedAt       string                `json:"checked_at,omitempty"`
	ManualCheck     bool                  `json:"manual_check,omitempty"`
	ErrorCode       string                `json:"error_code,omitempty"`
	ErrorMessage    string                `json:"error_message,omitempty"`
	FallbackPath    string                `json:"fallback_path,omitempty"`
	CanSelfUpdate   bool                  `json:"can_self_update"`
	SelfUpdateBlock string                `json:"self_update_block,omitempty"`
	Workloads       UpdateActiveWorkloads `json:"workloads"`
}

type updateInstallTarget struct {
	ExePath       string
	AppPath       string
	InstallDir    string
	CanSelfUpdate bool
	BlockCode     string
	BlockMessage  string
}

type platformApplyPlan struct {
	ArtifactPath    string
	StagingDir      string
	ExePath         string
	AppPath         string
	InstallDir      string
	TargetVersion   string
	ReleaseURL      string
	ErrorRecordPath string
	SkipRelaunch    bool
	Shutdown        func() error
	RequestQuit     func()
	PreserveDir     func()
	RunCmd          func(context.Context, string, ...string) ([]byte, error)
}

type UpdateOptions struct {
	APIURL            string
	CurrentVersion    string
	GOOS              string
	GOARCH            string
	StartupDelay      time.Duration
	CheckTimeout      time.Duration
	StagingRoot       string
	AllowInsecureHTTP bool
	HTTPClient        *http.Client
	ExecutablePath    func() (string, error)
	InspectTarget     func() updateInstallTarget
	ApplyFunc         func(context.Context, platformApplyPlan) error
	CommandRunner     func(context.Context, string, ...string) ([]byte, error)
	RequestQuit         func()
	SkipRelaunch        bool
	DisableAutoDownload bool
}

type updateManager struct {
	service               *Service
	opts                  UpdateOptions
	mu                    sync.Mutex
	state                 UpdateState
	opSeq                 uint64
	cancel                context.CancelFunc
	startupOnce           sync.Once
	stagedDir             string
	stagedArtifact        string
	stagedSHA256          string
	applyInFlight         bool
	preserveStagingOnExit bool
	requestQuit           func()
}

func newUpdateManager(s *Service, opts UpdateOptions) *updateManager {
	if opts.APIURL == "" {
		opts.APIURL = defaultUpdateAPIURL
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.StartupDelay == 0 {
		opts.StartupDelay = defaultUpdateStartupDelay
	}
	if opts.CheckTimeout == 0 {
		opts.CheckTimeout = defaultUpdateCheckTimeout
	}
	if opts.ExecutablePath == nil {
		opts.ExecutablePath = os.Executable
	}
	m := &updateManager{
		service:     s,
		opts:        opts,
		requestQuit: opts.RequestQuit,
		state: UpdateState{
			State:          UpdateIdle,
			CurrentVersion: opts.CurrentVersion,
			CanSelfUpdate:  true,
		},
	}
	if m.state.CurrentVersion == "" {
		m.state.CurrentVersion = Version
	}
	return m
}

func (m *updateManager) currentVersionLocked() string {
	if m.opts.CurrentVersion != "" {
		return m.opts.CurrentVersion
	}
	return Version
}

func (m *updateManager) publishLocked() {
	m.state.Seq++
	if m.service == nil || m.service.Bus == nil {
		return
	}
	snap := m.cloneStateLocked()
	_ = m.service.Bus.Publish("update.state", map[string]any{"update": snap})
}

func (m *updateManager) cloneStateLocked() UpdateState {
	out := m.state
	out.CurrentVersion = m.currentVersionLocked()
	if m.state.Release != nil {
		rel := *m.state.Release
		out.Release = &rel
	}
	if m.state.TotalBytes != nil {
		v := *m.state.TotalBytes
		out.TotalBytes = &v
	}
	if m.state.BytesPerSecond != nil {
		v := *m.state.BytesPerSecond
		out.BytesPerSecond = &v
	}
	return out
}

func (m *updateManager) stagingBaseDir() (string, error) {
	if m.opts.StagingRoot != "" {
		if err := os.MkdirAll(m.opts.StagingRoot, 0o700); err != nil {
			return "", err
		}
		return m.opts.StagingRoot, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		cacheDir = os.TempDir()
	}
	root := filepath.Join(cacheDir, "InferYard Studio", "updates")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func (m *updateManager) removeStagedLocked() {
	if m.stagedDir != "" {
		_ = os.RemoveAll(m.stagedDir)
	}
	m.stagedDir = ""
	m.stagedArtifact = ""
	m.stagedSHA256 = ""
	m.preserveStagingOnExit = false
	m.state.FallbackPath = ""
}

func (m *updateManager) cleanupOnShutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if !m.preserveStagingOnExit {
		m.removeStagedLocked()
	}
}

func (m *updateManager) inspectTarget() updateInstallTarget {
	if m.opts.InspectTarget != nil {
		return m.opts.InspectTarget()
	}
	if m.service != nil && m.service.paths != nil {
		return updateInstallTarget{
			CanSelfUpdate: false,
			BlockCode:     "update_dev_mode",
			BlockMessage:  l10n.TR("core.update.err.dev_mode"),
		}
	}
	exePath, err := m.opts.ExecutablePath()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return updateInstallTarget{
			CanSelfUpdate: false,
			BlockCode:     "update_executable_unknown",
			BlockMessage:  l10n.TR("core.update.err.executable_unknown"),
		}
	}
	return inspectPlatformInstallTarget(exePath)
}

func (s *Service) SetUpdateQuitHook(fn func()) {
	if s == nil || s.updater == nil {
		return
	}
	s.updater.mu.Lock()
	s.updater.requestQuit = fn
	s.updater.mu.Unlock()
}

func (s *Service) activeUpdateWorkloadsLocked() UpdateActiveWorkloads {
	var w UpdateActiveWorkloads
	for _, r := range s.runs {
		if !terminal(r.Task.State) {
			w.ActiveRuns++
		}
	}
	for _, d := range s.downloads {
		if d.Task.State == "downloading" {
			w.ActiveDownloads++
		}
	}
	return w
}

func (s *Service) ActiveUpdateWorkloads() UpdateActiveWorkloads {
	if s == nil {
		return UpdateActiveWorkloads{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeUpdateWorkloadsLocked()
}

func (s *Service) beginUpdateApplyAdmission(confirmStop bool) (UpdateActiveWorkloads, *ApiError, bool) {
	if s == nil {
		return UpdateActiveWorkloads{}, nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return UpdateActiveWorkloads{}, Err("service_closing"), false
	}
	if s.updateApplying {
		return UpdateActiveWorkloads{}, Conflict("update_applying"), false
	}
	workloads := s.activeUpdateWorkloadsLocked()
	if workloads.Any() && !confirmStop {
		return workloads, nil, false
	}
	s.updateApplying = true
	return workloads, nil, true
}

func (s *Service) endUpdateApplyAdmission() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.updateApplying = false
	s.mu.Unlock()
}

func (s *Service) UpdateStatus() UpdateState {
	if s == nil || s.updater == nil {
		return UpdateState{State: UpdateIdle, CurrentVersion: Version}
	}
	workloads := s.ActiveUpdateWorkloads()
	m := s.updater
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := m.cloneStateLocked()
	if snap.State == UpdateAvailable || snap.State == UpdateReadyToApply || snap.State == UpdateDownloading || snap.State == UpdateVerifying {
		snap.Workloads = workloads
	}
	return snap
}

func (m *updateManager) maybeAutoDownload(ctx context.Context, st UpdateState, async bool) {
	if m.opts.DisableAutoDownload || st.State != UpdateAvailable || !st.CanSelfUpdate ||
		st.Release == nil || st.Release.AssetURL == "" || st.Release.ChecksumsURL == "" {
		return
	}
	run := func() {
		_, _ = m.downloadUpdateWithSeq(ctx, false, st.Seq)
	}
	if async {
		go run()
		return
	}
	run()
}

func (s *Service) StartBackgroundUpdateCheck() {
	if s == nil || s.updater == nil {
		return
	}
	m := s.updater
	m.startupOnce.Do(func() {
		if m.consumePendingApplyError() {
			return
		}
		delay := m.opts.StartupDelay
		go func() {
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-s.ctx.Done():
					return
				case <-timer.C:
				}
			}
			if s.ctx.Err() != nil {
				return
			}
			m.mu.Lock()
			if m.state.State != UpdateIdle {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			st, err := m.checkForUpdate(s.ctx, false)
			if err == nil {
				m.maybeAutoDownload(s.ctx, st, false)
			}
		}()
	})
}

func (s *Service) CheckForUpdate(ctx context.Context) (UpdateState, error) {
	if s == nil || s.updater == nil {
		return UpdateState{State: UpdateIdle, CurrentVersion: Version}, errors.New("update service unavailable")
	}
	st, err := s.updater.checkForUpdate(ctx, true)
	if err == nil {
		s.updater.maybeAutoDownload(s.ctx, st, true)
	}
	return st, err
}

func (s *Service) DownloadUpdate(ctx context.Context) (UpdateState, error) {
	if s == nil || s.updater == nil {
		return UpdateState{State: UpdateIdle, CurrentVersion: Version}, errors.New("update service unavailable")
	}
	return s.updater.downloadUpdate(ctx, true)
}

func (s *Service) CancelUpdate() UpdateState {
	if s == nil || s.updater == nil {
		return UpdateState{State: UpdateIdle, CurrentVersion: Version}
	}
	return s.updater.cancelUpdate()
}

func (s *Service) ApplyUpdate(ctx context.Context, confirmStopWorkloads bool) (UpdateState, error) {
	if s == nil || s.updater == nil {
		return UpdateState{State: UpdateIdle, CurrentVersion: Version}, errors.New("update service unavailable")
	}
	return s.updater.applyUpdate(ctx, confirmStopWorkloads)
}

func (s *Service) StartOneClickUpdate() {
	if s == nil || s.updater == nil {
		return
	}
	m := s.updater
	go func() {
		state, err := m.downloadUpdate(s.ctx, true)
		if err != nil || state.State != UpdateReadyToApply {
			return
		}
		_, _ = m.applyUpdate(s.ctx, false)
	}()
}

func (s *Service) cleanupUpdateStagingOnShutdown() {
	if s == nil || s.updater == nil {
		return
	}
	s.updater.cleanupOnShutdown()
}
