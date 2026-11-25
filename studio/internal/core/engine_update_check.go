package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	EngineUpdateCheckIdle     = "idle"
	EngineUpdateCheckChecking = "checking"
	EngineUpdateCheckReady    = "ready"
	EngineUpdateCheckError    = "error"

	defaultEngineUpdateStartupDelay = 3 * time.Second
	defaultEngineUpdateCacheTTL     = 6 * time.Hour
	defaultEngineUpdateHTTPTimeout  = 15 * time.Second
	defaultGitHubAPIBase            = "https://api.github.com"
	defaultPyPIAPIBase              = "https://pypi.org"
	engineUpdateCacheFileName       = "engine-update-cache.v1.json"
	maxEngineUpdateResponseBytes    = 4 << 20
)

type EngineUpdateOptions struct {
	HTTPClient             *http.Client
	GitHubAPIBase          string
	PyPIAPIBase            string
	StartupDelay           time.Duration
	CacheTTL               time.Duration
	CachePath              string
	Platform               string
	DisableBackgroundCheck bool
	AllowInsecureHTTP      bool
	Now                    func() time.Time
	ExecUpgrade            func(ctx context.Context, name string, args ...string) (inferyard.Execution, error)
}

type EngineUpdatesStatus struct {
	State          string                      `json:"state"`
	CheckedAt      *string                     `json:"checked_at,omitempty"`
	UpdatableCount int                         `json:"updatable_count"`
	Error          *ApiError                   `json:"error,omitempty"`
	Updates        map[string]EngineUpdateInfo `json:"updates"`
}

type remoteReleaseAsset struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Bytes int64  `json:"bytes,omitempty"`
}

type remoteEngineRelease struct {
	Key          string               `json:"key"`
	Family       string               `json:"family"`
	Distribution string               `json:"distribution,omitempty"`
	Version      string               `json:"version"`
	ReleaseURL   string               `json:"release_url,omitempty"`
	DownloadURL  string               `json:"download_url,omitempty"`
	Assets       []remoteReleaseAsset `json:"assets,omitempty"`
	FetchedAt    time.Time            `json:"fetched_at"`
}

type engineUpdateDiskCache struct {
	Version int                            `json:"version"`
	Entries map[string]remoteEngineRelease `json:"entries"`
}

type engineUpdateManager struct {
	service        *Service
	opts           EngineUpdateOptions
	mu             sync.Mutex
	startupOnce    sync.Once
	cacheLoaded    bool
	checking       bool
	checkDone      chan struct{}
	state          string
	checkedAt      *string
	lastErr        *ApiError
	remote         map[string]remoteEngineRelease
	binaryVersions map[string]binaryVersionCacheEntry
}

func newEngineUpdateManager(s *Service, opts EngineUpdateOptions) *engineUpdateManager {
	if strings.TrimSpace(opts.GitHubAPIBase) == "" {
		opts.GitHubAPIBase = defaultGitHubAPIBase
	}
	if strings.TrimSpace(opts.PyPIAPIBase) == "" {
		opts.PyPIAPIBase = defaultPyPIAPIBase
	}
	if opts.StartupDelay <= 0 {
		opts.StartupDelay = defaultEngineUpdateStartupDelay
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = defaultEngineUpdateCacheTTL
	}
	if strings.TrimSpace(opts.Platform) == "" {
		opts.Platform = CurrentPlatform()
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &engineUpdateManager{
		service:        s,
		opts:           opts,
		state:          EngineUpdateCheckIdle,
		remote:         map[string]remoteEngineRelease{},
		binaryVersions: map[string]binaryVersionCacheEntry{},
	}
}

func (m *engineUpdateManager) remoteSnapshot() map[string]remoteEngineRelease {
	m.loadCacheOnce()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]remoteEngineRelease, len(m.remote))
	for k, v := range m.remote {
		out[k] = v
	}
	return out
}

func (m *engineUpdateManager) clearBinaryVersion(binPath string) {
	if strings.TrimSpace(binPath) == "" {
		return
	}
	m.mu.Lock()
	delete(m.binaryVersions, filepath.Clean(binPath))
	m.mu.Unlock()
}

