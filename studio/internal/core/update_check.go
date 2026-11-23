package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type semver struct {
	major int
	minor int
	patch int
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubReleasePayload struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	HTMLURL     string               `json:"html_url"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	PublishedAt string               `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type updateCheckError struct {
	code    string
	message string
	cause   error
}

func (e *updateCheckError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}

func (e *updateCheckError) Unwrap() error { return e.cause }

func parseSemver(raw string) (semver, bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" {
		return semver{}, false
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var nums [3]int
	for i, part := range parts {
		if part == "" {
			return semver{}, false
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return semver{}, false
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2]}, true
}

func (v semver) string() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func compareSemver(a, b semver) int {
	if a.major != b.major {
		if a.major > b.major {
			return 1
		}
		return -1
	}
	if a.minor != b.minor {
		if a.minor > b.minor {
			return 1
		}
		return -1
	}
	if a.patch != b.patch {
		if a.patch > b.patch {
			return 1
		}
		return -1
	}
	return 0
}

func expectedUpdateAssetName(version, goos, goarch string) (string, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		return "", false
	}
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return fmt.Sprintf("inferyard-studio-v%s-darwin-arm64.dmg", v), true
	case "windows/amd64":
		return fmt.Sprintf("inferyard-studio-v%s-windows-amd64.exe", v), true
	case "linux/amd64":
		return fmt.Sprintf("inferyard-studio-v%s-linux-amd64.tar.gz", v), true
	default:
		return "", false
	}
}

func validateUpdateURL(raw string, allowInsecureHTTP bool) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsAny(trimmed, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "https" {
		return true
	}
	return allowInsecureHTTP && scheme == "http"
}

