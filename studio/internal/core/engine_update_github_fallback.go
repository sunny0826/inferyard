package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (m *engineUpdateManager) isDefaultGitHubAPI() bool {
	base := strings.TrimRight(strings.TrimSpace(m.opts.GitHubAPIBase), "/")
	return base == "" || strings.EqualFold(base, defaultGitHubAPIBase)
}

func isGitHubRateLimitStatus(status int) bool {
	return status == http.StatusForbidden || status == http.StatusTooManyRequests
}

func hasArchiveReleaseAsset(assets []remoteReleaseAsset) bool {
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip") {
			return true
		}
	}
	return false
}

func (m *engineUpdateManager) resolveGitHubLatestTagViaWeb(ctx context.Context, baseClient *http.Client, repo string) (string, string, error) {
	var resolvedTag, resolvedHTML string
	errTagFound := errors.New("tag_found")
	noFollow := *baseClient
	noFollow.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !strings.EqualFold(req.URL.Hostname(), "github.com") {
			return errors.New("unexpected redirect host")
		}
		const marker = "/releases/tag/"
		if idx := strings.Index(req.URL.Path, marker); idx >= 0 {
			tag := strings.Trim(req.URL.Path[idx+len(marker):], "/")
			if tag != "" && !strings.Contains(tag, "/") {
				resolvedTag = tag
				resolvedHTML = "https://github.com/" + repo + "/releases/tag/" + tag
				return errTagFound
			}
		}
		return nil
	}
	webURL := "https://github.com/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, webURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "InferYard-Studio/"+Version)
	resp, err := noFollow.Do(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resolvedTag != "" {
		return resolvedTag, resolvedHTML, nil
	}
	if err != nil && !errors.Is(err, errTagFound) {
		return "", "", err
	}
	return "", "", fmt.Errorf("github latest tag redirect unavailable for %s", repo)
}

func probeGitHubReleaseAssetHEAD(ctx context.Context, baseClient *http.Client, assetURL string) (int64, bool) {
	probeClient := *baseClient
	probeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		host := strings.ToLower(req.URL.Hostname())
		if host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com") {
			return nil
		}
		return errors.New("disallowed redirect host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, assetURL, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("User-Agent", "InferYard-Studio/"+Version)
	resp, err := probeClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return size, true
}

func candidateGitHubAssetNames(repo, tag, platform, pyMinor string) []string {
	ver := strings.TrimPrefix(tag, "v")
	if pyMinor == "" {
		pyMinor = "12"
	}
	switch repo {
	case "PrismML-Eng/llama.cpp", "ggml-org/llama.cpp":
		switch platform {
		case "darwin/arm64":
			return []string{"llama-" + tag + "-bin-macos-arm64.tar.gz"}
		case "darwin/amd64":
			return []string{"llama-" + tag + "-bin-macos-x64.tar.gz"}
		case "windows/amd64":
			return []string{
				"llama-" + tag + "-bin-win-cpu-x64.zip",
				"llama-" + tag + "-bin-win-cuda-12.4-x64.zip",
			}
		case "linux/amd64":
			return []string{"llama-" + tag + "-bin-ubuntu-x64.tar.gz"}
		case "linux/arm64":
			return []string{"llama-" + tag + "-bin-ubuntu-arm64.tar.gz"}
		}
	case "vllm-project/vllm":
		if platform == "darwin/arm64" {
			return []string{
				fmt.Sprintf("vllm-%s+cpu-cp3%s-cp3%s-macosx_11_0_arm64.whl", ver, pyMinor, pyMinor),
				fmt.Sprintf("vllm-%s+cpu-cp38-abi3-macosx_11_0_arm64.whl", ver),
			}
		}
	case "vllm-project/vllm-metal":
		if platform == "darwin/arm64" {
			return []string{
				fmt.Sprintf("vllm_metal-%s-cp3%s-cp3%s-macosx_15_0_arm64.whl", ver, pyMinor, pyMinor),
				fmt.Sprintf("vllm_metal-%s-cp38-abi3-macosx_15_0_arm64.whl", ver),
			}
		}
	}
	return nil
}

func (m *engineUpdateManager) fallbackGitHubReleaseViaWeb(ctx context.Context, client *http.Client, key, family, dist, repo string) (remoteEngineRelease, error) {
	tag, htmlURL, err := m.resolveGitHubLatestTagViaWeb(ctx, client, repo)
	if err != nil {
		return remoteEngineRelease{}, err
	}
	pyMinor := "12"
	if family == "vllm" && m.service != nil {
		pyMinor = m.service.detectInstalledVenvPythonMinor("vllm")
	}
	platform := m.opts.Platform
	if strings.TrimSpace(platform) == "" {
		platform = CurrentPlatform()
	}
	var assets []remoteReleaseAsset
	for _, name := range candidateGitHubAssetNames(repo, tag, platform, pyMinor) {
		escaped := (&url.URL{Path: name}).EscapedPath()
		dlURL := "https://github.com/" + repo + "/releases/download/" + tag + "/" + escaped
		if size, ok := probeGitHubReleaseAssetHEAD(ctx, client, dlURL); ok {
			assets = append(assets, remoteReleaseAsset{Name: name, URL: dlURL, Bytes: size})
			if strings.HasPrefix(repo, "vllm-project/") {
				break
			}
		}
	}
	return remoteEngineRelease{
		Key:          key,
		Family:       family,
		Distribution: dist,
		Version:      tag,
		ReleaseURL:   htmlURL,
		Assets:       assets,
	}, nil
}
