package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/l10n"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m *engineUpdateManager) cachePath() string {
	if strings.TrimSpace(m.opts.CachePath) != "" {
		return m.opts.CachePath
	}
	if m.service != nil && m.service.settingsPath != "" {
		if cfgDir, err := os.UserConfigDir(); err == nil {
			defaultSettings := filepath.Join(cfgDir, "InferYard Studio", "settings.json")
			if filepath.Clean(m.service.settingsPath) != filepath.Clean(defaultSettings) {
				return filepath.Join(filepath.Dir(m.service.settingsPath), engineUpdateCacheFileName)
			}
		} else {
			return filepath.Join(filepath.Dir(m.service.settingsPath), engineUpdateCacheFileName)
		}
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cacheDir, "InferYard Studio", engineUpdateCacheFileName)
}

func (m *engineUpdateManager) loadCacheOnce() {
	m.mu.Lock()
	if m.cacheLoaded {
		m.mu.Unlock()
		return
	}
	m.cacheLoaded = true
	path := m.cachePath()
	m.mu.Unlock()

	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc engineUpdateDiskCache
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 || len(doc.Entries) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, entry := range doc.Entries {
		if strings.TrimSpace(entry.Version) == "" {
			continue
		}
		if existing, ok := m.remote[k]; !ok || entry.FetchedAt.After(existing.FetchedAt) {
			m.remote[k] = entry
		}
	}
}