func (m *updateManager) httpClient(proxyRaw string, timeout time.Duration) (*http.Client, error) {
	if m.opts.HTTPClient != nil {
		return m.opts.HTTPClient, nil
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if trimmed := strings.TrimSpace(proxyRaw); trimmed != "" {
		if !ValidateDownloadProxy(trimmed) {
			return nil, &updateCheckError{code: "update_invalid_proxy", message: "下载代理配置无效，请在设置中检查后重试。"}
		}
		proxyURL, err := url.Parse(trimmed)
		if err != nil {
			return nil, &updateCheckError{code: "update_invalid_proxy", message: "下载代理配置无效，请在设置中检查后重试。", cause: err}
		}
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}, nil
}

func (m *updateManager) checkForUpdate(ctx context.Context, manual bool) (UpdateState, error) {
	if ctx == nil {
		if m.service != nil && m.service.ctx != nil {
			ctx = m.service.ctx
		} else {
			ctx = context.Background()
		}
	}

	m.mu.Lock()
	if m.applyInFlight {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		if !manual {
			return snap, nil
		}
		return snap, &updateCheckError{code: "update_busy", message: "更新正在处理中，请稍候。"}
	}
	switch m.state.State {
	case UpdateDownloading, UpdateVerifying, UpdateApplying:
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		if !manual {
			return snap, nil
		}
		return snap, &updateCheckError{code: "update_busy", message: "更新正在处理中，请稍候。"}
	case UpdateChecking:
		if !manual {
			snap := m.cloneStateLocked()
			m.mu.Unlock()
			return snap, nil
		}
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
	default:
		if !manual && m.state.State != UpdateIdle {
			snap := m.cloneStateLocked()
			m.mu.Unlock()
			return snap, nil
		}
	}

	prevWasReady := m.state.State == UpdateReadyToApply
	prevArtifact := m.stagedArtifact
	prevSHA := m.stagedSHA256
	var prevVer, prevAsset string
	if m.state.Release != nil {
		prevVer = m.state.Release.Version
		prevAsset = m.state.Release.AssetName
	}

	m.opSeq++
	seq := m.opSeq
	opCtx, cancel := context.WithTimeout(ctx, m.opts.CheckTimeout)
	m.cancel = cancel
	currentVerStr := m.currentVersionLocked()
	m.state.State = UpdateChecking
	m.state.ManualCheck = manual
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""
	m.state.Workloads = UpdateActiveWorkloads{}
	m.publishLocked()
	m.mu.Unlock()
	defer cancel()

	proxy := ""
	if m.service != nil {
		proxy = m.service.downloadProxy()
	}

	rel, newer, checkErr := m.fetchLatestRelease(opCtx, currentVerStr, proxy)

	var target updateInstallTarget
	var workloads UpdateActiveWorkloads
	var reuseReady bool
	var reusedSize int64
	if checkErr == nil && newer {
		target = m.inspectTarget()
		if m.service != nil {
			workloads = m.service.ActiveUpdateWorkloads()
		}
		if prevWasReady && prevArtifact != "" && prevSHA != "" && prevVer == rel.Version && prevAsset == rel.AssetName {
			if st, statErr := os.Stat(prevArtifact); statErr == nil && st.Mode().IsRegular() {
				reuseReady = true
				reusedSize = st.Size()
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if seq != m.opSeq {
		return m.cloneStateLocked(), opCtx.Err()
	}
	m.cancel = nil

	if checkErr != nil {
		if !manual {
			m.state.State = UpdateIdle
			m.state.ErrorCode = ""
			m.state.ErrorMessage = ""
			m.publishLocked()
			return m.cloneStateLocked(), checkErr
		}
		var ce *updateCheckError
		code, msg := "update_check_failed", "检查更新失败，请稍后重试。"
		if errors.As(checkErr, &ce) {
			code, msg = ce.code, ce.message
		}
		m.state.State = UpdateError
		m.state.CheckedAt = now()
		m.state.ErrorCode = code
		m.state.ErrorMessage = msg
		m.publishLocked()
		return m.cloneStateLocked(), checkErr
	}

	m.state.CheckedAt = now()
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""

	if !newer {
		m.removeStagedLocked()
		m.state.State = UpdateUpToDate
		m.state.Release = nil
		m.state.DownloadedBytes = 0
		m.state.TotalBytes = nil
		m.state.BytesPerSecond = nil
		m.state.CanSelfUpdate = true
		m.state.SelfUpdateBlock = ""
		m.state.Workloads = UpdateActiveWorkloads{}
		m.publishLocked()
		return m.cloneStateLocked(), nil
	}

	if reuseReady && m.stagedArtifact == prevArtifact && m.stagedSHA256 == prevSHA {
		m.state.State = UpdateReadyToApply
		m.state.Release = &rel
		m.state.DownloadedBytes = reusedSize
		if reusedSize > 0 {
			m.state.TotalBytes = ptr(reusedSize)
		} else if rel.AssetSize > 0 {
			m.state.TotalBytes = ptr(rel.AssetSize)
		} else {
			m.state.TotalBytes = nil
		}
		m.state.BytesPerSecond = nil
		m.state.Workloads = workloads
		m.state.CanSelfUpdate = target.CanSelfUpdate
		m.state.SelfUpdateBlock = target.BlockMessage
		if m.state.CanSelfUpdate && (rel.AssetURL == "" || rel.ChecksumsURL == "") {
			m.state.CanSelfUpdate = false
			m.state.SelfUpdateBlock = "当前发布未提供适用安装包或校验文件，请前往发布页下载。"
		}
		m.publishLocked()
		return m.cloneStateLocked(), nil
	}

	m.removeStagedLocked()
	m.state.State = UpdateAvailable
	m.state.Release = &rel
	m.state.DownloadedBytes = 0
	if rel.AssetSize > 0 {
		m.state.TotalBytes = ptr(rel.AssetSize)
	} else {
		m.state.TotalBytes = nil
	}
	m.state.BytesPerSecond = nil
	m.state.Workloads = workloads
	m.state.CanSelfUpdate = target.CanSelfUpdate
	m.state.SelfUpdateBlock = target.BlockMessage
	if m.state.CanSelfUpdate && (rel.AssetURL == "" || rel.ChecksumsURL == "") {
		m.state.CanSelfUpdate = false
		m.state.SelfUpdateBlock = "当前发布未提供适用安装包或校验文件，请前往发布页下载。"
	}
	m.publishLocked()
	return m.cloneStateLocked(), nil
}

func (m *updateManager) fetchLatestRelease(ctx context.Context, currentVerRaw, proxyRaw string) (UpdateRelease, bool, error) {
	currentVer, ok := parseSemver(currentVerRaw)
	if !ok {
		return UpdateRelease{}, false, &updateCheckError{
			code:    "update_version_invalid",
			message: fmt.Sprintf("当前版本 %q 无法进行版本比较。", currentVerRaw),
		}
	}
	if !validateUpdateURL(m.opts.APIURL, m.opts.AllowInsecureHTTP) {
		return UpdateRelease{}, false, &updateCheckError{
			code:    "update_invalid_url",
			message: "更新检查地址无效。",
		}
	}
	client, err := m.httpClient(proxyRaw, m.opts.CheckTimeout)
	if err != nil {
		return UpdateRelease{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.opts.APIURL, nil)
	if err != nil {
		return UpdateRelease{}, false, &updateCheckError{code: "update_check_failed", message: "创建更新检查请求失败。", cause: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "InferYard-Studio/"+currentVer.string())

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return UpdateRelease{}, false, err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return UpdateRelease{}, false, &updateCheckError{code: "update_timeout", message: "检查更新超时，请稍后重试。", cause: err}
		}
		return UpdateRelease{}, false, &updateCheckError{code: "update_network_error", message: "无法连接更新服务，请检查网络或代理设置。", cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return UpdateRelease{}, false, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden &&
			(resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.TrimSpace(resp.Header.Get("Retry-After")) != "")) {
		return UpdateRelease{}, false, &updateCheckError{code: "update_rate_limited", message: "GitHub 接口请求受限，请稍后再试。"}
	}
	if resp.StatusCode != http.StatusOK {
		return UpdateRelease{}, false, &updateCheckError{
			code:    "update_check_failed",
			message: fmt.Sprintf("更新服务返回异常状态（HTTP %d），请稍后重试。", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return UpdateRelease{}, false, &updateCheckError{code: "update_network_error", message: "读取版本信息失败，请重试。", cause: err}
	}
	var payload githubReleasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return UpdateRelease{}, false, &updateCheckError{code: "update_parse_failed", message: "解析更新信息失败，请稍后重试。", cause: err}
	}
	if payload.Draft || payload.Prerelease {
		return UpdateRelease{}, false, nil
	}
	latestVer, ok := parseSemver(payload.TagName)
	if !ok {
		return UpdateRelease{}, false, nil
	}
	if compareSemver(latestVer, currentVer) <= 0 {
		return UpdateRelease{}, false, nil
	}

	rel := UpdateRelease{
		Version:     latestVer.string(),
		TagName:     payload.TagName,
		Name:        strings.TrimSpace(payload.Name),
		Notes:       strings.TrimSpace(payload.Body),
		PublishedAt: payload.PublishedAt,
	}
	if rel.Name == "" {
		rel.Name = "v" + rel.Version
	}
	if validateUpdateURL(payload.HTMLURL, m.opts.AllowInsecureHTTP) {
		rel.ReleaseURL = payload.HTMLURL
	}

	wantAsset, hasAssetName := expectedUpdateAssetName(rel.Version, m.opts.GOOS, m.opts.GOARCH)
	for _, asset := range payload.Assets {
		switch {
		case hasAssetName && asset.Name == wantAsset && validateUpdateURL(asset.BrowserDownloadURL, m.opts.AllowInsecureHTTP):
			rel.AssetName = asset.Name
			rel.AssetURL = asset.BrowserDownloadURL
			rel.AssetSize = asset.Size
		case asset.Name == "SHA256SUMS" && validateUpdateURL(asset.BrowserDownloadURL, m.opts.AllowInsecureHTTP):
			rel.ChecksumsURL = asset.BrowserDownloadURL
		}
	}
	return rel, true, nil
}
