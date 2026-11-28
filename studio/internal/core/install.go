package core

import (
	"context"
	"inferyard/studio/internal/l10n"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const installPython = "3.15.2"

const uvRelease = "0.13.6"

type installProxyCtxKey struct{}

func withInstallProxy(ctx context.Context, proxy string) context.Context {
	trimmed := strings.TrimSpace(proxy)
	if trimmed == "" || !ValidateDownloadProxy(trimmed) {
		return ctx
	}
	return context.WithValue(ctx, installProxyCtxKey{}, trimmed)
}

func installProxyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	proxy, _ := ctx.Value(installProxyCtxKey{}).(string)
	return proxy
}

func applyInstallProxyEnv(environ []string, proxy string) []string {
	if strings.TrimSpace(proxy) == "" {
		return environ
	}
	filtered := make([]string, 0, len(environ)+6)
	for _, entry := range environ {
		k, _, ok := strings.Cut(entry, "=")
		if ok {
			switch strings.ToUpper(k) {
			case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return append(filtered,
		"HTTP_PROXY="+proxy,
		"HTTPS_PROXY="+proxy,
		"ALL_PROXY="+proxy,
		"http_proxy="+proxy,
		"https_proxy="+proxy,
		"all_proxy="+proxy,
	)
}

func defaultInstallHTTPDo(req *http.Request) (*http.Response, error) {
	if proxy := installProxyFromContext(req.Context()); proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer tr.CloseIdleConnections()
			return (&http.Client{Transport: tr}).Do(req)
		}
	}
	return http.DefaultClient.Do(req)
}

var (
	installHTTPDo        = defaultInstallHTTPDo
	installLookPath      = exec.LookPath
	installStat          = os.Stat
	installCommandOutput = func(ctx context.Context, path string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		if proxy := installProxyFromContext(ctx); proxy != "" {
			cmd.Env = applyInstallProxyEnv(os.Environ(), proxy)
		}
		return cmd.CombinedOutput()
	}
)

func cliBinaryName() string {
	if runtime.GOOS == "windows" {
		return "inferyard.exe"
	}
	return "inferyard"
}

func uvBinaryName() string {
	if runtime.GOOS == "windows" {
		return "uv.exe"
	}
	return "uv"
}

func extraBinDirs() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	}
	return nil
}