func (m *engineUpdateManager) saveCacheLocked() {
	path := m.cachePath()
	if path == "" {
		return
	}
	doc := engineUpdateDiskCache{
		Version: 1,
		Entries: make(map[string]remoteEngineRelease, len(m.remote)),
	}
	for k, v := range m.remote {
		doc.Entries[k] = v
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (m *engineUpdateManager) httpClient() (*http.Client, *ApiError) {
	if m.opts.HTTPClient != nil {
		return m.opts.HTTPClient, nil
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxyRaw := strings.TrimSpace(m.service.downloadProxy()); proxyRaw != "" {
		if !ValidateDownloadProxy(proxyRaw) {
			return nil, &ApiError{Code: "invalid_proxy", Message: l10n.TR("core.update.err.invalid_proxy")}
		}
		proxyURL, err := url.Parse(proxyRaw)
		if err != nil {
			return nil, &ApiError{Code: "invalid_proxy", Message: l10n.TR("core.update.err.invalid_proxy")}
		}
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Transport: tr,
		Timeout:   defaultEngineUpdateHTTPTimeout,
	}, nil
}

func (m *engineUpdateManager) fetchUpstreamRelease(ctx context.Context, client *http.Client, target engineCheckUpstreamTarget) (remoteEngineRelease, error) {
	switch target.family {
	case "llama-cpp":
		repo := "ggml-org/llama.cpp"
		dist := "ggml-org"
		if strings.EqualFold(target.distribution, "PrismML") {
			repo = "PrismML-Eng/llama.cpp"
			dist = "PrismML"
		}
		return m.fetchGitHubRelease(ctx, client, target.key, target.family, dist, repo, false)
	case "polystrata":
		rel, err := m.fetchGitHubRelease(ctx, client, target.key, target.family, "simplexai-labs", "simplexai-labs/PolyStrata", true)
		if err != nil {
			return remoteEngineRelease{}, err
		}
		tag := strings.TrimSpace(rel.Version)
		if tag != "" && !strings.ContainsAny(tag, "/\\ \t\r\n?#") && tag != "." && tag != ".." {
			rel.DownloadURL = "https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/" + tag + ".tar.gz"
		}
		return rel, nil
	case "vllm":
		platform := m.opts.Platform
		if strings.TrimSpace(platform) == "" {
			platform = CurrentPlatform()
		}
		if platform == "darwin/arm64" {
			return m.fetchDarwinVLLMRelease(ctx, client, target.key)
		}
		return m.fetchPyPIRelease(ctx, client, target.key, target.family, venvPackageInstallName(target.family))
	case "mlx-lm":
		return m.fetchPyPIRelease(ctx, client, target.key, target.family, venvPackageInstallName(target.family))
	default:
		return remoteEngineRelease{}, fmt.Errorf("unsupported engine family: %s", target.family)
	}
}

func (m *engineUpdateManager) fetchGitHubRelease(ctx context.Context, client *http.Client, key, family, dist, repo string, allowPrereleaseFallback bool) (remoteEngineRelease, error) {
	base := strings.TrimRight(m.opts.GitHubAPIBase, "/")
	latestURL := base + "/repos/" + repo + "/releases/latest"
	payload, status, err := m.getGitHubReleasePayload(ctx, client, latestURL)
	if err != nil && status == http.StatusNotFound && allowPrereleaseFallback {
		listURL := base + "/repos/" + repo + "/releases?per_page=5"
		payload, err = m.getFirstNonDraftGitHubRelease(ctx, client, listURL, false)
	}
	if err != nil && isGitHubRateLimitStatus(status) && m.isDefaultGitHubAPI() {
		return m.fallbackGitHubReleaseViaWeb(ctx, client, key, family, dist, repo)
	}
	if err != nil {
		return remoteEngineRelease{}, err
	}
	buildRelease := func(p githubReleasePayload) remoteEngineRelease {
		assets := make([]remoteReleaseAsset, 0, len(p.Assets))
		for _, a := range p.Assets {
			dl := strings.TrimSpace(a.BrowserDownloadURL)
			if dl == "" || (!validEngineSource(dl) && !m.opts.AllowInsecureHTTP) {
				continue
			}
			assets = append(assets, remoteReleaseAsset{
				Name:  strings.TrimSpace(a.Name),
				URL:   dl,
				Bytes: a.Size,
			})
		}
		return remoteEngineRelease{
			Key:          key,
			Family:       family,
			Distribution: dist,
			Version:      strings.TrimSpace(p.TagName),
			ReleaseURL:   strings.TrimSpace(p.HTMLURL),
			Assets:       assets,
		}
	}
	rel := buildRelease(payload)
	if family == "llama-cpp" && !hasArchiveReleaseAsset(rel.Assets) {
		listURL := base + "/repos/" + repo + "/releases?per_page=5"
		if alt, altErr := m.getFirstNonDraftGitHubRelease(ctx, client, listURL, true); altErr == nil {
			if altRel := buildRelease(alt); altRel.Version != "" && hasArchiveReleaseAsset(altRel.Assets) {
				rel = altRel
			}
		}
	}
	if rel.Version == "" {
		return remoteEngineRelease{}, fmt.Errorf("empty tag_name for %s", repo)
	}
	return rel, nil
}

func (m *engineUpdateManager) getGitHubReleasePayload(ctx context.Context, client *http.Client, rawURL string) (githubReleasePayload, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return githubReleasePayload{}, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "InferYard-Studio/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return githubReleasePayload{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubReleasePayload{}, resp.StatusCode, fmt.Errorf("github status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEngineUpdateResponseBytes))
	if err != nil {
		return githubReleasePayload{}, resp.StatusCode, err
	}
	var payload githubReleasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return githubReleasePayload{}, resp.StatusCode, err
	}
	if payload.Draft {
		return githubReleasePayload{}, resp.StatusCode, fmt.Errorf("draft release")
	}
	return payload, resp.StatusCode, nil
}

func (m *engineUpdateManager) getFirstNonDraftGitHubRelease(ctx context.Context, client *http.Client, rawURL string, requireArchiveAsset bool) (githubReleasePayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return githubReleasePayload{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "InferYard-Studio/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return githubReleasePayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubReleasePayload{}, fmt.Errorf("github list status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEngineUpdateResponseBytes))
	if err != nil {
		return githubReleasePayload{}, err
	}
	var releases []githubReleasePayload
	if err := json.Unmarshal(body, &releases); err != nil {
		return githubReleasePayload{}, err
	}
	for _, rel := range releases {
		if rel.Draft || strings.TrimSpace(rel.TagName) == "" {
			continue
		}
		if requireArchiveAsset {
			hasArchive := false
			for _, a := range rel.Assets {
				lower := strings.ToLower(strings.TrimSpace(a.Name))
				if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip") {
					hasArchive = true
					break
				}
			}
			if !hasArchive {
				continue
			}
		}
		return rel, nil
	}
	return githubReleasePayload{}, fmt.Errorf("no usable release")
}