func (s *Service) StartBackgroundEngineUpdateCheck() {
	if s == nil || s.engineUpdater == nil || s.engineUpdater.opts.DisableBackgroundCheck {
		return
	}
	s.engineUpdater.startupOnce.Do(func() {
		go func() {
			timer := time.NewTimer(s.engineUpdater.opts.StartupDelay)
			defer timer.Stop()
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
			}
			_, _ = s.checkEngineUpdatesInternal(s.ctx, false)
		}()
	})
}

func (s *Service) CheckEngineUpdates(ctx context.Context) (EngineUpdatesStatus, *ApiError) {
	if ctx == nil {
		ctx = s.ctx
	}
	return s.checkEngineUpdatesInternal(ctx, true)
}

func (s *Service) EngineUpdateStatus() EngineUpdatesStatus {
	updates := s.EngineUpdates()
	if s == nil || s.engineUpdater == nil {
		return EngineUpdatesStatus{
			State:          EngineUpdateCheckIdle,
			UpdatableCount: countUpdatableEngineTargets(updates),
			Updates:        updates,
		}
	}
	m := s.engineUpdater
	m.loadCacheOnce()
	m.mu.Lock()
	state := m.state
	checkedAt := m.checkedAt
	lastErr := m.lastErr
	m.mu.Unlock()
	return EngineUpdatesStatus{
		State:          state,
		CheckedAt:      checkedAt,
		UpdatableCount: countUpdatableEngineTargets(updates),
		Error:          lastErr,
		Updates:        updates,
	}
}

func (s *Service) publishEngineUpdatesEvent(manual bool) {
	status := s.EngineUpdateStatus()
	s.Bus.Publish("engine.updates", map[string]any{
		"status": status,
		"manual": manual,
	})
}

type engineCheckUpstreamTarget struct {
	key          string
	family       string
	distribution string
}