func cliCandidatePaths(configured string) []string {
	var raw []string
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		if st, err := installStat(trimmed); err == nil && !st.IsDir() {
			raw = append(raw, trimmed)
		}
	}
	if p, err := installLookPath(cliBinaryName()); err == nil {
		raw = append(raw, p)
	}
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin", cliBinaryName()))
	}
	for _, dir := range extraBinDirs() {
		dirs = append(dirs, filepath.Join(dir, cliBinaryName()))
	}
	for _, p := range dirs {
		if st, err := installStat(p); err == nil && !st.IsDir() {
			raw = append(raw, p)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, cand := range raw {
		abs, err := filepath.Abs(cand)
		if err != nil {
			abs = cand
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

func FindCLI() string {
	if paths := cliCandidatePaths(""); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func uvCandidatePaths(toolsDir string) []string {
	raw := []string{}
	if p, err := installLookPath(uvBinaryName()); err == nil {
		raw = append(raw, p)
	}
	if strings.TrimSpace(toolsDir) != "" {
		raw = append(raw, filepath.Join(toolsDir, uvBinaryName()))
	}
	if home, err := os.UserHomeDir(); err == nil {
		raw = append(raw,
			filepath.Join(home, ".local", "bin", uvBinaryName()),
			filepath.Join(home, ".cargo", "bin", uvBinaryName()),
		)
	}
	for _, dir := range extraBinDirs() {
		raw = append(raw, filepath.Join(dir, uvBinaryName()))
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range raw {
		st, err := installStat(p)
		if err != nil || st.IsDir() {
			continue
		}
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func FindUV(toolsDir string) string {
	if paths := uvCandidatePaths(toolsDir); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func parseUVVersion(raw string) (semver, bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return semver{}, false
	}
	exe := strings.ToLower(fields[0])
	if exe != "uv" && exe != "uv.exe" {
		return semver{}, false
	}
	verToken := fields[1]
	if base, _, ok := strings.Cut(verToken, "+"); ok {
		verToken = base
	}
	return parseSemver(verToken)
}

func probeUVVersion(ctx context.Context, path string) (semver, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := installCommandOutput(probeCtx, path, "--version")
	if err != nil {
		return semver{}, false
	}
	return parseUVVersion(string(out))
}

func findCompatibleUV(ctx context.Context, toolsDir string) (string, bool) {
	minVer, hasMin := parseSemver(uvRelease)
	candidates := uvCandidatePaths(toolsDir)
	sawIncompatible := false
	for _, cand := range candidates {
		if !hasMin {
			return cand, false
		}
		ver, ok := probeUVVersion(ctx, cand)
		if ok && compareSemver(ver, minVer) >= 0 {
			return cand, sawIncompatible
		}
		sawIncompatible = true
	}
	return "", sawIncompatible
}

func (s *Service) inspectCLIForInstall(u UserSettings) (string, *InferyardProbe) {
	var outdated *InferyardProbe
	for _, cand := range cliCandidatePaths(str(u.InferyardPath)) {
		p := s.probeCLI(cand)
		if p.Status == "ok" && p.Path != nil {
			return *p.Path, nil
		}
		if p.Status == "outdated" && outdated == nil {
			copyP := copyProbe(p)
			outdated = &copyP
		}
	}
	return "", outdated
}

func (s *Service) InstallCLI(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()
	path, apiErr := s.installCLILocked(ctx, onStatus)
	if apiErr == nil {
		s.finishDirectCLIInstall(ctx)
	}
	return path, apiErr
}

func (s *Service) installCLILocked(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	if s.paths != nil {
		p := s.probe(s.userSettings())
		if p.Status == "outdated" {
			return "", cliOutdatedError(p.Version)
		}
		if p.Status != "ok" || p.Path == nil || *p.Path != s.paths.CLI {
			return "", Conflict("cli_binding_failed")
		}
		return s.paths.CLI, nil
	}
	report := func(message string) {
		if onStatus != nil {
			onStatus(message)
		}
	}
	toolsDir := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	initialSettings := s.userSettings()
	ctx = withInstallProxy(ctx, str(initialSettings.DownloadProxy))
	initialCLIPath := str(initialSettings.InferyardPath)
	reusable, outdated := s.inspectCLIForInstall(initialSettings)
	if reusable != "" {
		return s.finishInstall(ctx, initialCLIPath, reusable, report)
	}
	targetVersion := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if _, ok := parseSemver(targetVersion); !ok {
		return "", &ApiError{Code: "install_version_unknown", Message: l10n.TR("core.install.err.version_unknown")}
	}
	uv, sawIncompatibleUV := findCompatibleUV(ctx, toolsDir)
	if uv == "" {
		if sawIncompatibleUV {
			report(l10n.TR("core.install.status.uv_outdated_downloading"))
		} else {
			report(l10n.TR("core.install.status.uv_missing_downloading"))
		}
		var e *ApiError
		uv, e = downloadUV(ctx, toolsDir)
		if e != nil {
			return "", e
		}
	}
	if outdated != nil {
		report(l10n.TRf("core.install.status.upgrading_cli", "version", targetVersion))
	} else {
		report(l10n.TRf("core.install.status.installing_cli", "version", targetVersion))
	}
	installCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := installCommandOutput(installCtx, uv, "tool", "install", "--upgrade", "--force", "--managed-python", "--python", installPython, "inferyard=="+targetVersion)
	if err != nil {
		tail := string(out)
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		code := "install_failed"
		errMsg := l10n.TR("core.install.err.auto_install_failed")
		if outdated != nil {
			code = "cli_upgrade_failed"
			errMsg = l10n.TR("core.install.err.auto_upgrade_failed")
		}
		return "", &ApiError{Code: code, Message: errMsg, Details: map[string]any{"output_tail": tail}}
	}
	binOut, err := installCommandOutput(ctx, uv, "tool", "dir", "--bin")
	if err != nil {
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.locate_result_failed")}
	}
	installed := filepath.Join(strings.TrimSpace(string(binOut)), cliBinaryName())
	if st, err := installStat(installed); err != nil || st.IsDir() {
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.result_missing")}
	}
	s.clearProbeCache()
	if p := s.probeCLI(installed); p.Status != "ok" {
		if p.Status == "outdated" {
			return "", cliOutdatedError(p.Version)
		}
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.post_install_broken")}
	}
	return s.finishInstall(ctx, initialCLIPath, installed, report)
}

func (s *Service) saveInstalledCLIPathIfCurrent(ctx context.Context, initialPath, installed string) (string, *ApiError) {
	if !filepath.IsAbs(installed) || strings.ContainsRune(installed, 0) {
		return "", Err("invalid_request")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	if ctx.Err() != nil || (str(u.InferyardPath) != initialPath && s.probe(u).Status == "ok") {
		if p := s.probe(u); p.Status == "ok" && p.Path != nil {
			return *p.Path, nil
		}
		return "", Err("cancelled")
	}
	u.InferyardPath = ptr(installed)
	if _, e := s.persistSettings(u, true); e != nil {
		return "", e
	}
	return installed, nil
}

func (s *Service) finishInstall(ctx context.Context, initialPath, path string, onStatus func(string)) (string, *ApiError) {
	savedPath, e := s.saveInstalledCLIPathIfCurrent(ctx, initialPath, path)
	if e != nil {
		return "", e
	}
	if onStatus != nil {
		onStatus(l10n.TRf("core.install.status.cli_ready", "path", savedPath))
	}
	return savedPath, nil
}