func (m *engineUpdateManager) fetchPyPIRelease(ctx context.Context, client *http.Client, key, family, pkg string) (remoteEngineRelease, error) {
	base := strings.TrimRight(m.opts.PyPIAPIBase, "/")
	rawURL := base + "/pypi/" + pkg + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return remoteEngineRelease{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "InferYard-Studio/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return remoteEngineRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return remoteEngineRelease{}, fmt.Errorf("pypi status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEngineUpdateResponseBytes))
	if err != nil {
		return remoteEngineRelease{}, err
	}
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return remoteEngineRelease{}, err
	}
	ver := strings.TrimSpace(doc.Info.Version)
	if ver == "" {
		return remoteEngineRelease{}, fmt.Errorf("empty pypi version for %s", pkg)
	}
	return remoteEngineRelease{
		Key:          key,
		Family:       family,
		Distribution: "PyPI",
		Version:      ver,
		ReleaseURL:   fmt.Sprintf("https://pypi.org/project/%s/%s/", pkg, ver),
	}, nil
}

func matchLlamaReleaseAsset(assets []remoteReleaseAsset, platform string, wantCUDA bool) (remoteReleaseAsset, bool) {
	var candidates []remoteReleaseAsset
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if !(strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip")) {
			continue
		}
		if strings.Contains(lower, "cudart") || strings.Contains(lower, "xcframework") || strings.Contains(lower, "source") {
			continue
		}
		candidates = append(candidates, a)
	}
	pick := func(matchers ...func(string) bool) (remoteReleaseAsset, bool) {
		for _, fn := range matchers {
			var matched []remoteReleaseAsset
			for _, a := range candidates {
				if fn(strings.ToLower(a.Name)) {
					matched = append(matched, a)
				}
			}
			if len(matched) > 0 {
				sort.SliceStable(matched, func(i, j int) bool {
					ti := strings.HasSuffix(strings.ToLower(matched[i].Name), ".tar.gz") || strings.HasSuffix(strings.ToLower(matched[i].Name), ".tgz")
					tj := strings.HasSuffix(strings.ToLower(matched[j].Name), ".tar.gz") || strings.HasSuffix(strings.ToLower(matched[j].Name), ".tgz")
					return ti && !tj
				})
				return matched[0], true
			}
		}
		return remoteReleaseAsset{}, false
	}

	switch platform {
	case "darwin/arm64":
		return pick(
			func(n string) bool { return strings.Contains(n, "macos-arm64") && !strings.Contains(n, "kleidiai") },
			func(n string) bool { return strings.Contains(n, "macos-arm64") },
		)
	case "darwin/amd64":
		return pick(func(n string) bool { return strings.Contains(n, "macos-x64") || strings.Contains(n, "macos-amd64") })
	case "windows/amd64":
		if wantCUDA {
			return pick(
				func(n string) bool { return strings.Contains(n, "win-cuda-12.4") && strings.Contains(n, "x64") },
				func(n string) bool { return strings.Contains(n, "win-cuda-12") && strings.Contains(n, "x64") },
				func(n string) bool { return strings.Contains(n, "win-cuda") && strings.Contains(n, "x64") },
			)
		}
		return pick(
			func(n string) bool { return strings.Contains(n, "win-cpu-x64") },
			func(n string) bool { return strings.Contains(n, "win-avx2-x64") },
			func(n string) bool { return strings.Contains(n, "win-vulkan-x64") },
		)
	case "linux/amd64":
		return pick(
			func(n string) bool {
				return (strings.Contains(n, "ubuntu-x64") || strings.Contains(n, "linux-x64")) &&
					!strings.Contains(n, "rocm") && !strings.Contains(n, "sycl") && !strings.Contains(n, "vulkan") && !strings.Contains(n, "cuda")
			},
			func(n string) bool { return strings.Contains(n, "ubuntu-x64") || strings.Contains(n, "linux-x64") },
		)
	case "linux/arm64":
		return pick(func(n string) bool { return strings.Contains(n, "ubuntu-arm64") || strings.Contains(n, "linux-arm64") })
	default:
		return remoteReleaseAsset{}, false
	}
}