func (s *Service) collectInstalledUpstreamTargets(ctx context.Context, probeBinaries bool) []engineCheckUpstreamTarget {
	reg := s.registry()
	checks := s.EngineChecks()
	seen := map[string]engineCheckUpstreamTarget{}

	addTarget := func(family, dist string) {
		switch family {
		case "llama-cpp":
			if dist == "" {
				dist = "ggml-org"
			}
			key := "llama-cpp:" + dist
			seen[key] = engineCheckUpstreamTarget{key: key, family: family, distribution: dist}
		case "polystrata":
			seen["polystrata"] = engineCheckUpstreamTarget{key: "polystrata", family: "polystrata", distribution: "simplexai-labs"}
		case "mlx-lm":
			seen["mlx-lm"] = engineCheckUpstreamTarget{key: "mlx-lm", family: "mlx-lm", distribution: "PyPI"}
		case "vllm":
			seen["vllm"] = engineCheckUpstreamTarget{key: "vllm", family: "vllm", distribution: "PyPI"}
		case "sglang":
			seen["sglang"] = engineCheckUpstreamTarget{key: "sglang", family: "sglang", distribution: "PyPI"}
		}
	}

	for _, entry := range reg.engines {
		fam := normalizeCatalogFamilyID(entry.engine.Family, entry.family, entry.engine.Name, entry.path)
		if fam == "" {
			continue
		}
		elig := checks.Engines[entry.engine.Name]
		verifiedRelease := ""
		if elig.State == EngineCheckEligible {
			verifiedRelease = elig.Release
		}
		installed := ""
		if probeBinaries {
			installed = s.extractInstalledVersion(ctx, entry, fam, true)
		}
		dist := ""
		if fam == "llama-cpp" {
			dist = detectLlamaDistribution(entry, installed, verifiedRelease)
		}
		addTarget(fam, dist)
	}
	for name, elig := range checks.Engines {
		fam := normalizeCatalogFamilyID(elig.Family, name)
		if fam == "" {
			continue
		}
		dist := ""
		if fam == "llama-cpp" && ((elig.State == EngineCheckEligible && strings.Contains(strings.ToLower(elig.Release), "prism")) || strings.Contains(strings.ToLower(name), "prism")) {
			dist = "PrismML"
		}
		addTarget(fam, dist)
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]engineCheckUpstreamTarget, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func (s *Service) checkEngineUpdatesInternal(ctx context.Context, manual bool) (EngineUpdatesStatus, *ApiError) {
	m := s.engineUpdater
	m.loadCacheOnce()

	for {
		m.mu.Lock()
		if !m.checking {
			m.checking = true
			m.checkDone = make(chan struct{})
			m.state = EngineUpdateCheckChecking
			m.lastErr = nil
			m.mu.Unlock()
			break
		}
		doneCh := m.checkDone
		m.mu.Unlock()
		if !manual || doneCh == nil {
			return s.EngineUpdateStatus(), nil
		}
		select {
		case <-ctx.Done():
			return s.EngineUpdateStatus(), Err("cancelled")
		case <-doneCh:
		}
	}
	if manual {
		s.publishEngineUpdatesEvent(true)
	}

	defer func() {
		m.mu.Lock()
		m.checking = false
		if m.checkDone != nil {
			close(m.checkDone)
			m.checkDone = nil
		}
		m.mu.Unlock()
	}()

	targets := s.collectInstalledUpstreamTargets(ctx, true)
	if len(targets) == 0 {
		nowStr := m.opts.Now().UTC().Format(time.RFC3339)
		m.mu.Lock()
		m.state = EngineUpdateCheckReady
		m.checkedAt = &nowStr
		m.lastErr = nil
		m.mu.Unlock()
		status := s.EngineUpdateStatus()
		s.publishEngineUpdatesEvent(manual)
		return status, nil
	}

	nowTime := m.opts.Now().UTC()
	var toFetch []engineCheckUpstreamTarget
	m.mu.Lock()
	for _, target := range targets {
		cached, ok := m.remote[target.key]
		if !manual && ok && nowTime.Sub(cached.FetchedAt) < m.opts.CacheTTL {
			continue
		}
		toFetch = append(toFetch, target)
	}
	m.mu.Unlock()

	if len(toFetch) == 0 {
		nowStr := nowTime.Format(time.RFC3339)
		m.mu.Lock()
		m.state = EngineUpdateCheckReady
		m.checkedAt = &nowStr
		m.lastErr = nil
		m.mu.Unlock()
		status := s.EngineUpdateStatus()
		s.publishEngineUpdatesEvent(manual)
		return status, nil
	}

	client, apiErr := m.httpClient()
	if apiErr != nil {
		m.mu.Lock()
		if manual {
			m.state = EngineUpdateCheckError
			m.lastErr = apiErr
		} else {
			m.state = EngineUpdateCheckIdle
		}
		m.mu.Unlock()
		status := s.EngineUpdateStatus()
		s.publishEngineUpdatesEvent(manual)
		if manual {
			return status, apiErr
		}
		return status, nil
	}

	type fetchResult struct {
		target  engineCheckUpstreamTarget
		release remoteEngineRelease
		err     error
	}
	results := make([]fetchResult, len(toFetch))
	var wg sync.WaitGroup
	for i, target := range toFetch {
		wg.Add(1)
		go func(idx int, t engineCheckUpstreamTarget) {
			defer wg.Done()
			rel, err := m.fetchUpstreamRelease(ctx, client, t)
			results[idx] = fetchResult{target: t, release: rel, err: err}
		}(i, target)
	}
	wg.Wait()

	succeeded := 0
	var firstErr error
	m.mu.Lock()
	for _, res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		if strings.TrimSpace(res.release.Version) != "" {
			res.release.FetchedAt = nowTime
			m.remote[res.target.key] = res.release
			succeeded++
		}
	}
	if succeeded > 0 {
		m.saveCacheLocked()
	}
	nowStr := nowTime.Format(time.RFC3339)
	if succeeded == 0 && firstErr != nil {
		if manual {
			m.state = EngineUpdateCheckError
			m.lastErr = &ApiError{
				Code:    "engine_update_check_failed",
				Message: l10n.TR("core.engine_update.err.check_failed"),
			}
		} else {
			m.state = EngineUpdateCheckIdle
		}
	} else {
		m.state = EngineUpdateCheckReady
		m.checkedAt = &nowStr
		m.lastErr = nil
	}
	errOut := m.lastErr
	m.mu.Unlock()

	status := s.EngineUpdateStatus()
	s.publishEngineUpdatesEvent(manual)
	if manual && errOut != nil {
		return status, errOut
	}
	return status, nil
}
